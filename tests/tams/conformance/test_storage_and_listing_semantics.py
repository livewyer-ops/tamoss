from __future__ import annotations

from datetime import UTC, datetime, timedelta
from urllib.parse import quote
from uuid import UUID, uuid4

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient
from tamoss.app import create_app
from tamoss.application.use_cases import TamossUseCases
from tamoss.domain.model import DeletionRequestRecord, StorageBackend
from tamoss.settings import Settings, StorageBackendSettings

from tests.support.memory_repository import FakeTamossRepository
from tests.support.object_storage import InMemoryObjectStorage
from tests.tams.support import (
    PRIMARY_BACKEND_ID,
    PRIMARY_BACKEND_LABEL,
    controlled_object_instance_payload,
    upload_allocated_object,
    video_flow_payload,
)

pytestmark = [pytest.mark.tams_conformance, pytest.mark.tams_semantics]


def test_controlled_media_and_init_urls_include_backend_tags_only_when_verbose(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    backend_tags = {"access": ["programme", "archive"], "tier": "hot"}
    flow_id, media_id, init_id = _registered_media_and_init(
        tamoss_app,
        client,
        backend_tags=backend_tags,
    )

    responses = (
        client.get(
            f"/flows/{flow_id}/segments",
            params={"verbose_storage": "true", "presigned": "false"},
        ).json()[0],
        client.get(
            f"/objects/{media_id}",
            params={"verbose_storage": "true", "presigned": "false"},
        ).json(),
    )
    for payload in responses:
        assert payload["get_urls"][0]["tags"] == backend_tags
        assert payload["init_object"]["get_urls"][0]["tags"] == backend_tags
    verbose_init = client.get(
        f"/objects/{init_id}",
        params={"verbose_storage": "true", "presigned": "false"},
    ).json()
    assert verbose_init["get_urls"][0]["tags"] == backend_tags

    non_verbose_responses = (
        client.get(f"/flows/{flow_id}/segments", params={"presigned": "false"}).json()[
            0
        ],
        client.get(f"/objects/{media_id}", params={"presigned": "false"}).json(),
    )
    for payload in non_verbose_responses:
        assert "tags" not in payload["get_urls"][0]
        assert "tags" not in payload["init_object"]["get_urls"][0]
    non_verbose_init = client.get(
        f"/objects/{init_id}", params={"presigned": "false"}
    ).json()
    assert "tags" not in non_verbose_init["get_urls"][0]


def test_storage_backend_tag_filters_cover_storage_segments_and_objects(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    backend_tags = {"access": ["programme", "archive"], "tier": "hot"}
    flow_id, media_id, _init_id = _registered_media_and_init(
        tamoss_app,
        client,
        backend_tags=backend_tags,
    )

    present = client.get(
        "/service/storage-backends",
        params={"tag_exists.access": "true"},
    )
    absent = client.get(
        "/service/storage-backends",
        params={"tag_exists.access": "false"},
    )
    missing = client.get(
        "/service/storage-backends",
        params={"tag_exists.missing": "false"},
    )
    assert [item["id"] for item in present.json()] == [str(PRIMARY_BACKEND_ID)]
    assert absent.json() == []
    assert [item["id"] for item in missing.json()] == [str(PRIMARY_BACKEND_ID)]

    endpoints = (
        (f"/flows/{flow_id}/segments", lambda payload: payload[0]),
        (f"/objects/{media_id}", lambda payload: payload),
    )
    matching_filters = (
        {"storage_backend_tag.access": "programme"},
        {"storage_backend_tag_exists.tier": "true"},
        {"storage_backend_tag_exists.missing": "false"},
    )
    for endpoint, select_payload in endpoints:
        for params in matching_filters:
            response = client.get(endpoint, params=params)
            assert response.status_code == 200, (endpoint, params, response.text)
            payload = select_payload(response.json())
            assert payload["get_urls"]
            assert payload["init_object"]["get_urls"]

        for params in (
            {"storage_backend_tag.access": "news"},
            {"storage_backend_tag_exists.access": "false"},
        ):
            response = client.get(endpoint, params=params)
            assert response.status_code == 200, (endpoint, params, response.text)
            payload = select_payload(response.json())
            assert payload["get_urls"] == []
            assert payload["init_object"]["get_urls"] == []

    assert (
        client.get(
            "/service/storage-backends",
            params={"tag_exists.access": "maybe"},
        ).status_code
        == 400
    )
    assert (
        client.get(
            f"/objects/{media_id}",
            params={"storage_backend_tag_exists.access": "maybe"},
        ).status_code
        == 400
    )


def test_storage_allocation_rejects_content_type_alias_and_conflict(
    client: TestClient,
) -> None:
    flow_id = uuid4()
    flow = video_flow_payload(flow_id, uuid4())
    flow["essence_parameters"]["init_segments"] = True
    assert client.put(f"/flows/{flow_id}", json=flow).status_code == 201

    alias_only = client.post(
        f"/flows/{flow_id}/storage",
        json={"object_ids": [f"objects/{uuid4()}.mp4"], "content-type": "video/mp4"},
    )
    conflict = client.post(
        f"/flows/{flow_id}/storage",
        json={
            "object_ids": [f"objects/{uuid4()}.mp4"],
            "content_type": "video/mp4",
            "content-type": "video/quicktime",
        },
    )
    canonical = client.post(
        f"/flows/{flow_id}/storage",
        json={
            "object_ids": [f"objects/{uuid4()}.mp4"],
            "content_type": "video/mp4",
        },
    )

    assert alias_only.status_code == 400
    assert conflict.status_code == 400
    assert canonical.status_code == 201
    assert canonical.json()["media_objects"][0]["put_url"]["content-type"] == (
        "video/mp4"
    )


def test_media_instance_copy_and_delete_do_not_cascade_to_init_object() -> None:
    settings = Settings(
        auth_required=False,
        storage_backend=StorageBackendSettings(
            id=PRIMARY_BACKEND_ID,
            label=PRIMARY_BACKEND_LABEL,
            provider="example",
            region="local",
            store_product="s3",
            default_storage=True,
            bucket_name="media-primary",
        ),
    )
    primary = settings.storage_backend_record()
    assert primary is not None
    secondary = StorageBackend(
        id=UUID("22222222-2222-4222-8222-222222222222"),
        label="tamoss.us-west-2:s3:tamoss-secondary",
        provider="example",
        region="local",
        store_product="s3",
        bucket_name="media-secondary",
    )
    repository = FakeTamossRepository(
        primary,
        storage_backends=[primary, secondary],
    )
    object_storage = InMemoryObjectStorage()
    app = create_app(
        settings,
        use_cases=TamossUseCases(
            repository=repository,
            object_storage=object_storage,
            settings=settings,
        ),
    )

    with TestClient(app) as client:
        _flow_id, media_id, init_id = _registered_media_and_init(
            app,
            client,
            backend_tags={},
        )
        encoded_media_id = quote(media_id, safe="")
        init_before = client.get(
            f"/objects/{init_id}",
            params={"verbose_storage": "true", "presigned": "false"},
        )
        copied = client.post(
            f"/objects/{encoded_media_id}/instances",
            json=controlled_object_instance_payload(secondary.id),
        )

        queued = repository.list_object_copies()
        assert [
            (item.object_id, item.destination_storage_backend_id) for item in queued
        ] == [(media_id, secondary.id)]
        assert copied.status_code == 201
        assert (
            app.state.tamoss_use_cases.objects.process_pending_object_copies(
                worker_id="copy-worker",
                lease_seconds=60,
            )
            == 1
        )

        media_secondary = client.get(
            f"/objects/{media_id}",
            params={
                "accept_storage_ids": str(secondary.id),
                "verbose_storage": "true",
                "presigned": "false",
            },
        )
        init_secondary = client.get(
            f"/objects/{init_id}",
            params={
                "accept_storage_ids": str(secondary.id),
                "verbose_storage": "true",
                "presigned": "false",
            },
        )
        deleted = client.delete(
            f"/objects/{encoded_media_id}/instances",
            params={"storage_id": str(primary.id)},
        )
        init_after_delete = client.get(
            f"/objects/{init_id}",
            params={
                "accept_storage_ids": str(primary.id),
                "verbose_storage": "true",
                "presigned": "false",
            },
        )
        media_primary_after_delete = client.get(
            f"/objects/{media_id}",
            params={"accept_storage_ids": str(primary.id)},
        )
        cleanup_count = (
            app.state.tamoss_use_cases.deletion.process_pending_object_cleanups(
                worker_id="cleanup-worker",
                lease_seconds=60,
            )
        )

    assert init_before.json()["get_urls"][0]["storage_id"] == str(primary.id)
    assert media_secondary.json()["get_urls"][0]["storage_id"] == str(secondary.id)
    assert init_secondary.json()["get_urls"] == []
    assert object_storage.read(init_id, backend=secondary) is None
    assert deleted.status_code == 204
    assert init_after_delete.json()["get_urls"][0]["storage_id"] == str(primary.id)
    assert media_primary_after_delete.json()["get_urls"] == []
    assert cleanup_count == 1
    assert object_storage.read(media_id, backend=primary) is None
    assert object_storage.read(media_id, backend=secondary) == b"segment"
    assert object_storage.read(init_id, backend=primary) == b"segment"


def test_delete_request_created_sort_uses_resource_timestamp(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    repository = tamoss_app.state.tamoss_use_cases.repository
    base = datetime(2026, 8, 1, tzinfo=UTC)
    requests = [
        DeletionRequestRecord(
            id=UUID(f"00000000-0000-4000-8000-{index:012d}"),
            flow_id=uuid4(),
            timerange_to_delete="[0:0_10:0)",
            delete_flow=False,
            status="created",
            created=base + timedelta(hours=offset),
            updated=base + timedelta(hours=offset),
        )
        for index, offset in ((1, 1), (3, 3), (2, 2))
    ]
    for request in requests:
        repository.save_delete_request(request)

    newest_first = client.get("/flow-delete-requests", params={"sort_by": "created"})
    oldest_first = client.get(
        "/flow-delete-requests",
        params={"sort_by": "created", "reverse_order": "true"},
    )

    assert [item["id"] for item in newest_first.json()] == [
        str(requests[1].id),
        str(requests[2].id),
        str(requests[0].id),
    ]
    assert [item["id"] for item in oldest_first.json()] == [
        str(requests[0].id),
        str(requests[2].id),
        str(requests[1].id),
    ]


def _registered_media_and_init(
    tamoss_app: FastAPI,
    client: TestClient,
    *,
    backend_tags: dict[str, str | list[str]],
) -> tuple[UUID, str, str]:
    repository = tamoss_app.state.tamoss_use_cases.repository
    backend = repository.get_storage_backend(PRIMARY_BACKEND_ID)
    assert backend is not None
    backend.tags = backend_tags

    flow_id = uuid4()
    flow = video_flow_payload(flow_id, uuid4())
    flow["essence_parameters"]["init_segments"] = True
    assert client.put(f"/flows/{flow_id}", json=flow).status_code == 201

    media_id = f"objects/{uuid4()}.m4s"
    init_id = f"objects/{uuid4()}.mp4"
    media_allocation = client.post(
        f"/flows/{flow_id}/storage",
        json={"object_ids": [media_id], "presigned": False},
    )
    init_allocation = client.post(
        f"/flows/{flow_id}/storage",
        json={
            "object_ids": [init_id],
            "content_type": "video/mp4",
            "presigned": False,
        },
    )
    assert media_allocation.status_code == 201, media_allocation.text
    assert init_allocation.status_code == 201, init_allocation.text
    upload_allocated_object(client, media_id)
    upload_allocated_object(client, init_id)

    registered = client.post(
        f"/flows/{flow_id}/segments",
        json={
            "object_id": media_id,
            "init_object_id": init_id,
            "timerange": "[0:0_10:0)",
        },
    )
    assert registered.status_code == 201, registered.text
    return flow_id, media_id, init_id
