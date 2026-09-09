from __future__ import annotations

import logging
from collections.abc import Iterable
from datetime import timedelta
from unittest.mock import Mock
from uuid import UUID, uuid4

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient
from tamoss import worker
from tamoss.adapters.object_storage import ConfiguredObjectStorage
from tamoss.application.contexts import deletion_processor
from tamoss.domain.model import StorageBackend, utc_now

from tests.tams.support import (
    create_video_flow,
    register_segment,
    segment_payload,
)
from tests.workers.support import route_worker_to_app

pytestmark = pytest.mark.worker


def test_delete_worker_preserves_shared_objects_until_final_reference(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _ = monkeypatch
    use_cases = route_worker_to_app(tamoss_app)
    shared_object_id = f"bbc/{uuid4()}.ts"
    first_flow_id, _, _ = create_video_flow(client)
    second_flow_id, _, _ = create_video_flow(client)

    register_segment(client, first_flow_id, object_id=shared_object_id)
    default_backend = use_cases.repository.default_storage_backend()
    assert default_backend is not None
    use_cases.object_storage.write(
        shared_object_id,
        b"shared essence",
        backend=default_backend,
    )

    shared_segment = client.post(
        f"/flows/{second_flow_id}/segments",
        json=segment_payload(shared_object_id),
    )
    assert shared_segment.status_code == 201

    first_delete = client.delete(f"/flows/{first_flow_id}")
    assert first_delete.status_code == 202
    first_request_id = UUID(first_delete.json()["id"])

    assert (
        worker.drain_delete_requests(
            use_cases,
            max_requests=1,
            worker_id="delete-worker-a",
            lease_seconds=30,
        )
        == 1
    )
    first_request = use_cases.repository.get_delete_request(first_request_id)
    assert first_request is not None
    assert first_request.status == "done"
    assert first_request.timerange_remaining is None
    assert first_request.claimed_by is None
    assert client.get(f"/flows/{first_flow_id}").status_code == 404
    kept_object = client.get(f"/objects/{shared_object_id}")
    assert kept_object.status_code == 200
    assert kept_object.json()["referenced_by_flows"] == [str(second_flow_id)]
    assert (
        use_cases.object_storage.read(shared_object_id, backend=default_backend)
        == b"shared essence"
    )

    second_delete = client.delete(f"/flows/{second_flow_id}")
    assert second_delete.status_code == 202
    second_request_id = UUID(second_delete.json()["id"])

    assert (
        worker.drain_delete_requests(
            use_cases,
            max_requests=1,
            worker_id="delete-worker-b",
            lease_seconds=30,
        )
        == 1
    )
    second_request = use_cases.repository.get_delete_request(second_request_id)
    assert second_request is not None
    assert second_request.status == "done"
    assert second_request.claimed_by is None
    assert client.get(f"/flows/{second_flow_id}").status_code == 404
    assert client.get(f"/objects/{shared_object_id}").status_code == 404
    assert (
        use_cases.object_storage.read(shared_object_id, backend=default_backend) is None
    )


def test_delete_workers_split_active_leases_without_duplicate_processing(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    use_cases = route_worker_to_app(tamoss_app)
    request_ids: set[UUID] = set()
    claimed: list[tuple[UUID, str | None]] = []

    for _ in range(2):
        flow_id, _, _ = create_video_flow(client)
        object_id = f"bbc/{uuid4()}.ts"
        register_segment(client, flow_id, object_id=object_id)
        accepted = client.delete(f"/flows/{flow_id}")
        assert accepted.status_code == 202
        request_ids.add(UUID(accepted.json()["id"]))

    def hold_delete_claim(**kwargs: object):
        request_id = kwargs["request_id"]
        assert isinstance(request_id, UUID)
        request = use_cases.repository.get_delete_request(request_id)
        assert request is not None
        claimed.append((request.id, request.claimed_by))
        return request

    monkeypatch.setattr(
        deletion_processor,
        "process_delete_request",
        hold_delete_claim,
    )

    assert (
        worker.drain_delete_requests(
            use_cases,
            max_requests=1,
            worker_id="delete-worker-a",
            lease_seconds=30,
        )
        == 1
    )
    assert (
        worker.drain_delete_requests(
            use_cases,
            max_requests=1,
            worker_id="delete-worker-b",
            lease_seconds=30,
        )
        == 1
    )
    assert (
        worker.drain_delete_requests(
            use_cases,
            max_requests=1,
            worker_id="delete-worker-c",
            lease_seconds=30,
        )
        == 0
    )

    assert {request_id for request_id, _ in claimed} == request_ids
    assert {worker_id for _, worker_id in claimed} == {
        "delete-worker-a",
        "delete-worker-b",
    }
    for request_id in request_ids:
        request = use_cases.repository.get_delete_request(request_id)
        assert request is not None
        assert request.status == "started"
        assert request.claimed_by in {"delete-worker-a", "delete-worker-b"}
        assert request.claim_expires_at is not None


def test_delete_worker_continues_large_delete_requests_in_batches(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(deletion_processor, "DELETE_SEGMENT_BATCH_SIZE", 1)
    use_cases = route_worker_to_app(tamoss_app)
    flow_id, _, _ = create_video_flow(client)
    first_object_id = register_segment(
        client,
        flow_id,
        object_id=f"bbc/{uuid4()}.ts",
        timerange="[0:0_10:0)",
    )
    second_object_id = register_segment(
        client,
        flow_id,
        object_id=f"bbc/{uuid4()}.ts",
        timerange="[10:0_20:0)",
    )

    accepted = client.delete(f"/flows/{flow_id}")
    assert accepted.status_code == 202
    request_id = UUID(accepted.json()["id"])

    assert worker.drain_delete_requests(use_cases, max_requests=1) == 1
    started = use_cases.repository.get_delete_request(request_id)
    assert started is not None
    assert started.status == "started"
    assert started.timerange_remaining == "[10:0_20:0)"
    assert started.claimed_by is None
    assert client.get(f"/flows/{flow_id}").status_code == 200
    assert client.get(f"/objects/{first_object_id}").status_code == 404
    assert client.get(f"/objects/{second_object_id}").status_code == 200

    assert worker.drain_delete_requests(use_cases, max_requests=1) == 1
    completed = use_cases.repository.get_delete_request(request_id)
    assert completed is not None
    assert completed.status == "done"
    assert completed.timerange_remaining is None
    assert client.get(f"/flows/{flow_id}").status_code == 404
    assert client.get(f"/objects/{second_object_id}").status_code == 404


def test_delete_worker_batches_object_cleanup_by_backend(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    use_cases = route_worker_to_app(tamoss_app)
    flow_id, _, _ = create_video_flow(client)
    first_object_id = register_segment(
        client,
        flow_id,
        object_id=f"bbc/{uuid4()}.ts",
        timerange="[0:0_10:0)",
    )
    second_object_id = register_segment(
        client,
        flow_id,
        object_id=f"bbc/{uuid4()}.ts",
        timerange="[10:0_20:0)",
    )
    default_backend = use_cases.repository.default_storage_backend()
    assert default_backend is not None

    accepted = client.delete(f"/flows/{flow_id}")
    assert accepted.status_code == 202

    assert worker.drain_delete_requests(use_cases, max_requests=1) == 1

    assert len(use_cases.object_storage.deleted_batches) == 1
    assert set(use_cases.object_storage.deleted_batches[0]) == {
        (default_backend.id, first_object_id),
        (default_backend.id, second_object_id),
    }
    assert (
        use_cases.object_storage.read(first_object_id, backend=default_backend) is None
    )
    assert (
        use_cases.object_storage.read(second_object_id, backend=default_backend) is None
    )


@pytest.mark.parametrize("failure", ["exception", "s3-response"])
def test_delete_worker_retries_planned_cleanup_after_storage_failure(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
    failure: str,
) -> None:
    use_cases = route_worker_to_app(tamoss_app)
    flow_id, _, _ = create_video_flow(client)
    object_id = register_segment(client, flow_id, object_id=f"bbc/{uuid4()}.ts")
    default_backend = use_cases.repository.default_storage_backend()
    assert default_backend is not None
    use_cases.object_storage.write(
        object_id,
        b"essence",
        backend=default_backend,
    )
    original_delete_batch = use_cases.object_storage.delete_batch
    attempts = 0

    def fail_once(
        failed_object_ids: Iterable[str],
        *,
        backend: StorageBackend | None = None,
    ) -> None:
        nonlocal attempts
        attempts += 1
        if attempts == 1:
            if failure == "s3-response":
                storage = ConfiguredObjectStorage(use_cases.settings)
                s3 = Mock()
                s3.delete_objects.return_value = {
                    "Errors": [{"Key": object_id, "Code": "AccessDenied"}]
                }
                monkeypatch.setattr(storage, "_s3_client", lambda _backend: s3)
                storage.delete_batch(failed_object_ids, backend=backend)
                return
            raise RuntimeError("storage delete unavailable")
        original_delete_batch(failed_object_ids, backend=backend)

    monkeypatch.setattr(use_cases.object_storage, "delete_batch", fail_once)
    accepted = client.delete(f"/flows/{flow_id}")
    assert accepted.status_code == 202
    request_id = UUID(accepted.json()["id"])

    assert (
        worker.drain_delete_requests(
            use_cases,
            max_requests=1,
            worker_id="delete-worker-a",
            lease_seconds=30,
        )
        == 1
    )
    failed = use_cases.repository.get_delete_request(request_id)
    assert failed is not None
    assert failed.status == "error"
    assert failed.error is not None
    assert failed.error.type == "object_cleanup_failed"
    assert failed.claimed_by is None
    assert failed.claim_expires_at is not None
    assert failed.timerange_remaining is None
    cleanups = use_cases.repository.list_object_cleanups(delete_request_id=request_id)
    assert cleanups[0].object_id == object_id
    assert cleanups[0].status == "error"
    assert client.get(f"/flows/{flow_id}").status_code == 404
    assert client.get(f"/objects/{object_id}").status_code == 404
    assert (
        use_cases.object_storage.read(object_id, backend=default_backend) == b"essence"
    )

    assert (
        worker.drain_delete_requests(
            use_cases,
            max_requests=1,
            worker_id="delete-worker-b",
            lease_seconds=30,
        )
        == 0
    )
    failed.claim_expires_at = utc_now() - timedelta(seconds=1)
    use_cases.repository.save_delete_request(failed)

    assert (
        worker.drain_delete_requests(
            use_cases,
            max_requests=1,
            worker_id="delete-worker-b",
            lease_seconds=30,
        )
        == 1
    )
    completed = use_cases.repository.get_delete_request(request_id)
    assert completed is not None
    assert completed.status == "done"
    assert client.get(f"/flows/{flow_id}").status_code == 404
    assert use_cases.object_storage.read(object_id, backend=default_backend) is None


def test_delete_worker_persists_public_error_code_for_unexpected_failure(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
    caplog: pytest.LogCaptureFixture,
) -> None:
    use_cases = route_worker_to_app(tamoss_app)
    flow_id, _, _ = create_video_flow(client)
    register_segment(client, flow_id, object_id=f"bbc/{uuid4()}.ts")
    accepted = client.delete(f"/flows/{flow_id}")
    assert accepted.status_code == 202
    request_id = UUID(accepted.json()["id"])

    def fail_delete_segment_batch(*_args: object, **_kwargs: object) -> None:
        raise RuntimeError("private metadata failure")

    monkeypatch.setattr(
        use_cases.repository,
        "delete_segment_batch",
        fail_delete_segment_batch,
    )

    with caplog.at_level(
        logging.ERROR,
        logger="tamoss.application.contexts.deletion_processor",
    ):
        assert (
            worker.drain_delete_requests(
                use_cases,
                max_requests=1,
                worker_id="delete-worker-a",
                lease_seconds=30,
            )
            == 1
        )

    failed = use_cases.repository.get_delete_request(request_id)
    assert failed is not None
    assert failed.status == "error"
    assert failed.error is not None
    assert failed.error.type == "delete_request_failed"
    assert failed.error.summary == (
        "Delete request processing failed; retry will continue."
    )
    assert "private metadata failure" not in failed.error.summary
    assert "private metadata failure" in caplog.text


def test_delete_worker_treats_missing_object_cleanup_as_done(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _ = monkeypatch
    use_cases = route_worker_to_app(tamoss_app)
    flow_id, _, _ = create_video_flow(client)
    object_id = register_segment(client, flow_id, object_id=f"bbc/{uuid4()}.ts")
    default_backend = use_cases.repository.default_storage_backend()
    assert default_backend is not None
    use_cases.object_storage.delete(object_id, backend=default_backend)

    accepted = client.delete(f"/flows/{flow_id}")
    assert accepted.status_code == 202
    request_id = UUID(accepted.json()["id"])

    assert (
        worker.drain_delete_requests(
            use_cases,
            max_requests=1,
            worker_id="delete-worker-a",
            lease_seconds=30,
        )
        == 1
    )
    completed = use_cases.repository.get_delete_request(request_id)
    cleanups = use_cases.repository.list_object_cleanups(
        delete_request_id=request_id,
    )

    assert completed is not None
    assert completed.status == "done"
    assert client.get(f"/flows/{flow_id}").status_code == 404
    assert client.get(f"/objects/{object_id}").status_code == 404
    assert len(cleanups) == 1
    assert cleanups[0].status == "done"
    assert cleanups[0].attempt_count == 1
