from __future__ import annotations

import os
import subprocess
import sys
from dataclasses import dataclass
from typing import Any
from uuid import UUID, uuid4

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient
from tamoss import worker
from tamoss.application import webhooks as webhooking
from tamoss.application.use_cases import TamossUseCases
from tamoss.domain.model import WebhookDeliveryRecord, utc_now

from tests.adapters.bbc.support import (
    create_video_flow,
    register_segment,
    video_flow_payload,
)

pytestmark = pytest.mark.worker


@dataclass(frozen=True)
class _WebhookResponse:
    status_code: int
    reason: str


def test_worker_import_does_not_create_application_with_unavailable_postgres() -> None:
    env = os.environ.copy()
    env["TAMOSS_DATABASE_URL"] = "postgresql://tamoss:tamoss@127.0.0.1:1/tamoss"
    env["PYTHONPATH"] = os.pathsep.join(path for path in sys.path if path)

    result = subprocess.run(
        [sys.executable, "-c", "import tamoss.worker; print('imported')"],
        env=env,
        text=True,
        capture_output=True,
        check=False,
    )

    assert result.returncode == 0, result.stderr
    assert result.stdout.strip() == "imported"


def test_worker_main_retries_after_poll_failure(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    slept: list[int] = []
    closed = False

    def fail_once(*_: Any, **__: Any) -> tuple[int, int]:
        worker._shutdown = True
        raise RuntimeError("temporary backend startup failure")

    class Repository:
        def close(self) -> None:
            nonlocal closed
            closed = True

    class UseCases:
        repository = Repository()

    use_cases = UseCases()
    monkeypatch.setenv("TAMOSS_WORKER_POLL_INTERVAL_SECONDS", "1")
    monkeypatch.setenv("TAMOSS_WORKER_ENABLE_DELETE", "1")
    monkeypatch.setenv("TAMOSS_WORKER_ENABLE_WEBHOOK", "0")
    monkeypatch.setattr(worker, "create_use_cases", lambda: use_cases)
    monkeypatch.setattr(worker, "drain_once", fail_once)
    monkeypatch.setattr(worker.time, "sleep", slept.append)
    monkeypatch.setattr(worker.signal, "signal", lambda *_: None)
    worker._shutdown = False

    try:
        worker.main()
    finally:
        worker._shutdown = False

    assert slept == [1]
    assert closed is True


def test_delete_worker_preserves_shared_objects_until_final_reference(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    use_cases = _route_worker_to_app(monkeypatch, tamoss_app)
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
        json={"object_id": shared_object_id, "timerange": "[0:0_10:0)"},
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
    assert client.get(f"/objects/{shared_object_id}").status_code == 200
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
    use_cases = _route_worker_to_app(monkeypatch, tamoss_app)
    request_ids: set[UUID] = set()
    claimed: list[tuple[UUID, str | None]] = []

    for _ in range(2):
        flow_id, _, _ = create_video_flow(client)
        object_id = f"bbc/{uuid4()}.ts"
        register_segment(client, flow_id, object_id=object_id)
        accepted = client.delete(f"/flows/{flow_id}")
        assert accepted.status_code == 202
        request_ids.add(UUID(accepted.json()["id"]))

    def hold_delete_claim(request_id: UUID):
        request = use_cases.repository.get_delete_request(request_id)
        assert request is not None
        claimed.append((request.id, request.claimed_by))
        return request

    monkeypatch.setattr(use_cases, "process_delete_request", hold_delete_claim)

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


def test_delete_worker_retries_planned_cleanup_after_storage_failure(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    use_cases = _route_worker_to_app(monkeypatch, tamoss_app)
    flow_id, _, _ = create_video_flow(client)
    object_id = register_segment(client, flow_id, object_id=f"bbc/{uuid4()}.ts")
    default_backend = use_cases.repository.default_storage_backend()
    assert default_backend is not None
    use_cases.object_storage.write(
        object_id,
        b"essence",
        backend=default_backend,
    )
    original_delete = use_cases.object_storage.delete
    attempts = 0

    def fail_once(
        failed_object_id: str,
        *,
        backend=None,
    ) -> None:
        nonlocal attempts
        attempts += 1
        if attempts == 1:
            raise RuntimeError("storage delete unavailable")
        original_delete(failed_object_id, backend=backend)

    monkeypatch.setattr(use_cases.object_storage, "delete", fail_once)
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
    assert failed.claimed_by is None
    assert failed.segments_to_delete[0].object_id == object_id
    assert client.get(f"/flows/{flow_id}").status_code == 200
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
        == 1
    )
    completed = use_cases.repository.get_delete_request(request_id)
    assert completed is not None
    assert completed.status == "done"
    assert client.get(f"/flows/{flow_id}").status_code == 404
    assert use_cases.object_storage.read(object_id, backend=default_backend) is None


def test_webhook_worker_delivers_payload_and_clears_claim(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    use_cases = _route_worker_to_app(monkeypatch, tamoss_app)
    delivered: list[tuple[dict[str, Any], dict[str, Any]]] = []

    def send_success(*, webhook: dict[str, Any], payload: dict[str, Any]):
        delivered.append((webhook, payload))
        return _WebhookResponse(status_code=202, reason="Accepted")

    monkeypatch.setattr(webhooking, "send_webhook_delivery", send_success)
    flow_id = uuid4()
    source_id = uuid4()
    webhook = client.post(
        "/service/webhooks",
        json={
            "url": "https://example.test/tamoss-webhook",
            "events": ["flows/created"],
            "api_key_name": "x-api-key",
            "api_key_value": "secret",
        },
    )
    assert webhook.status_code == 201

    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(flow_id, source_id),
    )
    assert created.status_code == 201
    delivery = _only_delivery(use_cases)

    assert (
        worker.drain_webhook_deliveries(
            use_cases,
            max_deliveries=1,
            worker_id="webhook-worker-a",
            lease_seconds=30,
        )
        == 1
    )
    completed = use_cases.repository.get_webhook_delivery(delivery.id)
    assert completed is not None
    assert completed.status == "done"
    assert completed.response_status == 202
    assert completed.attempt_count == 1
    assert completed.claimed_by is None
    assert completed.next_attempt_at is None
    assert len(delivered) == 1
    delivered_webhook, delivered_payload = delivered[0]
    assert delivered_webhook["api_key_name"] == "x-api-key"
    assert delivered_webhook["api_key_value"] == "secret"
    assert delivered_payload["event_type"] == "flows/created"
    assert delivered_payload["event"]["flow"]["id"] == str(flow_id)


def test_webhook_workers_split_active_leases_without_duplicate_delivery(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    use_cases = _route_worker_to_app(monkeypatch, tamoss_app)
    claimed: list[tuple[UUID, str | None]] = []

    webhook = client.post(
        "/service/webhooks",
        json={
            "url": "https://example.test/tamoss-webhook",
            "events": ["flows/created"],
        },
    )
    assert webhook.status_code == 201

    for _ in range(2):
        flow_id = uuid4()
        source_id = uuid4()
        created = client.put(
            f"/flows/{flow_id}",
            json=video_flow_payload(flow_id, source_id),
        )
        assert created.status_code == 201

    delivery_ids = {
        delivery.id for delivery in use_cases.repository.list_webhook_deliveries()
    }
    assert len(delivery_ids) == 2

    def hold_webhook_claim(delivery_id: UUID):
        delivery = use_cases.repository.get_webhook_delivery(delivery_id)
        assert delivery is not None
        claimed.append((delivery.id, delivery.claimed_by))
        return delivery

    monkeypatch.setattr(use_cases, "process_webhook_delivery", hold_webhook_claim)

    assert (
        worker.drain_webhook_deliveries(
            use_cases,
            max_deliveries=1,
            worker_id="webhook-worker-a",
            lease_seconds=30,
        )
        == 1
    )
    assert (
        worker.drain_webhook_deliveries(
            use_cases,
            max_deliveries=1,
            worker_id="webhook-worker-b",
            lease_seconds=30,
        )
        == 1
    )
    assert (
        worker.drain_webhook_deliveries(
            use_cases,
            max_deliveries=1,
            worker_id="webhook-worker-c",
            lease_seconds=30,
        )
        == 0
    )

    assert {delivery_id for delivery_id, _ in claimed} == delivery_ids
    assert {worker_id for _, worker_id in claimed} == {
        "webhook-worker-a",
        "webhook-worker-b",
    }
    for delivery_id in delivery_ids:
        delivery = use_cases.repository.get_webhook_delivery(delivery_id)
        assert delivery is not None
        assert delivery.status == "started"
        assert delivery.claimed_by in {"webhook-worker-a", "webhook-worker-b"}
        assert delivery.claim_expires_at is not None


def test_webhook_worker_retries_then_marks_terminal_failures(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    use_cases = _route_worker_to_app(monkeypatch, tamoss_app)
    monkeypatch.setenv("TAMOSS_WORKER_MAX_ATTEMPTS", "2")

    def send_unavailable(*, webhook: dict[str, Any], payload: dict[str, Any]):
        return _WebhookResponse(status_code=503, reason="Service Unavailable")

    monkeypatch.setattr(webhooking, "send_webhook_delivery", send_unavailable)
    webhook = client.post(
        "/service/webhooks",
        json={
            "url": "https://example.test/tamoss-webhook",
            "events": ["flows/created"],
        },
    )
    assert webhook.status_code == 201
    webhook_id = UUID(webhook.json()["id"])
    flow_id = uuid4()
    source_id = uuid4()
    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(flow_id, source_id),
    )
    assert created.status_code == 201
    delivery = _only_delivery(use_cases)

    assert (
        worker.drain_webhook_deliveries(
            use_cases,
            max_deliveries=1,
            worker_id="webhook-worker-a",
            lease_seconds=30,
        )
        == 1
    )
    retrying = use_cases.repository.get_webhook_delivery(delivery.id)
    assert retrying is not None
    assert retrying.status == "pending"
    assert retrying.attempt_count == 1
    assert retrying.response_status == 503
    assert retrying.error is not None
    assert retrying.error["type"] == "HTTPError"
    assert "HTTP 503" in retrying.error["summary"]
    assert retrying.next_attempt_at is not None
    assert retrying.claimed_by is None
    active_webhook = use_cases.repository.get_webhook(webhook_id)
    assert active_webhook is not None
    assert active_webhook.status == "started"

    retrying.next_attempt_at = utc_now()
    use_cases.repository.save_webhook_delivery(retrying)
    assert (
        worker.drain_webhook_deliveries(
            use_cases,
            max_deliveries=1,
            worker_id="webhook-worker-b",
            lease_seconds=30,
        )
        == 1
    )
    dead = use_cases.repository.get_webhook_delivery(delivery.id)
    assert dead is not None
    assert dead.status == "dead"
    assert dead.attempt_count == 2
    assert dead.response_status == 503
    assert dead.next_attempt_at is None
    assert dead.claimed_by is None
    errored_webhook = use_cases.repository.get_webhook(webhook_id)
    assert errored_webhook is not None
    assert errored_webhook.status == "error"
    assert errored_webhook.data["error"]["type"] == "HTTPError"


def _route_worker_to_app(
    monkeypatch: pytest.MonkeyPatch, tamoss_app: FastAPI
) -> TamossUseCases:
    use_cases = tamoss_app.state.tamoss_use_cases
    return use_cases


def _only_delivery(use_cases: TamossUseCases) -> WebhookDeliveryRecord:
    deliveries = use_cases.repository.list_webhook_deliveries()
    assert len(deliveries) == 1
    return deliveries[0]
