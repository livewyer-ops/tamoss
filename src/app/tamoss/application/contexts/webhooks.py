from __future__ import annotations

from tamoss.application.contexts._shared import (
    DEFAULT_WORKER_ID,
    DEFAULT_WORKER_LEASE_SECONDS,
    UUID,
    BadRequest,
    FlowRecord,
    NotFound,
    Page,
    SegmentRecord,
    SourceRecord,
    UseCaseContext,
    WebhookDeliveryRecord,
    WebhookPost,
    WebhookPut,
    WebhookRecord,
    _clear_worker_claim,
    _timerange_covering_segments,
    error_payload,
    timedelta,
    utc_now,
    uuid4,
    webhooking,
)


class WebhookUseCases(UseCaseContext):
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

    def create_webhook(self, webhook_post: WebhookPost) -> WebhookRecord:
        data = webhook_post.model_dump(exclude_none=True, mode="json")
        try:
            webhooking.validate_webhook_configuration(data)
        except ValueError as exc:
            raise BadRequest("Bad request. Invalid webhook payload.") from exc
        status = data.pop("status", None) or "created"
        data.pop("tags", None)
        data.pop("error", None)
        webhook = WebhookRecord(
            id=uuid4(),
            data=data,
            status=status,
            tags=dict(webhook_post.tags or {}),
        )
        self.repository.save_webhook(webhook)
        return webhook

    def put_webhook(
        self, *, webhook_id: UUID, webhook_put: WebhookPut
    ) -> WebhookRecord:
        if webhook_put.id != webhook_id:
            raise NotFound("The requested Webhook ID in the path is invalid.")
        existing = self.get_webhook(webhook_id)
        if existing.status == "error" and webhook_put.status == "disabled":
            raise BadRequest(
                "Bad request. The Webhook is currently in an error status and "
                "therefore cannot be updated to disabled."
            )

        data = webhook_put.model_dump(exclude_none=True, mode="json")
        try:
            webhooking.validate_webhook_configuration(data)
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
            tags=dict(webhook_put.tags or {}),
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

        try:
            response = webhooking.send_webhook_delivery(
                webhook=delivery.webhook_snapshot,
                payload=delivery.payload,
            )
            if 200 <= response.status_code < 300:
                delivery.status = "done"
                delivery.response_status = response.status_code
                delivery.next_attempt_at = None
                delivery.error = None
                _clear_worker_claim(delivery)
                delivery.updated = utc_now()
                self.repository.save_webhook_delivery(delivery)
                return delivery

            error_type = "HTTPError"
            error_summary = f"HTTP {response.status_code}: {response.reason}"
            if (
                response.status_code in webhooking.RETRIABLE_STATUS_CODES
                and delivery.attempt_count < webhooking.max_attempts()
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
        except Exception as exc:
            error_type = type(exc).__name__
            error_summary = str(exc)
            if delivery.attempt_count < webhooking.max_attempts():
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

    def _publish_flow_event(self, event_type: str, flow: FlowRecord) -> None:
        source = self._source_for_flow(flow)
        self._publish_webhook_event(
            event_type=event_type,
            event_factory=lambda _webhook: {"flow": self._flow_payload(flow)},
            flow=flow,
            source=source,
        )

    def _publish_flow_deleted(self, flow: FlowRecord) -> None:
        self._publish_webhook_event(
            event_type="flows/deleted",
            event_factory=lambda _webhook: {"flow_id": str(flow.id)},
            flow=flow,
            source=self._source_for_flow(flow),
        )

    def _publish_segments_added(
        self, flow: FlowRecord, segments: list[SegmentRecord]
    ) -> None:
        self._publish_webhook_event(
            event_type="flows/segments_added",
            event_factory=lambda webhook: {
                "flow_id": str(flow.id),
                "segments": [
                    self._segment_payload(segment, webhook.data) for segment in segments
                ],
            },
            flow=flow,
            source=self._source_for_flow(flow),
        )

    def _publish_segments_deleted(
        self, flow: FlowRecord, segments: list[SegmentRecord]
    ) -> None:
        self._publish_webhook_event(
            event_type="flows/segments_deleted",
            event_factory=lambda _webhook: {
                "flow_id": str(flow.id),
                "timerange": _timerange_covering_segments(segments),
            },
            flow=flow,
            source=self._source_for_flow(flow),
        )

    def _publish_source_event(self, event_type: str, source: SourceRecord) -> None:
        self._publish_webhook_event(
            event_type=event_type,
            event_factory=lambda _webhook: {"source": self._source_payload(source)},
            flow=None,
            source=source,
        )

    def _publish_source_deleted(self, source: SourceRecord) -> None:
        self._publish_webhook_event(
            event_type="sources/deleted",
            event_factory=lambda _webhook: {"source_id": str(source.id)},
            flow=None,
            source=source,
        )

    def _publish_webhook_event(
        self,
        *,
        event_type: str,
        event_factory,
        flow: FlowRecord | None,
        source: SourceRecord | None,
    ) -> list[WebhookDeliveryRecord]:
        event_timestamp = utc_now()
        flow_ids = [str(flow.id)] if flow is not None else []
        source_ids = [str(source.id)] if source is not None else []
        flow_collected_by_ids = self._flow_collected_by_ids(flow)
        source_collected_by_ids = self._source_collected_by_ids(source)
        deliveries: list[WebhookDeliveryRecord] = []

        for webhook in self.repository.list_webhooks():
            if webhook.status not in {"created", "started"}:
                continue
            if not webhooking.webhook_matches(
                webhook.data,
                event_type=event_type,
                flow_ids=flow_ids,
                source_ids=source_ids,
                flow_collected_by_ids=flow_collected_by_ids,
                source_collected_by_ids=source_collected_by_ids,
            ):
                continue
            if webhook.status == "created":
                webhook.status = "started"
                self.repository.save_webhook(webhook)

            payload = {
                "event_timestamp": event_timestamp.isoformat(),
                "event_type": event_type,
                "event": event_factory(webhook),
            }
            delivery = WebhookDeliveryRecord(
                id=uuid4(),
                webhook_id=webhook.id,
                webhook_snapshot=webhooking.webhook_delivery_snapshot(
                    webhook.data, status=webhook.status
                ),
                event_type=event_type,
                event_timestamp=event_timestamp,
                payload=payload,
                status="pending",
                created=event_timestamp,
                updated=event_timestamp,
                next_attempt_at=event_timestamp,
            )
            self.repository.save_webhook_delivery(delivery)
            deliveries.append(delivery)
        return deliveries

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
        delivery.error = error_payload(error_type, error_summary)
        delivery.next_attempt_at = utc_now() + timedelta(
            seconds=webhooking.retry_delay(delivery.attempt_count)
        )
        _clear_worker_claim(delivery)
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
        error = error_payload(error_type, error_summary)
        delivery.status = "dead"
        delivery.response_status = response_status
        delivery.error = error
        delivery.next_attempt_at = None
        _clear_worker_claim(delivery)
        delivery.updated = utc_now()
        self.repository.save_webhook_delivery(delivery)
        if mark_webhook_error:
            webhook = self.repository.get_webhook(delivery.webhook_id)
            if webhook is not None:
                webhook.status = "error"
                webhook.data["error"] = error
                self.repository.save_webhook(webhook)
        return delivery
