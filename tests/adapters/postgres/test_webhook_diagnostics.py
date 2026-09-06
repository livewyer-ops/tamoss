from __future__ import annotations

import json
from datetime import timedelta
from uuid import uuid4

import psycopg
import pytest
from tamoss.adapters.postgres import PostgresRepository
from tamoss.domain.model import (
    DomainErrorPayload,
    WebhookDeliveryRecord,
    WebhookRecord,
    utc_now,
)

from tests.support.paths import REPO_ROOT, load_python_module

pytestmark = pytest.mark.needs_db


def test_diagnostics_are_bounded_scoped_and_do_not_expose_credentials(
    postgres_repo: PostgresRepository, postgres_connection: psycopg.Connection
) -> None:
    module = load_python_module(
        "webhook_diagnostics", REPO_ROOT / "scripts/webhook_diagnostics.py"
    )
    hooks = postgres_repo.webhook_repository
    webhook_id, unrelated_id = uuid4(), uuid4()
    for identity in (webhook_id, unrelated_id):
        hooks.save_webhook(
            WebhookRecord(
                id=identity,
                status="started",
                data={
                    "url": "https://user:private-password@receiver.example/events?token=secret-token",
                    "api_key_value": "private-api-key",
                    "events": ["flows/created"],
                    "flow_collected_by_ids": [],
                },
            )
        )
    now = utc_now()
    for index in range(25):
        delivery = WebhookDeliveryRecord(
            id=uuid4(),
            webhook_id=webhook_id if index < 24 else unrelated_id,
            webhook_snapshot={"api_key_value": "private-api-key"},
            event_type="flows/created",
            event_timestamp=now,
            payload={"url": "https://storage.example/?token=private-media-token"},
            status=["done", "dead", "started", "pending"][index % 4],
            attempt_count=2,
            error=DomainErrorPayload.create("HTTPError", "private-error-url"),
            response_status=503,
        )
        if delivery.status == "started":
            delivery.claimed_by = "expired-worker"
            delivery.claim_expires_at = now - timedelta(minutes=1)
        hooks.save_webhook_delivery(delivery)
    result = module.report(postgres_connection, webhook_id)
    assert result["receiver_host"] == "receiver.example"
    assert result["filters"] == {"flow_collected_by_ids": []}
    summary = result["retained_deliveries"]
    assert [
        summary[key] for key in ("pending", "started", "done", "dead", "expired_claims")
    ] == [6] * 5
    assert summary["oldest_pending_at"] is not None
    assert summary["last_success_at"] is not None
    assert len(result["recent_deliveries"]) == 20
    serialized = json.dumps(result, default=str)
    for secret in (
        "private-",
        "secret-token",
        "api_key_value",
        "webhook_snapshot",
        "payload",
    ):
        assert secret not in serialized
    assert len(hooks.list_webhook_deliveries()) == 25
    with pytest.raises(ValueError, match="does not exist"):
        module.report(postgres_connection, uuid4())


def test_expired_delivery_claim_is_recovered_without_resetting_attempts(
    postgres_repo: PostgresRepository, postgres_connection: psycopg.Connection
) -> None:
    hooks = postgres_repo.webhook_repository
    webhook_id = uuid4()
    hooks.save_webhook(WebhookRecord(id=webhook_id, status="started", data={}))
    delivery = WebhookDeliveryRecord(
        id=uuid4(),
        webhook_id=webhook_id,
        webhook_snapshot={},
        event_type="flows/created",
        event_timestamp=utc_now(),
        payload={},
        status="pending",
        attempt_count=2,
    )
    hooks.save_webhook_delivery(delivery)
    first = hooks.claim_webhook_deliveries(
        worker_id="worker-one", limit=1, lease_seconds=60
    )
    assert len(first) == 1
    assert (
        hooks.claim_webhook_deliveries(
            worker_id="worker-two", limit=1, lease_seconds=60
        )
        == []
    )
    postgres_connection.execute(
        "UPDATE tamoss_webhook_deliveries "
        "SET claim_expires_at = NOW() - INTERVAL '1 second' WHERE id = %s",
        (delivery.id,),
    )
    recovered = hooks.claim_webhook_deliveries(
        worker_id="worker-two", limit=1, lease_seconds=60
    )
    assert len(recovered) == 1
    assert recovered[0].id == delivery.id
    assert recovered[0].claimed_by == "worker-two"
    assert recovered[0].attempt_count == 2
