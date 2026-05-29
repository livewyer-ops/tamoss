from __future__ import annotations

import ipaddress
from typing import Any
from uuid import UUID, uuid4

import pytest
import requests
from fastapi import FastAPI
from fastapi.testclient import TestClient
from tamoss import worker
from tamoss.application import webhooks as webhooking
from tamoss.domain.model import utc_now

from tests.adapters.bbc.support import video_flow_payload
from tests.support.fixtures import load_json_fixture
from tests.workers.support import WebhookResponse, only_delivery, route_worker_to_app

pytestmark = pytest.mark.worker


def test_webhook_worker_delivers_payload_and_clears_claim(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    use_cases = route_worker_to_app(tamoss_app)
    delivered: list[tuple[dict[str, Any], dict[str, Any]]] = []

    def send_success(
        *,
        webhook: dict[str, Any],
        payload: dict[str, Any],
        timeout_seconds: float,
        egress_policy: object | None = None,
    ):
        delivered.append((webhook, payload))
        return WebhookResponse(status_code=202, reason="Accepted")

    monkeypatch.setattr(webhooking, "send_webhook_delivery", send_success)
    flow_id = uuid4()
    source_id = uuid4()
    webhook = client.post(
        "/service/webhooks",
        json=_webhook_payload(with_secret=True),
    )
    assert webhook.status_code == 201

    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(flow_id, source_id),
    )
    assert created.status_code == 201
    delivery = only_delivery(use_cases)
    assert "api_key_value" not in delivery.webhook_snapshot
    assert delivery.webhook_snapshot["api_key_value_ref"] == "webhook.api_key_value"

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


def test_webhook_worker_blocks_restricted_target_before_delivery(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    use_cases = route_worker_to_app(tamoss_app)
    sent = False

    def send_unexpected(
        *,
        webhook: dict[str, Any],
        payload: dict[str, Any],
        timeout_seconds: float,
        egress_policy: object | None = None,
    ):
        nonlocal sent
        sent = True
        return WebhookResponse(status_code=202, reason="Accepted")

    monkeypatch.setattr(webhooking, "send_webhook_delivery", send_unexpected)
    webhook = client.post(
        "/service/webhooks",
        json=_webhook_payload(
            receiver_url="https://receiver.example.test/tamoss-webhook"
        ),
    )
    assert webhook.status_code == 201
    flow_id = uuid4()
    source_id = uuid4()
    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(flow_id, source_id),
    )
    assert created.status_code == 201
    delivery = only_delivery(use_cases)
    monkeypatch.setattr(
        webhooking,
        "_resolve_host_addresses",
        lambda hostname, port: [ipaddress.ip_address("10.10.0.20")],
    )

    assert (
        worker.drain_webhook_deliveries(
            use_cases,
            max_deliveries=1,
            worker_id="webhook-worker-a",
            lease_seconds=30,
        )
        == 1
    )

    blocked = use_cases.repository.get_webhook_delivery(delivery.id)
    assert blocked is not None
    assert blocked.status == "dead"
    assert blocked.error is not None
    assert blocked.error.type == "WebhookTargetBlocked"
    assert sent is False


def test_webhook_workers_split_active_leases_without_duplicate_delivery(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    use_cases = route_worker_to_app(tamoss_app)
    claimed: list[tuple[UUID, str | None]] = []

    webhook = client.post(
        "/service/webhooks",
        json=_webhook_payload(),
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

    monkeypatch.setattr(
        use_cases.webhooks,
        "process_webhook_delivery",
        hold_webhook_claim,
    )

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
    use_cases = route_worker_to_app(tamoss_app)
    use_cases.settings.webhook_max_attempts = 2

    def send_unavailable(
        *,
        webhook: dict[str, Any],
        payload: dict[str, Any],
        timeout_seconds: float,
        egress_policy: object | None = None,
    ):
        return WebhookResponse(status_code=503, reason="Service Unavailable")

    monkeypatch.setattr(webhooking, "send_webhook_delivery", send_unavailable)
    webhook = client.post(
        "/service/webhooks",
        json=_webhook_payload(),
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
    delivery = only_delivery(use_cases)

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
    assert retrying.error.type == "HTTPError"
    assert "HTTP 503" in retrying.error.summary
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


def test_webhook_worker_retries_transport_failures(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    use_cases = route_worker_to_app(tamoss_app)
    use_cases.settings.webhook_max_attempts = 2

    def send_timeout(
        *,
        webhook: dict[str, Any],
        payload: dict[str, Any],
        timeout_seconds: float,
        egress_policy: object | None = None,
    ):
        raise requests.Timeout("webhook deadline exceeded")

    monkeypatch.setattr(webhooking, "send_webhook_delivery", send_timeout)
    webhook = client.post(
        "/service/webhooks",
        json=_webhook_payload(),
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
    delivery = only_delivery(use_cases)

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
    assert retrying.error is not None
    assert retrying.error.type == "Timeout"
    assert retrying.response_status is None
    assert retrying.next_attempt_at is not None

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
    assert dead.error is not None
    assert dead.error.type == "Timeout"
    errored_webhook = use_cases.repository.get_webhook(webhook_id)
    assert errored_webhook is not None
    assert errored_webhook.status == "error"


def _webhook_payload(
    *,
    receiver_url: str = "https://example.test/tamoss-webhook",
    with_secret: bool = False,
) -> dict[str, Any]:
    fixture = (
        "workers/webhook_registration_with_secret.json"
        if with_secret
        else "workers/webhook_registration.json"
    )
    payload: dict[str, Any] = load_json_fixture(fixture)
    payload["url"] = receiver_url
    return payload
