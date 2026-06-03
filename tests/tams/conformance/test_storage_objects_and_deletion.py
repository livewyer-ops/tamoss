from __future__ import annotations

from datetime import timedelta
from urllib.parse import quote
from uuid import UUID, uuid4

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient
from tamoss.app import create_app
from tamoss.application.use_cases import TamossUseCases
from tamoss.domain.model import (
    MediaObjectRecord,
    ObjectCopyRecord,
    ObjectInstance,
    StorageBackend,
    utc_now,
)
from tamoss.settings import Settings, StorageBackendSettings

from tests.support.memory_repository import FakeTamossRepository
from tests.support.object_storage import InMemoryObjectStorage
from tests.tams.support import (
    PRIMARY_BACKEND_ID,
    PRIMARY_BACKEND_LABEL,
    controlled_object_instance_payload,
    create_video_flow,
    external_object_instance,
    register_segment,
    segment_payload,
    storage_allocation_payload,
    upload_allocated_object,
    video_flow_payload,
)

pytestmark = pytest.mark.tams_conformance

SECONDARY_BACKEND_ID = UUID("22222222-2222-4222-8222-222222222222")


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
        json=storage_allocation_payload([object_id], limit=1),
    )
    assert invalid_allocation.status_code == 400

    allocated = client.post(
        f"/flows/{flow_id}/storage",
        json=storage_allocation_payload([object_id], storage_id=PRIMARY_BACKEND_ID),
    )
    assert allocated.status_code == 201
    media_object = allocated.json()["media_objects"][0]
    assert media_object["object_id"] == object_id
    assert media_object["storage_id"] == str(PRIMARY_BACKEND_ID)
    assert media_object["put_url"]["content-type"] == "video/mp2t"
    duplicate = client.post(
        f"/flows/{flow_id}/storage",
        json=storage_allocation_payload([object_id], storage_id=PRIMARY_BACKEND_ID),
    )
    assert duplicate.status_code == 400

    segment = client.post(
        f"/flows/{flow_id}/segments",
        json=segment_payload(object_id),
    )
    assert segment.status_code == 400

    upload_allocated_object(client, object_id)
    other_flow_id, _, _ = create_video_flow(client)

    wrong_flow_segment = client.post(
        f"/flows/{other_flow_id}/segments",
        json=segment_payload(object_id),
    )
    assert wrong_flow_segment.status_code == 400

    segment = client.post(
        f"/flows/{flow_id}/segments",
        json=segment_payload(object_id),
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
    assert obj.json()["get_urls"][0]["label"] == PRIMARY_BACKEND_LABEL
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

    reused = client.post(
        f"/flows/{other_flow_id}/segments",
        json=segment_payload(object_id),
    )
    assert reused.status_code == 201

    uncontrolled = client.post(
        f"/objects/{quote(object_id, safe='')}/instances",
        json=external_object_instance("https://media.example.test/object.ts"),
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
        f"/objects/{quote(object_id, safe='')}/instances",
        json=controlled_object_instance_payload(PRIMARY_BACKEND_ID),
    )
    assert duplicate_controlled.status_code == 400

    deleted_external = client.delete(
        f"/objects/{quote(object_id, safe='')}/instances", params={"label": "external"}
    )
    assert deleted_external.status_code == 204

    final_instance = client.delete(
        f"/objects/{quote(object_id, safe='')}/instances",
        params={"storage_id": str(PRIMARY_BACKEND_ID)},
    )
    assert final_instance.status_code == 400


def test_get_url_filters_apply_to_objects_and_segments(client: TestClient) -> None:
    flow_id, object_id = _object_with_external_instance(client)
    use_cases = client.app.state.tamoss_use_cases
    assert isinstance(use_cases.object_storage, InMemoryObjectStorage)

    cases = [
        ({"accept_get_urls": ""}, []),
        ({"accept_get_urls": "external"}, [("external", False)]),
        (
            {"accept_get_urls": PRIMARY_BACKEND_LABEL, "presigned": "false"},
            [(PRIMARY_BACKEND_LABEL, False)],
        ),
        (
            {"accept_get_urls": PRIMARY_BACKEND_LABEL, "presigned": "true"},
            [(PRIMARY_BACKEND_LABEL, True)],
        ),
        ({"accept_get_urls": "missing"}, []),
        ({"accept_storage_ids": str(uuid4())}, []),
    ]
    endpoints = [
        ("object", f"/objects/{object_id}"),
        ("segment", f"/flows/{flow_id}/segments"),
    ]

    for endpoint_name, endpoint in endpoints:
        for params, expected in cases:
            use_cases.object_storage.built_get_url_batches.clear()
            response = client.get(endpoint, params=params)
            assert response.status_code == 200, (endpoint_name, params)
            get_urls = _payload_get_urls(endpoint_name, response.json())
            assert [
                (item.get("label"), item.get("presigned")) for item in get_urls
            ] == expected, (endpoint_name, params)
            if params.get("accept_get_urls") == "":
                assert use_cases.object_storage.built_get_url_batches == []


def test_object_instance_updates_are_visible_to_all_reused_object_segments(
    client: TestClient,
) -> None:
    first_flow_id, object_id = _object_with_external_instance(client)
    second_flow_id, _, _ = create_video_flow(client)
    reused = client.post(
        f"/flows/{second_flow_id}/segments",
        json=segment_payload(object_id, "[10:0_20:0)"),
    )
    assert reused.status_code == 201

    for flow_id in (first_flow_id, second_flow_id):
        response = client.get(
            f"/flows/{flow_id}/segments",
            params={"accept_get_urls": "external"},
        )
        assert response.status_code == 200
        assert response.json()[0]["get_urls"] == [
            {
                "url": "https://media.example.test/object.ts",
                "label": "external",
                "presigned": False,
            }
        ]

    deleted = client.delete(
        f"/objects/{quote(object_id, safe='')}/instances",
        params={"label": "external"},
    )
    assert deleted.status_code == 204
    for flow_id in (first_flow_id, second_flow_id):
        response = client.get(
            f"/flows/{flow_id}/segments",
            params={"accept_get_urls": "external"},
        )
        assert response.status_code == 200
        assert response.json()[0]["get_urls"] == []


def test_allocated_object_is_not_visible_to_object_instance_endpoint(
    client: TestClient,
) -> None:
    use_cases = client.app.state.tamoss_use_cases
    flow_id, _, _ = create_video_flow(client)
    object_id = f"bbc/{uuid4()}.ts"
    backend = use_cases.repository.default_storage_backend()
    assert backend is not None
    allocated = client.post(
        f"/flows/{flow_id}/storage",
        json=storage_allocation_payload([object_id]),
    )
    assert allocated.status_code == 201
    use_cases.object_storage.write(object_id, b"orphan", backend=backend)

    deleted = client.delete(
        f"/objects/{quote(object_id, safe='')}/instances",
        params={"storage_id": str(PRIMARY_BACKEND_ID)},
    )
    cleanups = use_cases.repository.list_object_cleanups(statuses={"pending"})

    assert deleted.status_code == 404
    assert use_cases.repository.get_object(object_id) is not None
    assert cleanups == []
    assert use_cases.object_storage.read(object_id, backend=backend) == b"orphan"


def test_worker_cleans_up_stale_allocated_controlled_object(
    client: TestClient,
) -> None:
    use_cases = client.app.state.tamoss_use_cases
    flow_id, _, _ = create_video_flow(client)
    object_id = f"bbc/{uuid4()}.ts"
    backend = use_cases.repository.default_storage_backend()
    assert backend is not None
    allocated = client.post(
        f"/flows/{flow_id}/storage",
        json=storage_allocation_payload([object_id]),
    )
    assert allocated.status_code == 201
    use_cases.object_storage.write(object_id, b"stale", backend=backend)

    media_object = use_cases.repository.get_object(object_id)
    assert media_object is not None
    media_object.created = utc_now() - timedelta(seconds=301)
    use_cases.repository.save_object(media_object)

    queued = use_cases.deletion.queue_stale_allocated_object_cleanups(max_objects=10)
    processed = use_cases.deletion.process_pending_object_cleanups(
        worker_id="cleanup-worker",
        lease_seconds=60,
    )

    assert queued == 1
    assert processed == 1
    assert use_cases.repository.get_object(object_id) is None
    assert use_cases.object_storage.read(object_id, backend=backend) is None


def test_storage_allocation_rejects_high_limits_and_invalid_object_ids(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    tamoss_app.state.tamoss_settings.storage_allocation_max_objects = 1
    flow_id, _, _ = create_video_flow(client)

    too_many_by_count = client.post(
        f"/flows/{flow_id}/storage",
        json=storage_allocation_payload([], limit=2),
    )
    too_many_by_id = client.post(
        f"/flows/{flow_id}/storage",
        json=storage_allocation_payload(["one.ts", "two.ts"]),
    )
    invalid_id = client.post(
        f"/flows/{flow_id}/storage",
        json=storage_allocation_payload(["../escape.ts"]),
    )

    assert too_many_by_count.status_code == 400
    assert too_many_by_id.status_code == 400
    assert invalid_id.status_code == 400


def test_uncontrolled_instance_label_cannot_reuse_controlled_url_labels() -> None:
    settings = Settings(
        auth_required=False,
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
    storage_backend = settings.storage_backend_record()
    assert storage_backend is not None
    app = create_app(
        settings,
        use_cases=TamossUseCases(
            repository=FakeTamossRepository(storage_backend),
            object_storage=InMemoryObjectStorage(
                metadata_etag_prefix="predictable",
                quote_object_ids=False,
            ),
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
            json=storage_allocation_payload(
                [object_id],
                storage_id=PRIMARY_BACKEND_ID,
            ),
        )
        assert allocated.status_code == 201
        upload_allocated_object(client, object_id)
        assert (
            client.post(
                f"/flows/{flow_id}/segments",
                json=segment_payload(object_id),
            ).status_code
            == 201
        )

        direct_label = client.post(
            f"/objects/{quote(object_id, safe='')}/instances",
            json=external_object_instance(
                "https://media.example.test/direct-label.ts",
                label="example.primary:s3:media",
            ),
        )

    assert direct_label.status_code == 400


def test_uncontrolled_object_instance_requires_label(client: TestClient) -> None:
    flow_id, _, _ = create_video_flow(client)
    object_id = register_segment(client, flow_id, object_id=f"bbc/{uuid4()}.ts")
    body = external_object_instance("https://media.example.test/unlabelled.ts")
    body.pop("label")

    response = client.post(
        f"/objects/{quote(object_id, safe='')}/instances",
        json=body,
    )

    assert response.status_code == 400


def test_uncontrolled_only_object_cannot_be_copied_to_controlled_storage(
    client: TestClient,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    object_id = f"external/{uuid4()}.ts"
    registered = client.post(
        f"/flows/{flow_id}/segments",
        json=segment_payload(
            object_id,
            get_urls=[
                {
                    "url": "https://cdn.example.test/external.ts",
                    "label": "external",
                }
            ],
        ),
    )
    assert registered.status_code == 201

    copied = client.post(
        f"/objects/{quote(object_id, safe='')}/instances",
        json=controlled_object_instance_payload(PRIMARY_BACKEND_ID),
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


def test_controlled_object_copy_is_advertised_after_worker_completion() -> None:
    settings = Settings(
        auth_required=False,
        public_base_url="http://testserver",
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
        id=SECONDARY_BACKEND_ID,
        label="tamoss.us-west-2:s3:tamoss-secondary",
        provider="example",
        region="local",
        store_product="s3",
        default_storage=False,
        bucket_name="media-secondary",
    )
    object_storage = InMemoryObjectStorage()
    app = create_app(
        settings,
        use_cases=TamossUseCases(
            repository=FakeTamossRepository(
                primary,
                storage_backends=[primary, secondary],
            ),
            object_storage=object_storage,
            settings=settings,
        ),
    )
    with TestClient(app) as client:
        flow_id, _, _ = create_video_flow(client)
        object_id = register_segment(client, flow_id, object_id=f"bbc/{uuid4()}.ts")

        copied = client.post(
            f"/objects/{quote(object_id, safe='')}/instances",
            json=controlled_object_instance_payload(secondary.id),
        )
        before_worker = client.get(
            f"/objects/{object_id}",
            params={"accept_storage_ids": str(secondary.id), "verbose_storage": "true"},
        )
        processed = app.state.tamoss_use_cases.objects.process_pending_object_copies(
            worker_id="copy-worker",
            lease_seconds=60,
        )
        after_worker = client.get(
            f"/objects/{object_id}",
            params={"accept_storage_ids": str(secondary.id), "verbose_storage": "true"},
        )
        primary_before_cleanup = object_storage.read(object_id, backend=primary)
        secondary_before_cleanup = object_storage.read(object_id, backend=secondary)
        deleted_primary = client.delete(
            f"/objects/{quote(object_id, safe='')}/instances",
            params={"storage_id": str(primary.id)},
        )
        cleanup_processed = (
            app.state.tamoss_use_cases.deletion.process_pending_object_cleanups(
                worker_id="cleanup-worker",
                lease_seconds=60,
            )
        )
        after_cleanup = client.get(
            f"/objects/{object_id}",
            params={"accept_storage_ids": str(secondary.id), "verbose_storage": "true"},
        )

    assert copied.status_code == 201
    assert before_worker.status_code == 200
    assert before_worker.json()["get_urls"] == []
    assert processed == 1
    assert primary_before_cleanup == b"segment"
    assert secondary_before_cleanup == b"segment"
    assert after_worker.status_code == 200
    assert after_worker.json()["get_urls"][0]["storage_id"] == str(secondary.id)
    assert after_worker.json()["get_urls"][0]["controlled"] is True
    assert deleted_primary.status_code == 204
    assert cleanup_processed == 1
    assert object_storage.read(object_id, backend=primary) is None
    assert object_storage.read(object_id, backend=secondary) == b"segment"
    assert after_cleanup.status_code == 200
    assert after_cleanup.json()["get_urls"][0]["storage_id"] == str(secondary.id)


def test_controlled_object_copy_failure_waits_for_claim_lease(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    settings = Settings(
        auth_required=False,
        public_base_url="http://testserver",
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
        id=SECONDARY_BACKEND_ID,
        label="tamoss.us-west-2:s3:tamoss-secondary",
        provider="example",
        region="local",
        store_product="s3",
        default_storage=False,
        bucket_name="media-secondary",
    )
    repository = FakeTamossRepository(primary, storage_backends=[primary, secondary])
    object_storage = InMemoryObjectStorage()
    use_cases = TamossUseCases(
        repository=repository,
        object_storage=object_storage,
        settings=settings,
    )
    object_id = f"bbc/{uuid4()}.ts"
    repository.save_object(
        MediaObjectRecord(
            id=object_id,
            referenced_by_flows={uuid4()},
            instances=[
                ObjectInstance(
                    storage_backend=primary,
                    url=None,
                    label=primary.label,
                    controlled=True,
                )
            ],
        )
    )
    repository.save_object_copy(
        ObjectCopyRecord(
            id=uuid4(),
            object_id=object_id,
            source_storage_backend_id=primary.id,
            destination_storage_backend_id=secondary.id,
            status="pending",
        )
    )

    def fail_copy(*_args: object, **_kwargs: object) -> None:
        raise RuntimeError("copy unavailable")

    monkeypatch.setattr(object_storage, "copy", fail_copy)

    assert (
        use_cases.objects.process_pending_object_copies(
            worker_id="copy-worker-a",
            lease_seconds=60,
        )
        == 1
    )
    failed_copy = repository.list_object_copies(statuses={"error"})[0]

    assert failed_copy.claimed_by is None
    assert failed_copy.claim_expires_at is not None
    assert (
        use_cases.objects.process_pending_object_copies(
            worker_id="copy-worker-b",
            lease_seconds=60,
        )
        == 0
    )


def test_controlled_instance_delete_uses_recoverable_cleanup_path(
    client: TestClient,
) -> None:
    use_cases = client.app.state.tamoss_use_cases
    flow_id, _, _ = create_video_flow(client)
    object_id = register_segment(client, flow_id, object_id=f"bbc/{uuid4()}.ts")
    backend = use_cases.repository.default_storage_backend()
    assert backend is not None

    external = client.post(
        f"/objects/{quote(object_id, safe='')}/instances",
        json=external_object_instance("https://cdn.example.test/external.ts"),
    )
    deleted_controlled = client.delete(
        f"/objects/{quote(object_id, safe='')}/instances",
        params={"storage_id": str(PRIMARY_BACKEND_ID)},
    )
    controlled = client.get(
        f"/objects/{object_id}",
        params={
            "accept_storage_ids": str(PRIMARY_BACKEND_ID),
            "verbose_storage": "true",
        },
    )
    cleanups = use_cases.repository.list_object_cleanups(statuses={"pending"})
    pending_cleanup_object_ids = [cleanup.object_id for cleanup in cleanups]
    pending_cleanup_statuses = [cleanup.status for cleanup in cleanups]
    cleanup_processed = use_cases.deletion.process_pending_object_cleanups(
        worker_id="cleanup-worker",
        lease_seconds=60,
    )
    completed_cleanups = use_cases.repository.list_object_cleanups(statuses={"done"})

    assert external.status_code == 201
    assert deleted_controlled.status_code == 204
    assert controlled.status_code == 200
    assert controlled.json()["get_urls"] == []
    assert len(cleanups) == 1
    assert pending_cleanup_object_ids == [object_id]
    assert pending_cleanup_statuses == ["pending"]
    assert cleanup_processed == 1
    assert use_cases.object_storage.read(object_id, backend=backend) is None
    assert len(completed_cleanups) == 1
    assert completed_cleanups[0].object_id == object_id


def test_uncontrolled_object_instances_are_deduplicated_by_label(
    client: TestClient,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    object_id = register_segment(client, flow_id, object_id=f"bbc/{uuid4()}.ts")
    instance = external_object_instance("https://media.example.test/object.ts")

    assert (
        client.post(
            f"/objects/{quote(object_id, safe='')}/instances", json=instance
        ).status_code
        == 201
    )
    assert (
        client.post(
            f"/objects/{quote(object_id, safe='')}/instances", json=instance
        ).status_code
        == 201
    )
    conflicting = client.post(
        f"/objects/{quote(object_id, safe='')}/instances",
        json=external_object_instance("https://media.example.test/other.ts"),
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
        json=segment_payload(object_id, key_frame_count=6),
    )
    changed_timerange = client.post(
        f"/flows/{second_flow_id}/segments",
        json=segment_payload(
            object_id,
            "[10:0_20:0)",
            object_timerange="[100:0_110:0)",
        ),
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

    assert (
        tamoss_app.state.tamoss_use_cases.deletion.process_pending_delete_requests()
        == 1
    )
    completed = client.get(f"/flow-delete-requests/{request_id}")
    assert completed.status_code == 200
    assert completed.json()["status"] == "done"
    remaining = client.get(f"/flows/{flow_id}/segments")
    assert remaining.status_code == 200
    assert [item["object_id"] for item in remaining.json()] == [second_object]
    assert client.get(f"/objects/{first_object}").status_code == 404


def test_segment_delete_timerange_coverage_respects_inclusive_endpoints(
    client: TestClient,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    object_id = register_segment(
        client,
        flow_id,
        object_id=f"bbc/{uuid4()}.ts",
        timerange="[0:0_10:0]",
    )

    partial = client.delete(
        f"/flows/{flow_id}/segments",
        params={"timerange": "[0:0_10:0)"},
    )
    assert partial.status_code == 204
    assert client.get(f"/objects/{object_id}").status_code == 200

    full = client.delete(
        f"/flows/{flow_id}/segments",
        params={"timerange": "[0:0_10:0]"},
    )
    assert full.status_code == 202


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
    assert (
        tamoss_app.state.tamoss_use_cases.deletion.process_pending_delete_requests()
        == 1
    )
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


def _object_with_external_instance(client: TestClient) -> tuple[UUID, str]:
    flow_id, _, _ = create_video_flow(client)
    object_id = register_segment(client, flow_id, object_id=f"bbc/{uuid4()}.ts")
    external = client.post(
        f"/objects/{quote(object_id, safe='')}/instances",
        json=external_object_instance("https://media.example.test/object.ts"),
    )
    assert external.status_code == 201
    return flow_id, object_id


def _payload_get_urls(endpoint_name: str, payload: object) -> list[dict[str, object]]:
    if endpoint_name == "segment":
        assert isinstance(payload, list)
        return payload[0]["get_urls"]
    assert isinstance(payload, dict)
    return payload["get_urls"]
