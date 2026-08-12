from __future__ import annotations

from collections.abc import Iterable
from contextlib import AbstractContextManager
from datetime import datetime
from typing import Protocol
from uuid import UUID

from tamoss.domain.listings import DeleteRequestSortBy, FlowSortBy, SourceSortBy
from tamoss.domain.model import (
    DeletionRequestRecord,
    FlowRecord,
    MediaObjectRecord,
    ObjectCleanupRecord,
    ObjectCopyRecord,
    ProfileRecord,
    SegmentRecord,
    ServiceMetadata,
    SourceRecord,
    SourceRelationships,
    StorageBackend,
    WebhookDeliveryRecord,
    WebhookRecord,
)
from tamoss.domain.pagination import Page
from tamoss.domain.segments import SegmentDeleteFilter, SegmentTimerangeBounds


class TransactionalRepository(Protocol):
    def unit_of_work(self) -> AbstractContextManager[object]: ...


class StorageBackendRepository(Protocol):
    def list_storage_backends(self) -> list[StorageBackend]: ...

    def list_storage_backends_page(
        self,
        *,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        reverse_order: bool,
        page: str | None,
        limit: int | None,
    ) -> Page[StorageBackend]: ...

    def default_storage_backend(self) -> StorageBackend | None: ...

    def get_storage_backend(self, storage_id: UUID) -> StorageBackend | None: ...


class ServiceRepository(StorageBackendRepository, Protocol):
    def get_service_metadata(self) -> ServiceMetadata | None: ...

    def save_service_metadata(self, metadata: ServiceMetadata) -> None: ...


class ProfileRepository(TransactionalRepository, Protocol):
    def list_profiles_page(
        self,
        *,
        format: str | None,
        codec: str | None,
        label: str | None,
        page: str | None,
        limit: int | None,
    ) -> Page[ProfileRecord]: ...

    def get_profile(self, profile_id: UUID) -> ProfileRecord | None: ...

    def create_profile(self, profile: ProfileRecord) -> bool: ...

    def lock_profile(self, profile_id: UUID) -> None: ...

    def count_flows_by_profile(self, profile_id: UUID) -> int: ...

    def delete_profile(self, profile_id: UUID) -> bool: ...


class FlowLookupRepository(Protocol):
    def get_flow(self, flow_id: UUID) -> FlowRecord | None: ...


class FlowCollectionRepository(FlowLookupRepository, Protocol):
    def list_flows(self) -> list[FlowRecord]: ...

    def list_flows_by_source(self, source_id: UUID) -> list[FlowRecord]: ...

    def list_flows_collecting(self, flow_ids: Iterable[UUID]) -> list[FlowRecord]: ...

    def save_flow(self, flow: FlowRecord) -> None: ...


class FlowRepository(TransactionalRepository, FlowCollectionRepository, Protocol):
    def list_flows_page(
        self,
        *,
        source_id: UUID | None,
        timerange_start: int | None,
        timerange_end: int | None,
        timerange_is_empty: bool,
        timerange_is_point: bool,
        format: str | None,
        profile_id: UUID | None,
        status: str | None,
        init_segments: bool | None,
        collected_by_ids: set[UUID] | None,
        top_level_only: bool,
        sort_by: FlowSortBy,
        reverse_order: bool,
        codec: str | None,
        label: str | None,
        frame_width: int | None,
        frame_height: int | None,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[FlowRecord]: ...

    def flow_timeranges(self, flow_ids: Iterable[UUID]) -> dict[UUID, str]: ...

    def has_segments(self, flow_id: UUID) -> bool: ...

    def lock_flow_segments(self, flow_id: UUID) -> None: ...

    def list_segments(self, flow_id: UUID) -> list[SegmentRecord]: ...

    def list_flow_ids_matching_tags_page(
        self,
        *,
        flow_ids: Iterable[UUID],
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[UUID]: ...

    def get_source(self, source_id: UUID) -> SourceRecord | None: ...

    def source_relationships_for(
        self,
        source_ids: Iterable[UUID],
    ) -> dict[UUID, SourceRelationships]: ...

    def save_source(self, source: SourceRecord) -> None: ...

    def save_flow(self, flow: FlowRecord) -> None: ...


class SourceRepository(Protocol):
    def list_sources_page(
        self,
        *,
        label: str | None,
        format: str | None,
        collected_by_ids: set[UUID] | None,
        top_level_only: bool,
        sort_by: SourceSortBy,
        reverse_order: bool,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[SourceRecord]: ...

    def get_source(self, source_id: UUID) -> SourceRecord | None: ...

    def save_source(self, source: SourceRecord) -> None: ...

    def list_flows(self) -> list[FlowRecord]: ...

    def source_relationships_for(
        self,
        source_ids: Iterable[UUID],
    ) -> dict[UUID, SourceRelationships]: ...


class WebhookEventRepository(Protocol):
    def list_webhooks(self) -> list[WebhookRecord]: ...

    def save_webhook(self, webhook: WebhookRecord) -> None: ...

    def save_webhook_delivery(self, delivery: WebhookDeliveryRecord) -> None: ...


class WebhookResourceRepository(Protocol):
    def list_flows_by_source(self, source_id: UUID) -> list[FlowRecord]: ...

    def list_flows_collecting(self, flow_ids: Iterable[UUID]) -> list[FlowRecord]: ...

    def get_source(self, source_id: UUID) -> SourceRecord | None: ...

    def source_relationships_for(
        self,
        source_ids: Iterable[UUID],
    ) -> dict[UUID, SourceRelationships]: ...


class WebhookRepository(WebhookEventRepository, Protocol):
    def list_webhooks_page(
        self,
        *,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        reverse_order: bool,
        page: str | None,
        limit: int | None,
    ) -> Page[WebhookRecord]: ...

    def get_webhook(self, webhook_id: UUID) -> WebhookRecord | None: ...

    def delete_webhook(self, webhook_id: UUID) -> None: ...

    def claim_webhook_deliveries(
        self,
        *,
        worker_id: str,
        limit: int,
        lease_seconds: int,
    ) -> list[WebhookDeliveryRecord]: ...

    def get_webhook_delivery(
        self,
        delivery_id: UUID,
    ) -> WebhookDeliveryRecord | None: ...


class SegmentRepository(TransactionalRepository, StorageBackendRepository, Protocol):
    def lock_flow_segments(self, flow_id: UUID) -> None: ...

    def lock_objects(self, object_ids: Iterable[str]) -> None: ...

    def get_flow(self, flow_id: UUID) -> FlowRecord | None: ...

    def get_objects(
        self, object_ids: Iterable[str]
    ) -> dict[str, MediaObjectRecord]: ...

    def list_segments_overlapping(
        self,
        *,
        flow_id: UUID,
        timeranges: Iterable[SegmentTimerangeBounds],
    ) -> list[SegmentRecord]: ...

    def save_registered_segments(
        self,
        *,
        flow: FlowRecord,
        media_objects: Iterable[MediaObjectRecord],
        segments: Iterable[SegmentRecord],
    ) -> None: ...

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
    ) -> Page[SegmentRecord]: ...


class ObjectRepository(
    TransactionalRepository,
    StorageBackendRepository,
    Protocol,
):
    def lock_objects(self, object_ids: Iterable[str]) -> None: ...

    def get_object(self, object_id: str) -> MediaObjectRecord | None: ...

    def save_object(self, media_object: MediaObjectRecord) -> None: ...

    def claim_object_copies(
        self,
        *,
        worker_id: str,
        limit: int,
        lease_seconds: int,
    ) -> list[ObjectCopyRecord]: ...

    def save_object_copy(self, copy: ObjectCopyRecord) -> None: ...


class ObjectCleanupRepository(Protocol):
    def save_object_cleanup(self, cleanup: ObjectCleanupRecord) -> None: ...


class StorageRepository(
    TransactionalRepository,
    StorageBackendRepository,
    Protocol,
):
    def lock_objects(self, object_ids: Iterable[str]) -> None: ...

    def create_object(self, media_object: MediaObjectRecord) -> bool: ...

    def create_objects(
        self, media_objects: Iterable[MediaObjectRecord]
    ) -> set[str]: ...


class DeletionRepository(
    TransactionalRepository,
    StorageBackendRepository,
    FlowCollectionRepository,
    ObjectCleanupRepository,
    Protocol,
):
    def lock_flow_segments(self, flow_id: UUID) -> None: ...

    def lock_objects(self, object_ids: Iterable[str]) -> None: ...

    def delete_flow(self, flow_id: UUID) -> None: ...

    def get_source(self, source_id: UUID) -> SourceRecord | None: ...

    def delete_source(self, source_id: UUID) -> None: ...

    def list_delete_requests(self) -> list[DeletionRequestRecord]: ...

    def list_delete_requests_page(
        self,
        *,
        sort_by: DeleteRequestSortBy,
        reverse_order: bool,
        retention_seconds: int,
        page: str | None,
        limit: int | None,
    ) -> Page[DeletionRequestRecord]: ...

    def get_delete_request(self, request_id: UUID) -> DeletionRequestRecord | None: ...

    def save_delete_request(self, request: DeletionRequestRecord) -> None: ...

    def claim_delete_requests(
        self,
        *,
        worker_id: str,
        limit: int,
        lease_seconds: int,
    ) -> list[DeletionRequestRecord]: ...

    def list_object_cleanups(
        self,
        *,
        delete_request_id: UUID | None = None,
        statuses: set[str] | None = None,
    ) -> list[ObjectCleanupRecord]: ...

    def claim_object_cleanups(
        self,
        *,
        worker_id: str,
        limit: int,
        lease_seconds: int,
    ) -> list[ObjectCleanupRecord]: ...

    def get_objects(
        self, object_ids: Iterable[str]
    ) -> dict[str, MediaObjectRecord]: ...

    def list_unreferenced_objects_created_before(
        self,
        *,
        before: datetime,
        limit: int,
    ) -> list[MediaObjectRecord]: ...

    def save_object(self, media_object: MediaObjectRecord) -> None: ...

    def delete_object(self, object_id: str) -> None: ...

    def list_segments(self, flow_id: UUID) -> list[SegmentRecord]: ...

    def list_segments_for_objects(
        self,
        *,
        flow_id: UUID,
        object_ids: Iterable[str],
    ) -> list[SegmentRecord]: ...

    def segment_delete_timerange(self, delete_filter: SegmentDeleteFilter) -> str: ...

    def delete_segment_batch(
        self,
        delete_filter: SegmentDeleteFilter,
        *,
        limit: int,
    ) -> list[SegmentRecord]: ...
