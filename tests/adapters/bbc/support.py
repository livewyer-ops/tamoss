from __future__ import annotations

from typing import Any
from uuid import UUID, uuid4

from fastapi.testclient import TestClient

from tests.support import paths
from tests.support.fixtures import load_json_fixture

REPO_ROOT = paths.REPO_ROOT
BBC_API_SPEC_PATH = paths.BBC_API_SPEC_PATH
BBC_CONTENT_FORMAT_SCHEMA_PATH = paths.BBC_CONTENT_FORMAT_SCHEMA_PATH

PRIMARY_BACKEND_ID = UUID("11111111-1111-4111-8111-111111111111")
PRIMARY_BACKEND_LABEL = "tamoss.us-east-1:s3:tamoss-primary"

VIDEO_FORMAT = "urn:x-nmos:format:video"
IMAGE_FORMAT = "urn:x-tam:format:image"


def video_flow_payload(
    flow_id: str | UUID,
    source_id: str | UUID,
    **overrides: Any,
) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/video_flow_payload.json")
    payload["id"] = str(flow_id)
    payload["source_id"] = str(source_id)
    payload.update(overrides)
    return payload


def image_flow_payload(
    flow_id: str | UUID,
    source_id: str | UUID,
    **overrides: Any,
) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/image_flow_payload.json")
    payload["id"] = str(flow_id)
    payload["source_id"] = str(source_id)
    payload.update(overrides)
    return payload


def multi_flow_payload(
    flow_id: str | UUID,
    source_id: str | UUID,
    **overrides: Any,
) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/multi_flow_payload.json")
    payload["id"] = str(flow_id)
    payload["source_id"] = str(source_id)
    payload.update(overrides)
    return payload


def segment_payload(
    object_id: str,
    timerange: str = "[0:0_10:0)",
    **overrides: Any,
) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/segment_payload.json")
    payload["object_id"] = object_id
    payload["timerange"] = timerange
    payload.update(overrides)
    return payload


def segment_wrapper_payload(segments: list[dict[str, Any]]) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/segment_wrapper_payload.json")
    payload["segments"] = segments
    return payload


def storage_allocation_payload(
    object_ids: list[str],
    *,
    storage_id: UUID | str | None = None,
    limit: int | None = None,
) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/storage_allocation.json")
    payload["object_ids"] = object_ids
    if storage_id is not None:
        payload["storage_id"] = str(storage_id)
    if limit is not None:
        payload["limit"] = limit
    return payload


def controlled_object_instance_payload(storage_id: UUID | str) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/controlled_object_instance.json")
    payload["storage_id"] = str(storage_id)
    return payload


def external_object_instance(url: str, *, label: str = "external") -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/external_object_instance.json")
    payload["url"] = url
    payload["label"] = label
    return payload


def flow_collection_item(flow_id: UUID | str, *, role: str = "video") -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/flow_collection_item.json")
    payload["id"] = str(flow_id)
    payload["role"] = role
    return payload


def webhook_payload(**overrides: Any) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/webhook_minimal.json")
    payload.update(overrides)
    return payload


def create_video_flow(
    client: TestClient,
    *,
    flow_id: UUID | None = None,
    source_id: UUID | None = None,
    **overrides: Any,
) -> tuple[UUID, UUID, dict[str, Any]]:
    resolved_flow_id = flow_id or uuid4()
    resolved_source_id = source_id or uuid4()
    response = client.put(
        f"/flows/{resolved_flow_id}",
        json=video_flow_payload(resolved_flow_id, resolved_source_id, **overrides),
    )
    assert response.status_code == 201
    return resolved_flow_id, resolved_source_id, response.json()


def allocate_objects(
    client: TestClient,
    flow_id: UUID,
    object_ids: list[str],
) -> list[dict[str, Any]]:
    response = client.post(
        f"/flows/{flow_id}/storage",
        json=storage_allocation_payload(object_ids),
    )
    assert response.status_code == 201
    return response.json()["media_objects"]


def upload_allocated_object(
    client: TestClient,
    object_id: str,
    *,
    storage_id: UUID = PRIMARY_BACKEND_ID,
    data: bytes = b"segment",
) -> None:
    use_cases = client.app.state.tamoss_use_cases
    backend = use_cases.repository.get_storage_backend(storage_id)
    assert backend is not None
    use_cases.object_storage.write(object_id, data, backend=backend)


def register_segment(
    client: TestClient,
    flow_id: UUID,
    *,
    object_id: str | None = None,
    timerange: str = "[0:0_10:0)",
    **overrides: Any,
) -> str:
    resolved_object_id = object_id or f"bbc/{uuid4()}.ts"
    allocate_objects(client, flow_id, [resolved_object_id])
    upload_allocated_object(client, resolved_object_id)
    payload = segment_payload(resolved_object_id, timerange)
    payload.update(overrides)
    response = client.post(f"/flows/{flow_id}/segments", json=payload)
    assert response.status_code == 201
    return resolved_object_id


def assert_bbc_error(payload: dict[str, Any], error_type: str = "bad_request") -> None:
    assert payload["type"] == error_type
    assert isinstance(payload["summary"], str)
    assert isinstance(payload["time"], str)
