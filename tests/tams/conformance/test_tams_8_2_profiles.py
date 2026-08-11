from __future__ import annotations

from collections.abc import Iterator
from contextlib import contextmanager
from copy import deepcopy
from typing import Any
from uuid import UUID, uuid4

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient

from tests.tams.support import (
    image_flow_payload,
    multi_flow_payload,
    register_segment,
    upload_allocated_object,
    video_flow_payload,
)

pytestmark = pytest.mark.tams_conformance

VIDEO_FORMAT = "urn:x-nmos:format:video"
AUDIO_FORMAT = "urn:x-nmos:format:audio"
IMAGE_FORMAT = "urn:x-tam:format:image"
DATA_FORMAT = "urn:x-nmos:format:data"


def _video_profile_payload(
    profile_id: UUID,
    *,
    label: str = "HD profile",
    metadata_overrides: dict[str, Any] | None = None,
) -> dict[str, Any]:
    flow_metadata: dict[str, Any] = {
        "format": VIDEO_FORMAT,
        "codec": "video/h264",
        "container": "video/mp4",
        "avg_bit_rate": 8_000_000,
        "segment_duration": {"numerator": 2, "denominator": 1},
        "essence_parameters": {
            "frame_rate": {"numerator": 25, "denominator": 1},
            "frame_width": 1920,
            "frame_height": 1080,
        },
        "x-profile-mode": {"quality": "mezzanine"},
    }
    flow_metadata.update(metadata_overrides or {})
    return {
        "id": str(profile_id),
        "label": label,
        "description": "TAMS 8.2 Profile",
        "tags": {"profile_version": "1"},
        "flow_metadata": flow_metadata,
    }


def _profile_flow_payload(
    flow_id: UUID,
    source_id: UUID,
    profile_id: UUID | str,
    **overrides: Any,
) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "id": str(flow_id),
        "source_id": str(source_id),
        "profile_id": str(profile_id),
        "label": "Profiled flow",
        "status": "ingesting",
    }
    payload.update(overrides)
    return payload


def _create_profile(
    client: TestClient,
    profile_id: UUID,
    *,
    label: str = "HD profile",
    metadata_overrides: dict[str, Any] | None = None,
) -> dict[str, Any]:
    response = client.post(
        f"/service/profiles/{profile_id}",
        json=_video_profile_payload(
            profile_id,
            label=label,
            metadata_overrides=metadata_overrides,
        ),
    )
    assert response.status_code == 201, response.text
    return response.json()


def _create_profile_flow(
    client: TestClient,
    profile_id: UUID,
    *,
    flow_id: UUID | None = None,
    source_id: UUID | None = None,
) -> tuple[UUID, UUID, dict[str, Any]]:
    resolved_flow_id = flow_id or uuid4()
    resolved_source_id = source_id or uuid4()
    response = client.put(
        f"/flows/{resolved_flow_id}",
        json=_profile_flow_payload(
            resolved_flow_id,
            resolved_source_id,
            profile_id,
        ),
    )
    assert response.status_code == 201, response.text
    return resolved_flow_id, resolved_source_id, response.json()


def test_profile_id_tri_state_expands_updates_unlinks_and_strips_inherited_fields(
    client: TestClient,
) -> None:
    profile_id = uuid4()
    _create_profile(client, profile_id)
    flow_id, source_id, created = _create_profile_flow(client, profile_id)

    assert created["profile_id"] == str(profile_id)
    assert created["container"] == "video/mp4"
    assert created["avg_bit_rate"] == 8_000_000
    assert created["x-profile-mode"] == {"quality": "mezzanine"}
    assert "profile_version" not in created.get("tags", {})

    same_profile = _profile_flow_payload(
        flow_id,
        source_id,
        profile_id,
        label="Same Profile update",
    )
    assert client.put(f"/flows/{flow_id}", json=same_profile).status_code == 204

    omitted_profile = {
        "id": str(flow_id),
        "source_id": str(source_id),
        "description": "Association omitted but retained",
    }
    assert client.put(f"/flows/{flow_id}", json=omitted_profile).status_code == 204
    retained = client.get(f"/flows/{flow_id}").json()
    assert retained["profile_id"] == str(profile_id)
    assert retained["description"] == "Association omitted but retained"
    assert retained["avg_bit_rate"] == 8_000_000

    incomplete_unlink = {
        "id": str(flow_id),
        "source_id": str(source_id),
        "profile_id": "",
        "format": VIDEO_FORMAT,
    }
    rejected = client.put(f"/flows/{flow_id}", json=incomplete_unlink)
    assert rejected.status_code == 400
    assert client.get(f"/flows/{flow_id}").json()["profile_id"] == str(profile_id)

    direct_payload = video_flow_payload(
        flow_id,
        source_id,
        label="Direct flow after unlink",
    )
    direct_payload["profile_id"] = ""
    unlinked = client.put(f"/flows/{flow_id}", json=direct_payload)
    assert unlinked.status_code == 204, unlinked.text

    stored = client.get(f"/flows/{flow_id}").json()
    assert "profile_id" not in stored
    assert stored["container"] == "video/mp2t"
    assert "avg_bit_rate" not in stored
    assert "segment_duration" not in stored
    assert "x-profile-mode" not in stored
    assert client.get("/flows", params={"profile_id": str(profile_id)}).json() == []

    assert client.put(f"/flows/{flow_id}", json=direct_payload).status_code == 400


def test_profile_id_rejects_malformed_empty_attach_and_repoint(
    client: TestClient,
) -> None:
    first_profile_id = uuid4()
    second_profile_id = uuid4()
    _create_profile(client, first_profile_id)
    _create_profile(client, second_profile_id, label="Second Profile")
    profiled_flow_id, profiled_source_id, _ = _create_profile_flow(
        client, first_profile_id
    )

    re_point = _profile_flow_payload(
        profiled_flow_id,
        profiled_source_id,
        second_profile_id,
    )
    assert client.put(f"/flows/{profiled_flow_id}", json=re_point).status_code == 400

    malformed = _profile_flow_payload(
        profiled_flow_id,
        profiled_source_id,
        "not-a-uuid",
    )
    assert client.put(f"/flows/{profiled_flow_id}", json=malformed).status_code == 400

    direct_flow_id = uuid4()
    direct_source_id = uuid4()
    direct_payload = video_flow_payload(direct_flow_id, direct_source_id)
    assert (
        client.put(f"/flows/{direct_flow_id}", json=direct_payload).status_code == 201
    )

    attach = _profile_flow_payload(
        direct_flow_id,
        direct_source_id,
        first_profile_id,
    )
    assert client.put(f"/flows/{direct_flow_id}", json=attach).status_code == 400

    already_direct_unlink = deepcopy(direct_payload)
    already_direct_unlink["profile_id"] = ""
    assert (
        client.put(f"/flows/{direct_flow_id}", json=already_direct_unlink).status_code
        == 400
    )

    new_flow_id = uuid4()
    new_direct = video_flow_payload(new_flow_id, uuid4())
    new_direct["profile_id"] = ""
    assert client.put(f"/flows/{new_flow_id}", json=new_direct).status_code == 400

    missing_profile_flow_id = uuid4()
    missing_profile = _profile_flow_payload(
        missing_profile_flow_id,
        uuid4(),
        uuid4(),
    )
    assert (
        client.put(
            f"/flows/{missing_profile_flow_id}", json=missing_profile
        ).status_code
        == 400
    )


def test_profile_flow_metadata_rejects_flow_owned_keys_and_allows_extensions(
    client: TestClient,
) -> None:
    protected_fields = {
        "id",
        "source_id",
        "label",
        "description",
        "created_by",
        "updated_by",
        "tags",
        "metadata_version",
        "generation",
        "created",
        "metadata_updated",
        "segments_updated",
        "status",
        "read_only",
        "max_bit_rate",
        "timerange",
        "flow_collection",
        "collected_by",
        "profile_id",
    }
    for field_name in protected_fields:
        profile_id = uuid4()
        payload = _video_profile_payload(profile_id)
        payload["flow_metadata"][field_name] = None
        response = client.post(f"/service/profiles/{profile_id}", json=payload)
        assert response.status_code == 400, field_name

    extension_profile_id = uuid4()
    created = _create_profile(client, extension_profile_id)
    assert created["flow_metadata"]["x-profile-mode"] == {"quality": "mezzanine"}


def test_profile_backed_avg_bit_rate_put_and_delete_are_rejected(
    client: TestClient,
) -> None:
    profile_id = uuid4()
    _create_profile(client, profile_id)
    flow_id, _, _ = _create_profile_flow(client, profile_id)

    updated = client.put(f"/flows/{flow_id}/avg_bit_rate", json=4_000_000)
    deleted = client.delete(f"/flows/{flow_id}/avg_bit_rate")

    assert updated.status_code == 400
    assert deleted.status_code == 400
    assert client.get(f"/flows/{flow_id}/avg_bit_rate").json() == 8_000_000


def _single_essence_profile_payload(
    profile_id: UUID,
    *,
    format: str,
    codec: str,
    label: str,
) -> dict[str, Any]:
    essence_parameters: dict[str, Any]
    if format == VIDEO_FORMAT:
        essence_parameters = {
            "frame_width": 1920,
            "frame_height": 1080,
            "frame_rate": {"numerator": 25, "denominator": 1},
        }
    elif format == AUDIO_FORMAT:
        essence_parameters = {"sample_rate": 48_000, "channels": 2}
    elif format == IMAGE_FORMAT:
        essence_parameters = {"frame_width": 640, "frame_height": 360}
    else:
        essence_parameters = {"data_type": "urn:x-tams:data:example"}
    return {
        "id": str(profile_id),
        "label": label,
        "flow_metadata": {
            "format": format,
            "codec": codec,
            "essence_parameters": essence_parameters,
        },
    }


def test_profile_filters_accept_all_single_essence_formats_mime_and_paging(
    client: TestClient,
) -> None:
    profiles = [
        (VIDEO_FORMAT, "video/h264", "Video Profile"),
        (AUDIO_FORMAT, "audio/aac", "Audio Profile"),
        (IMAGE_FORMAT, "image/jpeg", "Image Profile"),
        (DATA_FORMAT, "application/json", "Data Profile"),
    ]
    profile_ids: dict[str, UUID] = {}
    for format_value, codec, label in profiles:
        profile_id = uuid4()
        profile_ids[format_value] = profile_id
        response = client.post(
            f"/service/profiles/{profile_id}",
            json=_single_essence_profile_payload(
                profile_id,
                format=format_value,
                codec=codec,
                label=label,
            ),
        )
        assert response.status_code == 201, response.text

    for format_value, codec, label in profiles:
        params = {
            "format": format_value,
            "codec": codec,
            "label": label,
            "limit": 1,
        }
        listed = client.get("/service/profiles", params=params)
        headed = client.head("/service/profiles", params=params)
        assert listed.status_code == 200
        assert listed.json()[0]["id"] == str(profile_ids[format_value])
        assert listed.headers["x-paging-limit"] == "1"
        assert headed.status_code == 200
        assert headed.content == b""
        assert headed.headers["x-paging-limit"] == "1"

    first_page = client.get("/service/profiles", params={"limit": 1})
    assert first_page.headers["x-paging-nextkey"]
    second_page = client.get(
        "/service/profiles",
        params={"limit": 1, "page": first_page.headers["x-paging-nextkey"]},
    )
    assert second_page.status_code == 200
    assert second_page.json()[0]["id"] != first_page.json()[0]["id"]

    assert (
        client.get("/service/profiles", params={"codec": "x-vendor/custom"}).status_code
        == 200
    )
    assert (
        client.head(
            "/service/profiles", params={"codec": "x-vendor/custom"}
        ).status_code
        == 200
    )


def test_profile_filters_and_item_ids_reject_invalid_values_with_contract_statuses(
    client: TestClient,
) -> None:
    invalid_filters = [
        {"format": "urn:x-nmos:format:multi"},
        {"format": "video"},
        {"codec": "video"},
        {"codec": "custom/h264"},
        {"codec": "video/h264;profile=high"},
    ]
    for method in ("GET", "HEAD"):
        for params in invalid_filters:
            response = client.request(method, "/service/profiles", params=params)
            assert response.status_code == 400, (method, params)

        missing_id = uuid4()
        missing = client.request(method, f"/service/profiles/{missing_id}")
        malformed = client.request(method, "/service/profiles/not-a-uuid")
        assert missing.status_code == 404
        assert malformed.status_code == 404

    profile_id = uuid4()
    malformed_create = client.post(
        "/service/profiles/not-a-uuid",
        json=_video_profile_payload(profile_id),
    )
    assert malformed_create.status_code == 404


def _set_init_segments(payload: dict[str, Any], enabled: bool) -> dict[str, Any]:
    updated = deepcopy(payload)
    updated["essence_parameters"]["init_segments"] = enabled
    return updated


def test_init_segments_changes_only_before_first_segment(
    client: TestClient,
) -> None:
    empty_flow_id = uuid4()
    empty_payload = video_flow_payload(empty_flow_id, uuid4())
    assert client.put(f"/flows/{empty_flow_id}", json=empty_payload).status_code == 201
    assert (
        client.put(
            f"/flows/{empty_flow_id}",
            json=_set_init_segments(empty_payload, True),
        ).status_code
        == 204
    )
    assert client.put(f"/flows/{empty_flow_id}", json=empty_payload).status_code == 204

    ordinary_flow_id = uuid4()
    ordinary_payload = video_flow_payload(ordinary_flow_id, uuid4())
    assert (
        client.put(f"/flows/{ordinary_flow_id}", json=ordinary_payload).status_code
        == 201
    )
    register_segment(client, ordinary_flow_id)
    changed_ordinary = client.put(
        f"/flows/{ordinary_flow_id}",
        json=_set_init_segments(ordinary_payload, True),
    )
    assert changed_ordinary.status_code == 400
    assert (
        client.put(f"/flows/{ordinary_flow_id}", json=ordinary_payload).status_code
        == 204
    )

    init_flow_id = uuid4()
    init_payload = _set_init_segments(
        video_flow_payload(init_flow_id, uuid4()),
        True,
    )
    assert client.put(f"/flows/{init_flow_id}", json=init_payload).status_code == 201
    media_id = f"objects/{uuid4()}.m4s"
    init_id = f"objects/{uuid4()}.mp4"
    assert (
        client.post(
            f"/flows/{init_flow_id}/storage",
            json={"object_ids": [media_id]},
        ).status_code
        == 201
    )
    assert (
        client.post(
            f"/flows/{init_flow_id}/storage",
            json={"object_ids": [init_id], "content_type": "video/mp4"},
        ).status_code
        == 201
    )
    upload_allocated_object(client, media_id)
    upload_allocated_object(client, init_id)
    registered = client.post(
        f"/flows/{init_flow_id}/segments",
        json={
            "object_id": media_id,
            "init_object_id": init_id,
            "timerange": "[0:0_10:0)",
        },
    )
    assert registered.status_code == 201, registered.text

    changed_init = client.put(
        f"/flows/{init_flow_id}",
        json=video_flow_payload(init_flow_id, init_payload["source_id"]),
    )
    assert changed_init.status_code == 400
    assert client.put(f"/flows/{init_flow_id}", json=init_payload).status_code == 204


def test_flow_write_rejects_coerced_technical_scalars_and_indexes_valid_boolean(
    client: TestClient,
) -> None:
    invalid_flow_id = uuid4()
    invalid_payload = video_flow_payload(invalid_flow_id, uuid4())
    invalid_payload["essence_parameters"]["init_segments"] = "false"

    rejected = client.put(f"/flows/{invalid_flow_id}", json=invalid_payload)
    assert rejected.status_code == 400

    invalid_bitrate_id = uuid4()
    invalid_bitrate = video_flow_payload(invalid_bitrate_id, uuid4())
    invalid_bitrate["avg_bit_rate"] = "1234"
    assert (
        client.put(f"/flows/{invalid_bitrate_id}", json=invalid_bitrate).status_code
        == 400
    )

    flow_id = uuid4()
    payload = _set_init_segments(video_flow_payload(flow_id, uuid4()), False)
    assert client.put(f"/flows/{flow_id}", json=payload).status_code == 201
    assert (
        client.get(f"/flows/{flow_id}").json()["essence_parameters"]["init_segments"]
        is False
    )
    assert flow_id in {
        UUID(item["id"])
        for item in client.get("/flows", params={"init_segments": "false"}).json()
    }
    assert flow_id not in {
        UUID(item["id"])
        for item in client.get("/flows", params={"init_segments": "true"}).json()
    }


def test_image_flows_and_profiles_reject_init_segments(
    client: TestClient,
) -> None:
    flow_id = uuid4()
    flow_payload = image_flow_payload(flow_id, uuid4())
    flow_payload["essence_parameters"]["init_segments"] = True
    assert client.put(f"/flows/{flow_id}", json=flow_payload).status_code == 400

    profile_id = uuid4()
    profile_payload = _single_essence_profile_payload(
        profile_id,
        format=IMAGE_FORMAT,
        codec="image/jpeg",
        label="Invalid init image Profile",
    )
    profile_payload["flow_metadata"]["essence_parameters"]["init_segments"] = True
    assert (
        client.post(f"/service/profiles/{profile_id}", json=profile_payload).status_code
        == 400
    )


@pytest.mark.parametrize(
    ("field_name", "invalid_value"),
    [
        ("frame_width", "1920"),
        ("init_segments", "false"),
    ],
)
def test_profile_write_rejects_coerced_technical_scalars(
    client: TestClient,
    field_name: str,
    invalid_value: str,
) -> None:
    profile_id = uuid4()
    payload = _video_profile_payload(profile_id)
    payload["flow_metadata"]["essence_parameters"][field_name] = invalid_value

    response = client.post(f"/service/profiles/{profile_id}", json=payload)
    assert response.status_code == 400


def test_init_segments_check_and_flow_save_share_the_segment_write_lock(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    flow_id = uuid4()
    payload = video_flow_payload(flow_id, uuid4())
    assert client.put(f"/flows/{flow_id}", json=payload).status_code == 201
    register_segment(client, flow_id)

    repository = tamoss_app.state.tamoss_use_cases.repository
    original_unit_of_work = repository.unit_of_work
    original_lock = repository.lock_flow_segments
    original_has_segments = repository.has_segments
    calls: list[str] = []

    @contextmanager
    def tracked_unit_of_work() -> Iterator[object]:
        calls.append("unit-of-work-enter")
        try:
            with original_unit_of_work() as active:
                yield active
        finally:
            calls.append("unit-of-work-exit")

    def tracked_lock(requested_flow_id: UUID) -> None:
        assert calls == ["unit-of-work-enter"]
        calls.append("segment-lock")
        original_lock(requested_flow_id)

    def tracked_has_segments(requested_flow_id: UUID) -> bool:
        assert calls == ["unit-of-work-enter", "segment-lock"]
        calls.append("has-segments")
        return original_has_segments(requested_flow_id)

    monkeypatch.setattr(repository, "unit_of_work", tracked_unit_of_work)
    monkeypatch.setattr(repository, "lock_flow_segments", tracked_lock)
    monkeypatch.setattr(repository, "has_segments", tracked_has_segments)

    changed = client.put(
        f"/flows/{flow_id}",
        json=_set_init_segments(payload, True),
    )

    assert changed.status_code == 400
    assert calls == [
        "unit-of-work-enter",
        "segment-lock",
        "has-segments",
        "unit-of-work-exit",
    ]


def test_all_flow_status_values_are_filterable_through_get_and_head(
    client: TestClient,
) -> None:
    statuses = (
        "awaiting_content",
        "ingesting",
        "replication_in_progress",
        "closed_complete",
    )
    flow_ids: dict[str, UUID] = {}
    for flow_status in statuses:
        flow_id = uuid4()
        flow_ids[flow_status] = flow_id
        payload = video_flow_payload(flow_id, uuid4(), status=flow_status)
        response = client.put(f"/flows/{flow_id}", json=payload)
        assert response.status_code == 201, response.text

    for flow_status in statuses:
        listed = client.get("/flows", params={"status": flow_status})
        headed = client.head("/flows", params={"status": flow_status})
        assert listed.status_code == 200
        assert [item["id"] for item in listed.json()] == [str(flow_ids[flow_status])]
        assert headed.status_code == 200


def test_nonempty_flow_and_source_collected_by_filters_match_parent_ids(
    client: TestClient,
) -> None:
    parent_flow_id = uuid4()
    parent_source_id = uuid4()
    child_flow_id = uuid4()
    child_source_id = uuid4()
    assert (
        client.put(
            f"/flows/{parent_flow_id}",
            json=multi_flow_payload(parent_flow_id, parent_source_id),
        ).status_code
        == 201
    )
    assert (
        client.put(
            f"/flows/{child_flow_id}",
            json=video_flow_payload(child_flow_id, child_source_id),
        ).status_code
        == 201
    )
    collection = client.put(
        f"/flows/{parent_flow_id}/flow_collection",
        json=[{"id": str(child_flow_id), "role": "video"}],
    )
    assert collection.status_code == 204

    flows = client.get(
        "/flows",
        params={"collected_by_ids": str(parent_flow_id)},
    )
    sources = client.get(
        "/sources",
        params={"collected_by_ids": str(parent_source_id)},
    )
    assert flows.status_code == 200
    assert [item["id"] for item in flows.json()] == [str(child_flow_id)]
    assert sources.status_code == 200
    assert [item["id"] for item in sources.json()] == [str(child_source_id)]

    top_flows = client.get("/flows?collected_by_ids=")
    top_sources = client.get("/sources?collected_by_ids=")
    assert [item["id"] for item in top_flows.json()] == [str(parent_flow_id)]
    assert [item["id"] for item in top_sources.json()] == [str(parent_source_id)]


def test_flow_and_source_created_and_updated_sorting_defaults_and_reversal(
    client: TestClient,
) -> None:
    first_flow_id = uuid4()
    first_source_id = uuid4()
    second_flow_id = uuid4()
    second_source_id = uuid4()
    first_payload = video_flow_payload(first_flow_id, first_source_id, label="First")
    second_payload = video_flow_payload(
        second_flow_id, second_source_id, label="Second"
    )
    assert client.put(f"/flows/{first_flow_id}", json=first_payload).status_code == 201
    assert (
        client.put(f"/flows/{second_flow_id}", json=second_payload).status_code == 201
    )

    def ids(path: str, **params: str) -> list[str]:
        response = client.get(path, params=params)
        assert response.status_code == 200
        assert response.headers["x-paging-reverse-order"] == params.get(
            "reverse_order", "false"
        )
        return [item["id"] for item in response.json()]

    assert ids("/flows") == [str(second_flow_id), str(first_flow_id)]
    assert ids("/flows", sort_by="created") == [
        str(second_flow_id),
        str(first_flow_id),
    ]
    assert ids("/flows", sort_by="created", reverse_order="true") == [
        str(first_flow_id),
        str(second_flow_id),
    ]
    assert ids("/sources") == [str(second_source_id), str(first_source_id)]
    assert ids("/sources", sort_by="created") == [
        str(second_source_id),
        str(first_source_id),
    ]
    assert ids("/sources", sort_by="created", reverse_order="true") == [
        str(first_source_id),
        str(second_source_id),
    ]

    first_payload["label"] = "First updated"
    assert client.put(f"/flows/{first_flow_id}", json=first_payload).status_code == 204
    assert ids("/flows", sort_by="metadata_updated") == [
        str(first_flow_id),
        str(second_flow_id),
    ]
    assert ids(
        "/flows",
        sort_by="metadata_updated",
        reverse_order="true",
    ) == [str(second_flow_id), str(first_flow_id)]
    assert ids("/sources", sort_by="updated") == [
        str(first_source_id),
        str(second_source_id),
    ]
    assert ids("/sources", sort_by="updated", reverse_order="true") == [
        str(second_source_id),
        str(first_source_id),
    ]
