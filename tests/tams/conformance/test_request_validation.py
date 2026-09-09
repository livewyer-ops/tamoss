from __future__ import annotations

from copy import deepcopy
from typing import Any
from uuid import uuid4

import pytest
from fastapi.testclient import TestClient

from tests.tams.support import (
    IMAGE_FORMAT,
    VIDEO_FORMAT,
    allocate_objects,
    create_video_flow,
    upload_allocated_object,
    webhook_payload,
)

pytestmark = [pytest.mark.tams_conformance, pytest.mark.tams_semantics]

AUDIO_FORMAT = "urn:x-nmos:format:audio"
DATA_FORMAT = "urn:x-nmos:format:data"
MULTI_FORMAT = "urn:x-nmos:format:multi"


def _technical_metadata(format_value: str) -> dict[str, Any]:
    essence_by_format: dict[str, dict[str, Any]] = {
        VIDEO_FORMAT: {
            "frame_width": 1920,
            "frame_height": 1080,
            "frame_rate": {"numerator": 25, "denominator": 1},
        },
        AUDIO_FORMAT: {"sample_rate": 48_000, "channels": 2},
        IMAGE_FORMAT: {"frame_width": 1920, "frame_height": 1080},
        DATA_FORMAT: {"data_type": "urn:x-tams:data:example"},
        MULTI_FORMAT: {},
    }
    metadata: dict[str, Any] = {
        "format": format_value,
        "essence_parameters": deepcopy(essence_by_format[format_value]),
    }
    if format_value == MULTI_FORMAT:
        metadata["container"] = "video/mp2t"
    else:
        metadata["codec"] = {
            VIDEO_FORMAT: "video/h264",
            AUDIO_FORMAT: "audio/aac",
            IMAGE_FORMAT: "image/jpeg",
            DATA_FORMAT: "application/json",
        }[format_value]
    return metadata


def _direct_flow_payload(format_value: str) -> dict[str, Any]:
    return {
        "id": str(uuid4()),
        "source_id": str(uuid4()),
        **_technical_metadata(format_value),
    }


def _profile_payload(format_value: str) -> dict[str, Any]:
    return {
        "id": str(uuid4()),
        "label": f"{format_value} validation profile",
        "flow_metadata": _technical_metadata(format_value),
    }


@pytest.mark.parametrize(
    "format_value",
    [VIDEO_FORMAT, AUDIO_FORMAT, IMAGE_FORMAT, DATA_FORMAT, MULTI_FORMAT],
)
def test_flow_essence_parameters_reject_unknown_fields(
    client: TestClient,
    format_value: str,
) -> None:
    payload = _direct_flow_payload(format_value)
    payload["essence_parameters"]["unknown_parameter"] = "not allowed"

    response = client.put(f"/flows/{payload['id']}", json=payload)

    assert response.status_code == 400


@pytest.mark.parametrize(
    "format_value",
    [VIDEO_FORMAT, AUDIO_FORMAT, IMAGE_FORMAT, DATA_FORMAT],
)
def test_profile_essence_parameters_reject_unknown_fields(
    client: TestClient,
    format_value: str,
) -> None:
    payload = _profile_payload(format_value)
    payload["flow_metadata"]["essence_parameters"]["unknown_parameter"] = "not allowed"

    response = client.post(f"/service/profiles/{payload['id']}", json=payload)

    assert response.status_code == 400


@pytest.mark.parametrize(
    ("format_value", "field_name"),
    [
        (VIDEO_FORMAT, "frame_rate"),
        (VIDEO_FORMAT, "vfr"),
        (AUDIO_FORMAT, "bit_depth"),
        (IMAGE_FORMAT, "aspect_ratio"),
        (DATA_FORMAT, "data_type"),
        (MULTI_FORMAT, "init_segments"),
    ],
)
def test_closed_essence_parameters_reject_explicit_null_fields(
    client: TestClient,
    format_value: str,
    field_name: str,
) -> None:
    payload = _direct_flow_payload(format_value)
    payload["essence_parameters"][field_name] = None

    response = client.put(f"/flows/{payload['id']}", json=payload)

    assert response.status_code == 400


def test_multi_essence_parameters_rejects_explicit_null_object(
    client: TestClient,
) -> None:
    payload = _direct_flow_payload(MULTI_FORMAT)
    payload["essence_parameters"] = None

    response = client.put(f"/flows/{payload['id']}", json=payload)

    assert response.status_code == 400


def test_multi_flows_keep_technical_metadata(client: TestClient) -> None:
    payload = _direct_flow_payload(MULTI_FORMAT)
    payload["segment_duration"] = {"numerator": 1, "denominator": 1}
    payload["avg_bit_rate"] = 12_000

    assert client.put(f"/flows/{payload['id']}", json=payload).status_code == 201

    flow = client.get(f"/flows/{payload['id']}").json()
    assert flow["container"] == "video/mp2t"
    assert flow["segment_duration"] == {"numerator": 1, "denominator": 1}
    assert flow["avg_bit_rate"] == 12_000


@pytest.mark.parametrize(
    "mutate",
    [
        lambda payload: payload.update(container=12_345),
        lambda payload: payload.update(container="not a mime type"),
        lambda payload: payload.update(avg_bit_rate=-1),
        lambda payload: payload.update(segment_duration="PT1S"),
    ],
    ids=("container-type", "container-pattern", "avg-bit-rate", "segment-duration"),
)
def test_multi_flow_technical_metadata_is_validated(
    client: TestClient,
    mutate: Any,
) -> None:
    payload = _direct_flow_payload(MULTI_FORMAT)
    mutate(payload)

    response = client.put(f"/flows/{payload['id']}", json=payload)

    assert response.status_code == 400


def test_profiles_enforce_video_frame_rate_conditionals(client: TestClient) -> None:
    fixed_without_rate = _profile_payload(VIDEO_FORMAT)
    fixed_without_rate["flow_metadata"]["essence_parameters"].pop("frame_rate")

    variable_with_rate = _profile_payload(VIDEO_FORMAT)
    variable_with_rate["flow_metadata"]["essence_parameters"]["vfr"] = True

    valid_variable = _profile_payload(VIDEO_FORMAT)
    valid_variable["flow_metadata"]["essence_parameters"].pop("frame_rate")
    valid_variable["flow_metadata"]["essence_parameters"]["vfr"] = True

    assert (
        client.post(
            f"/service/profiles/{fixed_without_rate['id']}",
            json=fixed_without_rate,
        ).status_code
        == 400
    )
    assert (
        client.post(
            f"/service/profiles/{variable_with_rate['id']}",
            json=variable_with_rate,
        ).status_code
        == 400
    )
    assert (
        client.post(
            f"/service/profiles/{valid_variable['id']}",
            json=valid_variable,
        ).status_code
        == 201
    )


@pytest.mark.parametrize(
    "field_name",
    ["label", "description", "created_by", "created", "tags"],
)
def test_profile_optional_contract_fields_reject_explicit_null(
    client: TestClient,
    field_name: str,
) -> None:
    payload = _profile_payload(VIDEO_FORMAT)
    payload[field_name] = None

    response = client.post(f"/service/profiles/{payload['id']}", json=payload)

    assert response.status_code == 400


def test_flow_status_and_init_segments_reject_explicit_null(
    client: TestClient,
) -> None:
    null_status = _direct_flow_payload(VIDEO_FORMAT)
    null_status["status"] = None

    null_init_segments = _direct_flow_payload(VIDEO_FORMAT)
    null_init_segments["essence_parameters"]["init_segments"] = None

    null_profile_init = _profile_payload(VIDEO_FORMAT)
    null_profile_init["flow_metadata"]["essence_parameters"]["init_segments"] = None

    assert (
        client.put(f"/flows/{null_status['id']}", json=null_status).status_code == 400
    )
    assert (
        client.put(
            f"/flows/{null_init_segments['id']}", json=null_init_segments
        ).status_code
        == 400
    )
    assert (
        client.post(
            f"/service/profiles/{null_profile_init['id']}",
            json=null_profile_init,
        ).status_code
        == 400
    )


@pytest.mark.parametrize(
    "mutate",
    [
        lambda payload: payload.update(container=None),
        lambda payload: payload.update(
            segment_duration={"numerator": 1, "denominator": None}
        ),
        lambda payload: payload["essence_parameters"].update(
            frame_rate={"numerator": 25, "denominator": None}
        ),
    ],
    ids=("container", "segment-duration", "frame-rate"),
)
def test_flow_nested_contract_fields_reject_explicit_null(
    client: TestClient,
    mutate: Any,
) -> None:
    payload = _direct_flow_payload(VIDEO_FORMAT)
    mutate(payload)

    response = client.put(f"/flows/{payload['id']}", json=payload)

    assert response.status_code == 400


def test_profile_nested_technical_fields_reject_explicit_null(
    client: TestClient,
) -> None:
    payload = _profile_payload(VIDEO_FORMAT)
    payload["flow_metadata"]["segment_duration"] = {
        "numerator": 1,
        "denominator": None,
    }

    response = client.post(f"/service/profiles/{payload['id']}", json=payload)

    assert response.status_code == 400


def test_open_technical_objects_preserve_null_extensions(
    client: TestClient,
) -> None:
    payload = _direct_flow_payload(VIDEO_FORMAT)
    payload["vendor_extension"] = None
    payload["segment_duration"] = {
        "numerator": 1,
        "vendor_extension": None,
    }
    payload["container_mapping"] = {
        "track_index": 0,
        "vendor_extension": None,
    }
    payload["essence_parameters"]["frame_rate"] = {
        "numerator": 25,
        "vendor_extension": None,
    }

    response = client.put(f"/flows/{payload['id']}", json=payload)

    assert response.status_code == 201
    flow = client.get(f"/flows/{payload['id']}").json()
    assert flow["vendor_extension"] is None
    assert flow["segment_duration"]["vendor_extension"] is None
    assert flow["container_mapping"]["vendor_extension"] is None
    assert flow["essence_parameters"]["frame_rate"]["vendor_extension"] is None


def test_profile_open_technical_objects_preserve_null_extensions(
    client: TestClient,
) -> None:
    payload = _profile_payload(VIDEO_FORMAT)
    payload["flow_metadata"]["vendor_extension"] = None
    payload["flow_metadata"]["segment_duration"] = {
        "numerator": 1,
        "vendor_extension": None,
    }
    payload["flow_metadata"]["essence_parameters"]["frame_rate"] = {
        "numerator": 25,
        "vendor_extension": None,
    }

    response = client.post(f"/service/profiles/{payload['id']}", json=payload)

    assert response.status_code == 201
    profile = client.get(f"/service/profiles/{payload['id']}").json()
    assert profile["flow_metadata"]["vendor_extension"] is None
    assert profile["flow_metadata"]["segment_duration"]["vendor_extension"] is None
    assert (
        profile["flow_metadata"]["essence_parameters"]["frame_rate"]["vendor_extension"]
        is None
    )

    flow_id = uuid4()
    derived = client.put(
        f"/flows/{flow_id}",
        json={
            "id": str(flow_id),
            "source_id": str(uuid4()),
            "profile_id": payload["id"],
        },
    )
    assert derived.status_code == 201
    flow = client.get(f"/flows/{flow_id}").json()
    assert flow["vendor_extension"] is None
    assert flow["segment_duration"]["vendor_extension"] is None
    assert flow["essence_parameters"]["frame_rate"]["vendor_extension"] is None


def test_flow_collection_rejects_explicit_null_role(client: TestClient) -> None:
    parent_id, _, _ = create_video_flow(client)
    child_id, _, _ = create_video_flow(client)

    response = client.put(
        f"/flows/{parent_id}/flow_collection",
        json=[{"id": str(child_id), "role": None}],
    )

    assert response.status_code == 400


@pytest.mark.parametrize("bulk", [False, True], ids=("single", "bulk"))
def test_segment_posts_reject_explicit_null_init_object_id(
    client: TestClient,
    bulk: bool,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    object_id = f"validation/{uuid4()}.ts"
    allocate_objects(client, flow_id, [object_id])
    upload_allocated_object(client, object_id)
    segment = {
        "object_id": object_id,
        "timerange": "[0:0_10:0)",
        "init_object_id": None,
    }

    response = client.post(
        f"/flows/{flow_id}/segments",
        json=[segment] if bulk else segment,
    )

    assert response.status_code == 400


@pytest.mark.parametrize("invalid_value", [0, 1, None, "false"])
def test_storage_presigned_rejects_non_boolean_json_values(
    client: TestClient,
    invalid_value: object,
) -> None:
    flow_id, _, _ = create_video_flow(client)

    response = client.post(
        f"/flows/{flow_id}/storage",
        json={"object_ids": [f"objects/{uuid4()}.ts"], "presigned": invalid_value},
    )

    assert response.status_code == 400


def test_storage_distinguishes_omitted_body_from_explicit_null(
    client: TestClient,
) -> None:
    omitted_flow_id, _, _ = create_video_flow(client)
    null_flow_id, _, _ = create_video_flow(client)

    omitted = client.post(f"/flows/{omitted_flow_id}/storage")
    explicit_null = client.post(
        f"/flows/{null_flow_id}/storage",
        content=b"null",
        headers={"Content-Type": "application/json"},
    )

    assert omitted.status_code == 201
    assert explicit_null.status_code == 400


@pytest.mark.parametrize(
    "field_name",
    ["limit", "object_ids", "storage_id", "content_type"],
)
def test_storage_optional_contract_fields_reject_explicit_null(
    client: TestClient,
    field_name: str,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    payload: dict[str, Any] = {"object_ids": [f"objects/{uuid4()}.ts"]}
    payload[field_name] = None

    response = client.post(f"/flows/{flow_id}/storage", json=payload)

    assert response.status_code == 400


@pytest.mark.parametrize(
    ("field_name", "invalid_value"),
    [
        ("presigned", 0),
        ("presigned", None),
        ("verbose_storage", 1),
        ("verbose_storage", None),
        ("include_object_timerange", 1),
        ("include_object_timerange", None),
        ("flow_collected_by_ids", None),
        ("source_collected_by_ids", None),
    ],
)
def test_webhook_options_reject_non_boolean_json_values(
    client: TestClient,
    field_name: str,
    invalid_value: object,
) -> None:
    payload = webhook_payload()
    payload[field_name] = invalid_value

    response = client.post("/service/webhooks", json=payload)

    assert response.status_code == 400


def test_webhook_put_uses_strict_option_validation(client: TestClient) -> None:
    created = client.post("/service/webhooks", json=webhook_payload())
    assert created.status_code == 201
    webhook_id = created.json()["id"]
    payload = webhook_payload(
        id=webhook_id,
        status="disabled",
        include_object_timerange=1,
    )

    response = client.put(f"/service/webhooks/{webhook_id}", json=payload)

    assert response.status_code == 400


@pytest.mark.parametrize("path", ["/flows", "/sources"])
@pytest.mark.parametrize("method", ["GET", "HEAD"])
def test_collected_by_ids_matches_uuid_list_empty_schema(
    client: TestClient,
    path: str,
    method: str,
) -> None:
    valid_id = str(uuid4())
    invalid_values = (
        valid_id.upper(),
        "00000000-0000-0000-0000-000000000000",
        "00000000-0000-6000-8000-000000000000",
        f"{valid_id},",
        f",{valid_id}",
        f"{valid_id},,{uuid4()}",
    )

    assert (
        client.request(method, path, params={"collected_by_ids": ""}).status_code == 200
    )
    assert (
        client.request(
            method,
            path,
            params={"collected_by_ids": f"{valid_id},{uuid4()}"},
        ).status_code
        == 200
    )
    for invalid_value in invalid_values:
        response = client.request(
            method,
            path,
            params={"collected_by_ids": invalid_value},
        )
        assert response.status_code == 400, (method, path, invalid_value)


@pytest.mark.parametrize(
    ("path", "parameter"),
    [
        ("/flows", "tag.editorial"),
        ("/sources", "tag.editorial"),
        ("/service/webhooks", "tag.editorial"),
        ("/service/storage-backends", "tag.editorial"),
    ],
)
@pytest.mark.parametrize("method", ["GET", "HEAD"])
def test_resource_tag_filters_match_url_tag_list_schema(
    client: TestClient,
    path: str,
    parameter: str,
    method: str,
) -> None:
    assert client.request(method, path, params={parameter: ""}).status_code == 200
    assert (
        client.request(method, path, params={parameter: "news,archive"}).status_code
        == 200
    )
    for invalid_value in (",news", "news,", "news,,archive"):
        assert (
            client.request(
                method,
                path,
                params={parameter: invalid_value},
            ).status_code
            == 400
        )


@pytest.mark.parametrize("method", ["GET", "HEAD"])
@pytest.mark.parametrize(
    "parameter",
    ["flow_tag.editorial", "storage_backend_tag.access"],
)
def test_object_dynamic_tag_filters_match_url_tag_list_schema(
    client: TestClient,
    method: str,
    parameter: str,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    object_id = f"validation/{uuid4()}.ts"
    allocate_objects(client, flow_id, [object_id])
    upload_allocated_object(client, object_id)
    registered = client.post(
        f"/flows/{flow_id}/segments",
        json={"object_id": object_id, "timerange": "[0:0_10:0)"},
    )
    assert registered.status_code == 201

    assert (
        client.request(
            method, f"/objects/{object_id}", params={parameter: ""}
        ).status_code
        == 200
    )
    for invalid_value in (",news", "news,", "news,,archive"):
        assert (
            client.request(
                method,
                f"/objects/{object_id}",
                params={parameter: invalid_value},
            ).status_code
            == 400
        )


@pytest.mark.parametrize("method", ["GET", "HEAD"])
def test_segment_storage_tag_filters_match_url_tag_list_schema(
    client: TestClient,
    method: str,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    parameter = "storage_backend_tag.access"

    assert (
        client.request(
            method,
            f"/flows/{flow_id}/segments",
            params={parameter: ""},
        ).status_code
        == 200
    )
    for invalid_value in (",news", "news,", "news,,archive"):
        assert (
            client.request(
                method,
                f"/flows/{flow_id}/segments",
                params={parameter: invalid_value},
            ).status_code
            == 400
        )
