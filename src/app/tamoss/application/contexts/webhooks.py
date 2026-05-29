from __future__ import annotations

from collections.abc import Mapping
from datetime import timedelta
from typing import Any
from uuid import UUID, uuid4

import requests

from tamoss.application import webhooks as webhooking
from tamoss.domain.model import (
    DomainErrorPayload,
    WebhookDeliveryRecord,
    WebhookRecord,
    utc_now,
)
from tamoss.domain.pagination import Page
from tamoss.errors import BadRequest, NotFound
from tamoss.ports.repositories import WebhookRepository
from tamoss.settings import DEFAULT_WORKER_LEASE_SECONDS, Settings

DEFAULT_WORKER_ID = "tamoss-worker"


class WebhookUseCases:
    repository: WebhookRepository
    settings: Settings

    def __init__(
        self,
        *,
        repository: WebhookRepository,
        settings: Settings,
    ) -> None:
        self.repository = repository
        self.settings = settings

    def list_webhooks(
        self,
        *,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[WebhookRecord]:
        return self.repository.list_webhooks_page(
            tag_values=tag_values,
            tag_exists=tag_exists,
            page=page,
            limit=limit,
        )

    def get_webhook(self, webhook_id: UUID) -> WebhookRecord:
        webhook = self.repository.get_webhook(webhook_id)
        if webhook is None:
            raise NotFound("The requested Webhook ID in the path is invalid.")
        return webhook

    def create_webhook(self, webhook: Mapping[str, Any]) -> WebhookRecord:
        data = dict(webhook)
        try:
            webhooking.validate_webhook_configuration(
                data,
                egress_policy=self._webhook_egress_policy(),
            )
        except ValueError as exc:
            raise BadRequest("Bad request. Invalid webhook payload.") from exc
        status = data.pop("status", None) or "created"
        data.pop("tags", None)
        data.pop("error", None)
        webhook = WebhookRecord(
            id=uuid4(),
            data=data,
            status=status,
            tags=dict(webhook.get("tags") or {}),
        )
        self.repository.save_webhook(webhook)
        return webhook

    def put_webhook(
        self, *, webhook_id: UUID, webhook: Mapping[str, Any]
    ) -> WebhookRecord:
        if UUID(str(webhook.get("id"))) != webhook_id:
            raise NotFound("The requested Webhook ID in the path is invalid.")
        existing = self.get_webhook(webhook_id)
        if existing.status == "error" and webhook.get("status") == "disabled":
            raise BadRequest(
                "Bad request. The Webhook is currently in an error status and "
                "therefore cannot be updated to disabled."
            )

        data = dict(webhook)
        try:
            webhooking.validate_webhook_configuration(
                data,
                egress_policy=self._webhook_egress_policy(),
            )
        except ValueError as exc:
            raise BadRequest("Bad request. Invalid webhook payload.") from exc
        data.pop("id", None)
        status = data.pop("status")
        data.pop("tags", None)
        data.pop("error", None)
        webhook = WebhookRecord(
            id=webhook_id,
            data=data,
            status=status,
            tags=dict(webhook.get("tags") or {}),
        )
        self.repository.save_webhook(webhook)
        return webhook

    def delete_webhook(self, webhook_id: UUID) -> None:
        self.get_webhook(webhook_id)
        self.repository.delete_webhook(webhook_id)

    def process_pending_webhook_deliveries(
        self,
        *,
        max_deliveries: int = 50,
        worker_id: str = DEFAULT_WORKER_ID,
        lease_seconds: int = DEFAULT_WORKER_LEASE_SECONDS,
    ) -> int:
        processed = 0
        deliveries = self.repository.claim_webhook_deliveries(
            worker_id=worker_id,
            limit=max_deliveries,
            lease_seconds=lease_seconds,
        )
        for delivery in deliveries:
            self.process_webhook_delivery(delivery.id)
            processed += 1
        return processed

    def process_webhook_delivery(
        self, delivery_id: UUID
    ) -> WebhookDeliveryRecord | None:
        delivery = self.repository.get_webhook_delivery(delivery_id)
        if delivery is None:
            return None
        if delivery.status not in {"pending", "started"}:
            return delivery

        live_webhook = self.repository.get_webhook(delivery.webhook_id)
        if live_webhook is None:
            return self._mark_webhook_delivery_dead(
                delivery,
                response_status=None,
                error_type="WebhookNotFound",
                error_summary="Webhook no longer exists",
                mark_webhook_error=False,
            )
        if live_webhook.status not in {"created", "started"}:
            return self._mark_webhook_delivery_dead(
                delivery,
                response_status=None,
                error_type="WebhookDisabled",
                error_summary="Webhook is no longer active for delivery",
                mark_webhook_error=False,
            )

        delivery.status = "started"
        delivery.attempt_count += 1
        delivery.updated = utc_now()
        self.repository.save_webhook_delivery(delivery)

        delivery_webhook = webhooking.webhook_for_delivery(
            delivery.webhook_snapshot,
            live_webhook.data,
        )
        egress_policy = self._webhook_egress_policy()
        try:
            webhooking.validate_webhook_url(
                delivery_webhook.get("url"),
                egress_policy=egress_policy,
            )
        except ValueError as exc:
            return self._mark_webhook_delivery_dead(
                delivery,
                response_status=None,
                error_type="WebhookTargetBlocked",
                error_summary=str(exc),
            )

        try:
            response = webhooking.send_webhook_delivery(
                webhook=delivery_webhook,
                payload=delivery.payload,
                timeout_seconds=self.settings.webhook_timeout_seconds,
                egress_policy=egress_policy,
            )
            if 200 <= response.status_code < 300:
                delivery.status = "done"
                delivery.response_status = response.status_code
                delivery.next_attempt_at = None
                delivery.error = None
                _clear_webhook_delivery_claim(delivery)
                delivery.updated = utc_now()
                self.repository.save_webhook_delivery(delivery)
                return delivery

            error_type = "HTTPError"
            error_summary = f"HTTP {response.status_code}: {response.reason}"
            if (
                response.status_code in webhooking.RETRIABLE_STATUS_CODES
                and delivery.attempt_count < self.settings.webhook_max_attempts
            ):
                return self._mark_webhook_delivery_retry(
                    delivery,
                    response_status=response.status_code,
                    error_type=error_type,
                    error_summary=error_summary,
                )
            return self._mark_webhook_delivery_dead(
                delivery,
                response_status=response.status_code,
                error_type=error_type,
                error_summary=error_summary,
            )
        except webhooking.WebhookEgressError as exc:
            return self._mark_webhook_delivery_dead(
                delivery,
                response_status=None,
                error_type="WebhookTargetBlocked",
                error_summary=str(exc),
            )
        except requests.RequestException as exc:
            error_type = type(exc).__name__
            error_summary = str(exc)
            if delivery.attempt_count < self.settings.webhook_max_attempts:
                return self._mark_webhook_delivery_retry(
                    delivery,
                    response_status=None,
                    error_type=error_type,
                    error_summary=error_summary,
                )
            return self._mark_webhook_delivery_dead(
                delivery,
                response_status=None,
                error_type=error_type,
                error_summary=error_summary,
            )

    def _webhook_egress_policy(self) -> webhooking.WebhookEgressPolicy:
        return webhooking.WebhookEgressPolicy(
            allow_private_targets=self.settings.webhook_allow_private_targets,
            allowed_hosts=tuple(self.settings.webhook_allowed_hosts),
        )

    def _mark_webhook_delivery_retry(
        self,
        delivery: WebhookDeliveryRecord,
        *,
        response_status: int | None,
        error_type: str,
        error_summary: str,
    ) -> WebhookDeliveryRecord:
        delivery.status = "pending"
        delivery.response_status = response_status
        delivery.error = DomainErrorPayload.create(error_type, error_summary)
        delivery.next_attempt_at = utc_now() + timedelta(
            seconds=webhooking.retry_delay(delivery.attempt_count)
        )
        _clear_webhook_delivery_claim(delivery)
        delivery.updated = utc_now()
        self.repository.save_webhook_delivery(delivery)
        return delivery

    def _mark_webhook_delivery_dead(
        self,
        delivery: WebhookDeliveryRecord,
        *,
        response_status: int | None,
        error_type: str,
        error_summary: str,
        mark_webhook_error: bool = True,
    ) -> WebhookDeliveryRecord:
        domain_error = DomainErrorPayload.create(error_type, error_summary)
        delivery.status = "dead"
        delivery.response_status = response_status
        delivery.error = domain_error
        delivery.next_attempt_at = None
        _clear_webhook_delivery_claim(delivery)
        delivery.updated = utc_now()
        self.repository.save_webhook_delivery(delivery)
        if mark_webhook_error:
            webhook = self.repository.get_webhook(delivery.webhook_id)
            if webhook is not None:
                webhook.status = "error"
                webhook.data["error"] = domain_error.to_json_dict()
                self.repository.save_webhook(webhook)
        return delivery


def _clear_webhook_delivery_claim(delivery: WebhookDeliveryRecord) -> None:
    delivery.claimed_at = None
    delivery.claimed_by = None
    delivery.claim_expires_at = None
