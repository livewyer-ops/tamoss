from __future__ import annotations

from pathlib import Path
from typing import Any
from uuid import UUID, uuid4

from fastapi.testclient import TestClient

REPO_ROOT = Path(__file__).resolve().parents[3]
BBC_API_SPEC_PATH = REPO_ROOT / "src/vendor/bbc-tams/api/TimeAddressableMediaStore.yaml"
BBC_CONTENT_FORMAT_SCHEMA_PATH = (
    REPO_ROOT / "src/vendor/bbc-tams/api/schemas/content-format.json"
)

PRIMARY_BACKEND_ID = UUID("11111111-1111-4111-8111-111111111111")
PRIMARY_BACKEND_LABEL = "tamoss.storage.primary"

VIDEO_FORMAT = "urn:x-nmos:format:video"
AUDIO_FORMAT = "urn:x-nmos:format:audio"
DATA_FORMAT = "urn:x-nmos:format:data"
MULTI_FORMAT = "urn:x-nmos:format:multi"
IMAGE_FORMAT = "urn:x-tam:format:image"

MIN_VIDEO_ESSENCE = {
    "frame_width": 1920,
    "frame_height": 1080,
    "frame_rate": {"numerator": 25, "denominator": 1},
}

MIN_IMAGE_ESSENCE = {
    "frame_width": 1920,
    "frame_height": 1080,
}


def video_flow_payload(
    flow_id: str | UUID,
    source_id: str | UUID,
    **overrides: Any,
) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "id": str(flow_id),
        "source_id": str(source_id),
        "format": VIDEO_FORMAT,
        "codec": "video/h264",
        "container": "video/mp2t",
        "essence_parameters": MIN_VIDEO_ESSENCE,
    }
    payload.update(overrides)
    return payload


def image_flow_payload(
    flow_id: str | UUID,
    source_id: str | UUID,
    **overrides: Any,
) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "id": str(flow_id),
        "source_id": str(source_id),
        "format": IMAGE_FORMAT,
        "codec": "image/jpeg",
        "container": "image/jpeg",
        "essence_parameters": MIN_IMAGE_ESSENCE,
    }
    payload.update(overrides)
    return payload


def multi_flow_payload(
    flow_id: str | UUID,
    source_id: str | UUID,
    **overrides: Any,
) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "id": str(flow_id),
        "source_id": str(source_id),
        "format": MULTI_FORMAT,
        "container": "video/mp2t",
    }
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
        json={"object_ids": object_ids},
    )
    assert response.status_code == 201
    return response.json()["media_objects"]


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
    payload: dict[str, Any] = {
        "object_id": resolved_object_id,
        "timerange": timerange,
    }
    payload.update(overrides)
    response = client.post(f"/flows/{flow_id}/segments", json=payload)
    assert response.status_code == 201
    return resolved_object_id


def assert_bbc_error(payload: dict[str, Any], error_type: str = "bad_request") -> None:
    assert payload["type"] == error_type
    assert isinstance(payload["summary"], str)
    assert isinstance(payload["time"], str)
