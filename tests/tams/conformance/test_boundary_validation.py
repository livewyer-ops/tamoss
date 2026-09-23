from copy import deepcopy
from uuid import uuid4

import pytest
from fastapi.testclient import TestClient

from tests.support.bbc_contract import bbc_spec, bbc_validator
from tests.tams.support import (
    PRIMARY_BACKEND_ID,
    create_video_flow,
    register_segment,
    webhook_payload,
)

pytestmark = [pytest.mark.tams_conformance, pytest.mark.tams_semantics]


def _uuid_operations() -> list[tuple[str, str, str]]:
    # Every UUID resource path declares 404. Body/query validation is separate.
    operations = []
    for path, item in bbc_spec()["paths"].items():
        for parameter in item.get("parameters", []):
            if (
                parameter.get("in") != "path"
                or parameter.get("schema", {}).get("$ref") != "schemas/uuid.json"
            ):
                continue
            for method in ("get", "head", "post", "put", "delete"):
                if method in item:
                    assert "404" in item[method]["responses"], (path, method)
                    operations.append((method, path, parameter["name"]))
    assert operations
    return operations


@pytest.mark.parametrize(("method", "path", "parameter"), _uuid_operations())
@pytest.mark.parametrize(
    "identifier",
    [
        "not-a-uuid",
        "A1111111-1111-4111-8111-111111111111",
        "11111111111141118111111111111111",
        "00000000-0000-0000-0000-000000000000",
    ],
)
def test_uuid_resource_paths_follow_bbc_pattern_and_404(
    client: TestClient, method: str, path: str, parameter: str, identifier: str
) -> None:
    assert not bbc_validator("uuid.json").is_valid(identifier)
    concrete_path = path.replace("{" + parameter + "}", identifier).replace(
        "{name}", "test"
    )
    response = client.request(
        method, concrete_path, json={} if method in {"put", "post"} else None
    )
    assert response.status_code == 404, (method, path, response.text)
    if method == "head":
        assert response.content == b""


@pytest.mark.parametrize("path", ["/flows", "/service/profiles"])
@pytest.mark.parametrize("method", ["GET", "HEAD"])
@pytest.mark.parametrize(
    "codec", ["not-a-valid-value", "custom/h264", "video/h264;profile=high"]
)
def test_codec_filters_use_bbc_mime_constraint(
    client: TestClient, path: str, method: str, codec: str
) -> None:
    assert not bbc_validator("mime-type.json").is_valid(codec)
    assert client.request(method, path, params={"codec": codec}).status_code == 400
    valid = client.request(method, path, params={"codec": "x-vendor/custom"})
    assert valid.status_code == 200
    assert valid.content == b"" if method == "HEAD" else valid.json() == []


@pytest.mark.parametrize(
    "property_name,value",
    [
        (name, value)
        for name in ("max_bit_rate", "avg_bit_rate")
        for value in (True, False, "1", "false", 1.5, None, [], {})
    ]
    + [("read_only", value) for value in (0, 1, "1", "false", 1.5, None, [], {})],
)
def test_scalar_properties_validate_json_types_before_coercion(
    client: TestClient, property_name: str, value: object
) -> None:
    flow_id, _, _ = create_video_flow(client)
    path = f"/flows/{flow_id}/{property_name}"
    before = client.get(f"/flows/{flow_id}").json()
    assert client.put(path, json=value).status_code == 400
    assert client.get(f"/flows/{flow_id}").json() == before


@pytest.mark.parametrize("property_name", ["max_bit_rate", "avg_bit_rate"])
@pytest.mark.parametrize("value", [0, 12, 12.0])
def test_integer_properties_accept_json_schema_integers(
    client: TestClient, property_name: str, value: float
) -> None:
    flow_id, _, _ = create_video_flow(client)
    path = f"/flows/{flow_id}/{property_name}"
    assert client.put(path, json=value).status_code == 204
    assert client.get(path).json() == value


@pytest.mark.parametrize("field", ["id", "source_id"])
@pytest.mark.parametrize("value", [None, "not-a-uuid", True, "missing"])
def test_flow_required_identity_is_validated_before_lookup(
    client: TestClient, field: str, value: object
) -> None:
    flow_id, _, original = create_video_flow(client)
    body = deepcopy(original)
    if value == "missing":
        body.pop(field)
    else:
        body[field] = value
    assert client.put(f"/flows/{flow_id}", json=body).status_code == 400
    assert client.get(f"/flows/{flow_id}").json() == original


def test_empty_flow_body_is_invalid_for_existing_flow(client: TestClient) -> None:
    flow_id, _, original = create_video_flow(client)
    assert client.put(f"/flows/{flow_id}", json={}).status_code == 400
    assert client.get(f"/flows/{flow_id}").json() == original


@pytest.mark.parametrize("field", ["name", "description"])
def test_service_optional_strings_distinguish_omission_and_null(
    client: TestClient, field: str
) -> None:
    original = client.get("/service").json()
    assert not bbc_validator("service-post.json").is_valid({field: None})
    assert client.post("/service", json={field: None}).status_code == 400
    assert client.get("/service").json() == original
    assert client.post("/service", json={}).status_code == 200


def test_object_instance_body_matches_exactly_one_bbc_alternative(
    client: TestClient,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    object_id = f"validation/{uuid4()}.ts"
    assert (
        client.post(
            f"/flows/{flow_id}/storage", json={"object_ids": [object_id]}
        ).status_code
        == 201
    )
    body = {
        "storage_id": str(PRIMARY_BACKEND_ID),
        "url": "https://example.test/media",
        "label": "external",
    }
    assert not bbc_validator("objects-instances-post.json").is_valid(body)
    assert client.post(f"/objects/{object_id}/instances", json=body).status_code == 400


@pytest.mark.parametrize("extra", [{"storage_id": "not-a-uuid"}, {"storage_id": None}])
def test_object_registration_uses_the_valid_schema_alternative(
    client: TestClient, extra: dict
) -> None:
    flow_id, _, _ = create_video_flow(client)
    object_id = register_segment(client, flow_id)
    body = {"url": "https://example.test/media", "label": "external", **extra}
    assert bbc_validator("objects-instances-post.json").is_valid(body)
    assert client.post(f"/objects/{object_id}/instances", json=body).status_code == 201
    media_object = client.get(
        f"/objects/{object_id}", params={"accept_get_urls": "external"}
    ).json()
    assert media_object["get_urls"][0]["url"] == body["url"]


@pytest.mark.parametrize("parameter", ["source_id", "profile_id"])
@pytest.mark.parametrize("method", ["GET", "HEAD"])
def test_query_uuid_constraints_are_not_resource_path_errors(
    client: TestClient, parameter: str, method: str
) -> None:
    for value in ("not-a-uuid", "11111111111141118111111111111111"):
        assert (
            client.request(method, "/flows", params={parameter: value}).status_code
            == 400
        )


@pytest.mark.parametrize(
    "extra", [{"url": "https://example.test/media"}, {"label": "external"}]
)
def test_controlled_object_registration_uses_the_valid_schema_alternative(
    client: TestClient, monkeypatch: pytest.MonkeyPatch, extra: dict
) -> None:
    queued = []
    objects = client.app.state.tamoss_use_cases.objects
    monkeypatch.setattr(
        objects,
        "_queue_controlled_object_copy",
        lambda object_id, storage_id: queued.append((object_id, storage_id)),
    )
    body = {"storage_id": str(PRIMARY_BACKEND_ID), **extra}
    assert bbc_validator("objects-instances-post.json").is_valid(body)
    assert (
        client.post("/objects/schema-alternative.ts/instances", json=body).status_code
        == 201
    )
    assert queued == [("schema-alternative.ts", PRIMARY_BACKEND_ID)]


def test_webhook_body_uuid_uses_body_validation(client: TestClient) -> None:
    body = webhook_payload()
    body["flow_ids"] = ["not-a-uuid"]
    assert client.post("/service/webhooks", json=body).status_code == 400


@pytest.mark.parametrize("method", ["GET", "HEAD"])
@pytest.mark.parametrize(
    "value",
    [
        "",
        "not-a-uuid",
        "11111111111141118111111111111111",
        "00000000-0000-0000-0000-000000000000",
    ],
)
def test_storage_uuid_lists_use_bbc_constraint(
    client: TestClient, method: str, value: str
) -> None:
    flow_id, _, _ = create_video_flow(client)
    object_id = register_segment(client, flow_id)
    assert not bbc_validator("uuid-list.json").is_valid(value)
    for path in (f"/flows/{flow_id}/segments", f"/objects/{object_id}"):
        assert (
            client.request(
                method, path, params={"accept_storage_ids": value}
            ).status_code
            == 400
        )
        assert (
            client.request(
                method, path, params={"accept_storage_ids": str(PRIMARY_BACKEND_ID)}
            ).status_code
            == 200
        )


def test_model_integer_fields_accept_integral_json_numbers(client: TestClient) -> None:
    flow_id, _, original = create_video_flow(client)
    body = deepcopy(original)
    body["essence_parameters"]["frame_width"] = 1920.0
    body["essence_parameters"]["frame_rate"] = {"numerator": 25.0, "denominator": 1.0}
    assert bbc_validator("flow-put.json").is_valid(body)
    assert client.put(f"/flows/{flow_id}", json=body).status_code == 204
    assert (
        client.post(f"/flows/{flow_id}/storage", json={"limit": 1.0}).status_code == 201
    )
    for invalid in (True, "1"):
        assert (
            client.post(
                f"/flows/{flow_id}/storage", json={"limit": invalid}
            ).status_code
            == 400
        )
