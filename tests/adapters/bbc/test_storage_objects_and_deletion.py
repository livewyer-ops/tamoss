from __future__ import annotations

from uuid import UUID, uuid4

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient
from tamoss.app import create_app
from tamoss.application.use_cases import TamossUseCases, storage_backend_from_settings
from tamoss.domain.model import StorageBackend
from tamoss.settings import Settings, StorageBackendSettings

from tests.adapters.bbc.support import (
    PRIMARY_BACKEND_ID,
    PRIMARY_BACKEND_LABEL,
    create_video_flow,
    register_segment,
    video_flow_payload,
)
from tests.support.memory_repository import InMemoryRepository

pytestmark = pytest.mark.bbc


def test_storage_allocation_and_object_instance_lifecycle(
    client: TestClient,
) -> None:
    flow_id = uuid4()
    source_id = uuid4()
    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(flow_id, source_id),
    )
    assert created.status_code == 201
    object_id = f"bbc/{uuid4()}.ts"

    invalid_allocation = client.post(
        f"/flows/{flow_id}/storage",
        json={"limit": 1, "object_ids": [object_id]},
    )
    assert invalid_allocation.status_code == 400

    allocated = client.post(
        f"/flows/{flow_id}/storage",
        json={"object_ids": [object_id], "storage_id": str(PRIMARY_BACKEND_ID)},
    )
    assert allocated.status_code == 201
    media_object = allocated.json()["media_objects"][0]
    assert media_object["object_id"] == object_id
    assert media_object["put_url"]["content-type"] == "video/mp2t"
    duplicate = client.post(
        f"/flows/{flow_id}/storage",
        json={"object_ids": [object_id], "storage_id": str(PRIMARY_BACKEND_ID)},
    )
    assert duplicate.status_code == 400

    segment = client.post(
        f"/flows/{flow_id}/segments",
        json={"object_id": object_id, "timerange": "[0:0_10:0)"},
    )
    assert segment.status_code == 201

    obj = client.get(
        f"/objects/{object_id}",
        params={
            "accept_get_urls": PRIMARY_BACKEND_LABEL,
            "accept_storage_ids": str(PRIMARY_BACKEND_ID),
            "presigned": "true",
            "verbose_storage": "true",
        },
    )
    assert obj.status_code == 200
    assert obj.headers["x-paging-count"] == "1"
    assert obj.json()["id"] == object_id
    assert obj.json()["first_referenced_by_flow"] == str(flow_id)
    assert obj.json()["referenced_by_flows"] == [str(flow_id)]
    assert obj.json()["get_urls"][0]["storage_id"] == str(PRIMARY_BACKEND_ID)
    assert obj.json()["get_urls"][0]["controlled"] is True
    non_verbose = client.get(
        f"/objects/{object_id}",
        params={
            "accept_get_urls": PRIMARY_BACKEND_LABEL,
            "accept_storage_ids": str(PRIMARY_BACKEND_ID),
        },
    )
    assert non_verbose.status_code == 200
    assert set(non_verbose.json()["get_urls"][0]) == {"url", "label", "presigned"}

    uncontrolled = client.post(
        f"/objects/{object_id}/instances",
        json={"url": "https://media.example.test/object.ts", "label": "external"},
    )
    assert uncontrolled.status_code == 201
    external = client.get(
        f"/objects/{object_id}",
        params={"accept_get_urls": "external", "verbose_storage": "true"},
    )
    assert external.status_code == 200
    assert external.json()["get_urls"] == [
        {
            "url": "https://media.example.test/object.ts",
            "label": "external",
            "presigned": False,
            "controlled": False,
        }
    ]

    duplicate_controlled = client.post(
        f"/objects/{object_id}/instances",
        json={"storage_id": str(PRIMARY_BACKEND_ID)},
    )
    assert duplicate_controlled.status_code == 400

    deleted_external = client.delete(
        f"/objects/{object_id}/instances", params={"label": "external"}
    )
    assert deleted_external.status_code == 204

    final_instance = client.delete(
        f"/objects/{object_id}/instances",
        params={"storage_id": str(PRIMARY_BACKEND_ID)},
    )
    assert final_instance.status_code == 400


def test_hidden_storage_proxy_route_is_not_registered(client: TestClient) -> None:
    assert client.put("/_tamoss/storage/object.ts", content=b"media").status_code == 404


def test_uncontrolled_instance_label_cannot_reuse_controlled_url_labels() -> None:
    settings = Settings(
        auth_required=False,
        database_url=None,
        public_base_url="http://testserver",
        storage_backend=StorageBackendSettings(
            id=PRIMARY_BACKEND_ID,
            label="example.primary:s3:media",
            provider="example",
            region="local",
            store_product="s3",
            default_storage=True,
            bucket_name="media-primary",
        ),
    )
    storage_backend = storage_backend_from_settings(settings)
    assert storage_backend is not None
    app = create_app(
        settings,
        use_cases=TamossUseCases(
            repository=InMemoryRepository(storage_backend),
            object_storage=_PredictableObjectStorage(),
            settings=settings,
        ),
    )
    with TestClient(app) as client:
        flow_id = uuid4()
        source_id = uuid4()
        created = client.put(
            f"/flows/{flow_id}",
            json=video_flow_payload(flow_id, source_id),
        )
        assert created.status_code == 201
        object_id = f"bbc/{uuid4()}.ts"
        allocated = client.post(
            f"/flows/{flow_id}/storage",
            json={"object_ids": [object_id], "storage_id": str(PRIMARY_BACKEND_ID)},
        )
        assert allocated.status_code == 201
        assert (
            client.post(
                f"/flows/{flow_id}/segments",
                json={"object_id": object_id, "timerange": "[0:0_10:0)"},
            ).status_code
            == 201
        )

        direct_label = client.post(
            f"/objects/{object_id}/instances",
            json={
                "url": "https://media.example.test/direct-label.ts",
                "label": "example.primary:s3:media",
            },
        )

    assert direct_label.status_code == 400


def test_uncontrolled_only_object_cannot_be_copied_to_controlled_storage(
    client: TestClient,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    object_id = f"external/{uuid4()}.ts"
    registered = client.post(
        f"/flows/{flow_id}/segments",
        json={
            "object_id": object_id,
            "timerange": "[0:0_10:0)",
            "get_urls": [
                {
                    "url": "https://cdn.example.test/external.ts",
                    "label": "external",
                }
            ],
        },
    )
    assert registered.status_code == 201

    copied = client.post(
        f"/objects/{object_id}/instances",
        json={"storage_id": str(PRIMARY_BACKEND_ID)},
    )
    assert copied.status_code == 400

    controlled = client.get(
        f"/objects/{object_id}",
        params={
            "accept_storage_ids": str(PRIMARY_BACKEND_ID),
            "verbose_storage": "true",
        },
    )
    assert controlled.status_code == 200
    assert controlled.json()["get_urls"] == []

    uncontrolled = client.get(
        f"/objects/{object_id}",
        params={"accept_get_urls": "external", "verbose_storage": "true"},
    )
    assert uncontrolled.status_code == 200
    assert uncontrolled.json()["get_urls"] == [
        {
            "url": "https://cdn.example.test/external.ts",
            "label": "external",
            "presigned": False,
            "controlled": False,
        }
    ]


def test_uncontrolled_object_instances_are_deduplicated_by_label(
    client: TestClient,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    object_id = register_segment(client, flow_id, object_id=f"bbc/{uuid4()}.ts")
    instance = {"url": "https://media.example.test/object.ts", "label": "external"}

    assert (
        client.post(f"/objects/{object_id}/instances", json=instance).status_code == 201
    )
    assert (
        client.post(f"/objects/{object_id}/instances", json=instance).status_code == 201
    )
    conflicting = client.post(
        f"/objects/{object_id}/instances",
        json={"url": "https://media.example.test/other.ts", "label": "external"},
    )

    listed = client.get(f"/objects/{object_id}", params={"accept_get_urls": "external"})
    assert conflicting.status_code == 400
    assert [item["url"] for item in listed.json()["get_urls"]] == [instance["url"]]


def test_reused_object_cannot_change_object_level_properties(
    client: TestClient,
) -> None:
    first_flow_id, _, _ = create_video_flow(client)
    object_id = register_segment(
        client,
        first_flow_id,
        object_id=f"bbc/{uuid4()}.ts",
        key_frame_count=5,
    )
    second_flow_id, _, _ = create_video_flow(client)

    changed_keyframes = client.post(
        f"/flows/{second_flow_id}/segments",
        json={
            "object_id": object_id,
            "timerange": "[0:0_10:0)",
            "key_frame_count": 6,
        },
    )
    changed_timerange = client.post(
        f"/flows/{second_flow_id}/segments",
        json={
            "object_id": object_id,
            "timerange": "[10:0_20:0)",
            "object_timerange": "[100:0_110:0)",
        },
    )

    assert changed_keyframes.status_code == 400
    assert changed_timerange.status_code == 400


def test_segment_deletion_request_lifecycle(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    first_object = register_segment(
        client,
        flow_id,
        object_id=f"bbc/{uuid4()}.ts",
        timerange="[0:0_10:0)",
    )
    second_object = register_segment(
        client,
        flow_id,
        object_id=f"bbc/{uuid4()}.ts",
        timerange="[10:0_20:0)",
    )

    partial = client.delete(
        f"/flows/{flow_id}/segments",
        params={"timerange": "[5:0_15:0)"},
    )
    assert partial.status_code == 204
    assert client.get("/flow-delete-requests").json() == []

    accepted = client.delete(
        f"/flows/{flow_id}/segments",
        params={"timerange": "[0:0_10:0)"},
    )
    assert accepted.status_code == 202
    assert accepted.headers["location"].startswith(
        "http://testserver/flow-delete-requests/"
    )
    request_id = accepted.json()["id"]
    assert accepted.json()["flow_id"] == str(flow_id)
    assert accepted.json()["delete_flow"] is False
    assert accepted.json()["timerange_to_delete"] == "[0:0_10:0)"
    assert accepted.json()["status"] == "created"

    listed_requests = client.get("/flow-delete-requests")
    assert listed_requests.status_code == 200
    assert [item["id"] for item in listed_requests.json()] == [request_id]
    request_head = client.head(f"/flow-delete-requests/{request_id}")
    assert request_head.status_code == 200

    assert tamoss_app.state.tamoss_use_cases.process_pending_delete_requests() == 1
    completed = client.get(f"/flow-delete-requests/{request_id}")
    assert completed.status_code == 200
    assert completed.json()["status"] == "done"
    remaining = client.get(f"/flows/{flow_id}/segments")
    assert remaining.status_code == 200
    assert [item["object_id"] for item in remaining.json()] == [second_object]
    assert client.get(f"/objects/{first_object}").status_code == 404


def test_flow_deletion_request_removes_flow_segments_and_orphan_source(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    source_id = uuid4()
    flow_id, _, _ = create_video_flow(client, source_id=source_id)
    object_id = register_segment(client, flow_id, object_id=f"bbc/{uuid4()}.ts")

    accepted = client.delete(f"/flows/{flow_id}")
    assert accepted.status_code == 202
    request_id = accepted.json()["id"]
    assert accepted.json()["delete_flow"] is True
    assert accepted.json()["timerange_to_delete"] == "[0:0_10:0)"

    assert client.get(f"/flows/{flow_id}").status_code == 200
    assert tamoss_app.state.tamoss_use_cases.process_pending_delete_requests() == 1
    assert client.get(f"/flows/{flow_id}").status_code == 404
    assert client.get(f"/sources/{source_id}").status_code == 404
    assert client.get(f"/objects/{object_id}").status_code == 404
    assert client.get(f"/flow-delete-requests/{request_id}").json()["status"] == "done"


def test_flow_without_segments_deletes_synchronously(client: TestClient) -> None:
    flow_id, source_id, _ = create_video_flow(client)

    deleted = client.delete(f"/flows/{flow_id}")
    assert deleted.status_code == 204
    assert client.get(f"/flows/{flow_id}").status_code == 404
    assert client.get(f"/sources/{source_id}").status_code == 404


class _PredictableObjectStorage:
    def __init__(self) -> None:
        self._objects: dict[tuple[UUID, str], bytes] = {}

    def build_put_request(
        self, *, object_id: str, flow_container: str, backend: StorageBackend
    ) -> dict:
        return {
            "url": f"https://objects.example.test/{backend.id}/{object_id}",
            "content-type": flow_container,
            "headers": {"Content-Type": flow_container},
        }

    def build_get_url(self, *, object_id: str, backend: StorageBackend) -> str:
        return f"https://objects.example.test/{backend.id}/{object_id}"

    def build_get_urls(self, *, object_id: str, backend: StorageBackend) -> list[dict]:
        return [
            {
                "url": self.build_get_url(object_id=object_id, backend=backend),
                "label": backend.label,
                "presigned": True,
            },
        ]

    def write(
        self, object_id: str, data: bytes, *, backend: StorageBackend | None = None
    ) -> None:
        assert backend is not None
        self._objects[(backend.id, object_id)] = data

    def read(
        self, object_id: str, *, backend: StorageBackend | None = None
    ) -> bytes | None:
        assert backend is not None
        return self._objects.get((backend.id, object_id))

    def iter_chunks(
        self,
        object_id: str,
        *,
        backend: StorageBackend | None = None,
        chunk_size: int = 1024 * 1024,
    ):
        body = self.read(object_id, backend=backend)
        if body is None:
            return None
        return iter([body])

    def delete(self, object_id: str, *, backend: StorageBackend | None = None) -> None:
        assert backend is not None
        self._objects.pop((backend.id, object_id), None)
