from __future__ import annotations

from collections.abc import Iterable
from contextlib import AbstractContextManager
from dataclasses import dataclass
from typing import Protocol
from uuid import UUID

from tamoss.domain.model import (
    DeletionRequestRecord,
    FlowRecord,
    MediaObjectRecord,
    SegmentRecord,
    ServiceMetadata,
    SourceRecord,
    SourceRelationships,
    StorageBackend,
    WebhookDeliveryRecord,
    WebhookRecord,
)
from tamoss.domain.pagination import Page


@dataclass(frozen=True)
class SegmentTimerangeBounds:
    start: int
    end: int
    is_point: bool = False


class TamossRepository(Protocol):
    def unit_of_work(self) -> AbstractContextManager[TamossRepository]:
        raise NotImplementedError

    def lock_flow_segments(self, flow_id: UUID) -> None:
        raise NotImplementedError

    def get_service_metadata(self) -> ServiceMetadata | None:
        raise NotImplementedError

    def save_service_metadata(self, metadata: ServiceMetadata) -> None:
        raise NotImplementedError

    def list_storage_backends(self) -> list[StorageBackend]:
        raise NotImplementedError

    def default_storage_backend(self) -> StorageBackend | None:
        raise NotImplementedError

    def get_storage_backend(self, storage_id: UUID) -> StorageBackend | None:
        raise NotImplementedError

    def list_flows(self) -> list[FlowRecord]:
        raise NotImplementedError

    def list_flows_page(
        self,
        *,
        source_id: UUID | None,
        timerange_start: int | None,
        timerange_end: int | None,
        timerange_is_empty: bool,
        timerange_is_point: bool,
        format: str | None,
        codec: str | None,
        label: str | None,
        frame_width: int | None,
        frame_height: int | None,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[FlowRecord]:
        raise NotImplementedError

    def flow_timeranges(self, flow_ids: Iterable[UUID]) -> dict[UUID, str]:
        raise NotImplementedError

    def get_flow(self, flow_id: UUID) -> FlowRecord | None:
        raise NotImplementedError

    def save_flow(self, flow: FlowRecord) -> None:
        raise NotImplementedError

    def delete_flow(self, flow_id: UUID) -> None:
        raise NotImplementedError

    def get_source(self, source_id: UUID) -> SourceRecord | None:
        raise NotImplementedError

    def save_source(self, source: SourceRecord) -> None:
        raise NotImplementedError

    def delete_source(self, source_id: UUID) -> None:
        raise NotImplementedError

    def list_sources(self) -> list[SourceRecord]:
        raise NotImplementedError

    def list_sources_page(
        self,
        *,
        label: str | None,
        format: str | None,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[SourceRecord]:
        raise NotImplementedError

    def source_relationships_for(
        self, source_ids: Iterable[UUID]
    ) -> dict[UUID, SourceRelationships]:
        raise NotImplementedError

    def get_object(self, object_id: str) -> MediaObjectRecord | None:
        raise NotImplementedError

    def get_objects(self, object_ids: Iterable[str]) -> dict[str, MediaObjectRecord]:
        raise NotImplementedError

    def save_object(self, media_object: MediaObjectRecord) -> None:
        raise NotImplementedError

    def create_object(self, media_object: MediaObjectRecord) -> bool:
        raise NotImplementedError

    def delete_object(self, object_id: str) -> None:
        raise NotImplementedError

    def list_segments(self, flow_id: UUID) -> list[SegmentRecord]:
        raise NotImplementedError

    def list_segments_overlapping(
        self,
        *,
        flow_id: UUID,
        timeranges: Iterable[SegmentTimerangeBounds],
    ) -> list[SegmentRecord]:
        raise NotImplementedError

    def list_segments_page(
        self,
        *,
        flow_id: UUID,
        object_id: str | None,
        timerange_start: int | None,
        timerange_end: int | None,
        timerange_is_empty: bool,
        timerange_is_point: bool,
        reverse_order: bool,
        page: str | None,
        limit: int | None,
    ) -> Page[SegmentRecord]:
        raise NotImplementedError

    def append_segment(self, segment: SegmentRecord) -> None:
        raise NotImplementedError

    def save_registered_segments(
        self,
        *,
        flow: FlowRecord,
        media_objects: Iterable[MediaObjectRecord],
        segments: Iterable[SegmentRecord],
    ) -> None:
        raise NotImplementedError

    def replace_segments(self, flow_id: UUID, segments: list[SegmentRecord]) -> None:
        raise NotImplementedError

    def list_webhooks(self) -> list[WebhookRecord]:
        raise NotImplementedError

    def list_webhooks_page(
        self,
        *,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[WebhookRecord]:
        raise NotImplementedError

    def list_flow_ids_matching_tags_page(
        self,
        *,
        flow_ids: Iterable[UUID],
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[UUID]:
        raise NotImplementedError

    def get_webhook(self, webhook_id: UUID) -> WebhookRecord | None:
        raise NotImplementedError

    def save_webhook(self, webhook: WebhookRecord) -> None:
        raise NotImplementedError

    def delete_webhook(self, webhook_id: UUID) -> None:
        raise NotImplementedError

    def list_webhook_deliveries(self) -> list[WebhookDeliveryRecord]:
        raise NotImplementedError

    def get_webhook_delivery(self, delivery_id: UUID) -> WebhookDeliveryRecord | None:
        raise NotImplementedError

    def save_webhook_delivery(self, delivery: WebhookDeliveryRecord) -> None:
        raise NotImplementedError

    def claim_webhook_deliveries(
        self, *, worker_id: str, limit: int, lease_seconds: int
    ) -> list[WebhookDeliveryRecord]:
        raise NotImplementedError

    def list_delete_requests(self) -> list[DeletionRequestRecord]:
        raise NotImplementedError

    def get_delete_request(self, request_id: UUID) -> DeletionRequestRecord | None:
        raise NotImplementedError

    def save_delete_request(self, request: DeletionRequestRecord) -> None:
        raise NotImplementedError

    def claim_delete_requests(
        self, *, worker_id: str, limit: int, lease_seconds: int
    ) -> list[DeletionRequestRecord]:
        raise NotImplementedError
