"""Test-only repository fake for fast API and application tests."""

from __future__ import annotations

from collections.abc import Iterable, Iterator
from contextlib import contextmanager
from copy import deepcopy
from datetime import datetime, timedelta
from threading import RLock
from uuid import UUID

from tamoss.db.migrations import CURRENT_SCHEMA_REVISION
from tamoss.domain import flow_collections
from tamoss.domain import segments as segment_domain
from tamoss.domain.exceptions import SEGMENT_OVERLAP_MESSAGE, SegmentOverlapError
from tamoss.domain.model import (
    DeletionRequestRecord,
    DomainErrorPayload,
    FlowRecord,
    MediaObjectRecord,
    ObjectCleanupRecord,
    ObjectCopyRecord,
    SegmentRecord,
    ServiceMetadata,
    SourceRecord,
    SourceRelationships,
    StorageBackend,
    WebhookDeliveryRecord,
    WebhookRecord,
    utc_now,
)
from tamoss.domain.pagination import Page, page_sequence
from tamoss.domain.segments import SegmentDeleteFilter, SegmentTimerangeBounds
from tamoss.domain.tags import tags_match
from tamoss.errors import normalize_error_payload

WorkerRecord = (
    WebhookDeliveryRecord
    | DeletionRequestRecord
    | ObjectCleanupRecord
    | ObjectCopyRecord
)


class FakeTamossRepository:
    def __init__(
        self,
        storage_backend: StorageBackend,
        *,
        storage_backends: Iterable[StorageBackend] | None = None,
    ):
        self._lock = RLock()
        self._storage_backends = list(storage_backends or [storage_backend])
        self._storage_backend = next(
            (backend for backend in self._storage_backends if backend.default_storage),
            self._storage_backends[0],
        )
        self._flows: dict[UUID, FlowRecord] = {}
        self._sources: dict[UUID, SourceRecord] = {}
        self._objects: dict[str, MediaObjectRecord] = {}
        self._segments: dict[UUID, list[SegmentRecord]] = {}
        self._webhooks: dict[UUID, WebhookRecord] = {}
        self._webhook_deliveries: dict[UUID, WebhookDeliveryRecord] = {}
        self._delete_requests: dict[UUID, DeletionRequestRecord] = {}
        self._object_cleanups: dict[UUID, ObjectCleanupRecord] = {}
        self._object_copies: dict[UUID, ObjectCopyRecord] = {}
        self._service_metadata: ServiceMetadata | None = None

    @property
    def service_repository(self) -> FakeTamossRepository:
        return self

    @property
    def webhook_repository(self) -> FakeTamossRepository:
        return self

    @property
    def deletion_repository(self) -> FakeTamossRepository:
        return self

    @property
    def source_repository(self) -> FakeTamossRepository:
        return self

    @property
    def flow_repository(self) -> FakeTamossRepository:
        return self

    @property
    def storage_repository(self) -> FakeTamossRepository:
        return self

    @property
    def segment_repository(self) -> FakeTamossRepository:
        return self

    @property
    def object_repository(self) -> FakeTamossRepository:
        return self

    @contextmanager
    def unit_of_work(self) -> Iterator[FakeTamossRepository]:
        with self._lock:
            snapshot = (
                deepcopy(self._flows),
                deepcopy(self._sources),
                deepcopy(self._objects),
                deepcopy(self._segments),
                deepcopy(self._webhooks),
                deepcopy(self._webhook_deliveries),
                deepcopy(self._delete_requests),
                deepcopy(self._object_cleanups),
                deepcopy(self._object_copies),
                deepcopy(self._service_metadata),
            )
            try:
                yield self
            except Exception:
                (
                    self._flows,
                    self._sources,
                    self._objects,
                    self._segments,
                    self._webhooks,
                    self._webhook_deliveries,
                    self._delete_requests,
                    self._object_cleanups,
                    self._object_copies,
                    self._service_metadata,
                ) = snapshot
                raise

    def lock_flow_segments(self, flow_id: UUID) -> None:
        return None

    def get_service_metadata(self) -> ServiceMetadata | None:
        with self._lock:
            return self._service_metadata

    def save_service_metadata(self, metadata: ServiceMetadata) -> None:
        with self._lock:
            self._service_metadata = metadata

    def current_schema_revision(self) -> str:
        return CURRENT_SCHEMA_REVISION

    def list_storage_backends(self) -> list[StorageBackend]:
        with self._lock:
            return list(self._storage_backends)

    def default_storage_backend(self) -> StorageBackend | None:
        with self._lock:
            return self._storage_backend

    def get_storage_backend(self, storage_id: UUID) -> StorageBackend | None:
        with self._lock:
            for backend in self._storage_backends:
                if storage_id == backend.id:
                    return backend
            return None

    def list_flows(self) -> list[FlowRecord]:
        with self._lock:
            return list(self._flows.values())

    def list_flows_by_source(self, source_id: UUID) -> list[FlowRecord]:
        with self._lock:
            return [
                flow for flow in self._flows.values() if flow.source_id == source_id
            ]

    def list_flows_collecting(self, flow_ids: Iterable[UUID]) -> list[FlowRecord]:
        requested_ids = set(flow_ids)
        with self._lock:
            flows = list(self._flows.values())
        return [
            flow
            for flow in flows
            if any(
                flow_collections.collection_child_id(item) in requested_ids
                for item in flow_collections.flow_collection(flow)
            )
        ]

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
        with self._lock:
            flows = list(self._flows.values())
            segments = {
                flow_id: list(flow_segments)
                for flow_id, flow_segments in self._segments.items()
            }
        collected_by = flow_collections.collected_by_by_flow_id(flows)
        flows = [
            flow_collections.flow_with_collected_by(
                flow,
                collected_by.get(flow.id, []),
            )
            for flow in flows
        ]
        if source_id is not None:
            flows = [flow for flow in flows if flow.source_id == source_id]
        if format is not None:
            flows = [flow for flow in flows if flow.format == format]
        if codec is not None:
            flows = [
                flow
                for flow in flows
                if flow_collections.flow_data_value(flow, "codec") == codec
            ]
        if label is not None:
            flows = [flow for flow in flows if flow.data.get("label") == label]
        if frame_width is not None:
            flows = [
                flow
                for flow in flows
                if flow_collections.flow_data_value(flow, "frame_width") == frame_width
            ]
        if frame_height is not None:
            flows = [
                flow
                for flow in flows
                if (
                    flow_collections.flow_data_value(flow, "frame_height")
                    == frame_height
                )
            ]
        if timerange_is_empty:
            flows = [flow for flow in flows if not segments.get(flow.id)]
        elif timerange_start is not None or timerange_end is not None:
            flows = [
                flow
                for flow in flows
                if segment_domain.flow_timerange_matches(
                    segments.get(flow.id, []),
                    start=timerange_start,
                    end=timerange_end,
                    requested_is_point=timerange_is_point,
                )
            ]
        flows = [
            flow for flow in flows if tags_match(flow.tags, tag_values, tag_exists)
        ]
        flows.sort(key=lambda flow: str(flow.id))
        return page_sequence(flows, page=page, limit=limit)

    def flow_timeranges(self, flow_ids: Iterable[UUID]) -> dict[UUID, str]:
        requested_ids = list(dict.fromkeys(flow_ids))
        with self._lock:
            segments = {
                flow_id: list(self._segments.get(flow_id, []))
                for flow_id in requested_ids
            }
        return {
            flow_id: segment_domain.timerange_union(segments[flow_id])
            for flow_id in requested_ids
        }

    def get_flow(self, flow_id: UUID) -> FlowRecord | None:
        with self._lock:
            return self._flows.get(flow_id)

    def save_flow(self, flow: FlowRecord) -> None:
        with self._lock:
            self._flows[flow.id] = flow

    def delete_flow(self, flow_id: UUID) -> None:
        with self._lock:
            self._flows.pop(flow_id, None)

    def get_source(self, source_id: UUID) -> SourceRecord | None:
        with self._lock:
            return self._sources.get(source_id)

    def save_source(self, source: SourceRecord) -> None:
        with self._lock:
            self._sources[source.id] = source

    def delete_source(self, source_id: UUID) -> None:
        with self._lock:
            self._sources.pop(source_id, None)

    def list_sources(self) -> list[SourceRecord]:
        with self._lock:
            return list(self._sources.values())

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
        with self._lock:
            sources = list(self._sources.values())
        if label is not None:
            sources = [source for source in sources if source.label == label]
        if format is not None:
            sources = [source for source in sources if source.format == format]
        sources = [
            source
            for source in sources
            if tags_match(source.tags, tag_values, tag_exists)
        ]
        sources.sort(key=lambda source: str(source.id))
        return page_sequence(sources, page=page, limit=limit)

    def source_relationships_for(
        self, source_ids: Iterable[UUID]
    ) -> dict[UUID, SourceRelationships]:
        requested_ids = list(source_ids)
        relationships = {
            source_id: SourceRelationships(source_collection=[], collected_by=[])
            for source_id in requested_ids
        }
        with self._lock:
            flows = list(self._flows.values())
        flows_by_id = {flow.id: flow for flow in flows}
        for parent_flow in flows:
            if parent_flow.source_id is None:
                continue
            for item in flow_collections.flow_collection(parent_flow):
                child_flow_id = flow_collections.collection_child_id(item)
                if child_flow_id is None:
                    continue
                child_flow = flows_by_id.get(child_flow_id)
                if child_flow is None or child_flow.source_id is None:
                    continue
                role = item.get("role")
                if not isinstance(role, str):
                    continue
                if parent_flow.source_id in relationships:
                    source_item = {"id": str(child_flow.source_id), "role": role}
                    source_collection = relationships[
                        parent_flow.source_id
                    ].source_collection
                    if source_item not in source_collection:
                        source_collection.append(source_item)
                if child_flow.source_id in relationships:
                    collected_by = relationships[child_flow.source_id].collected_by
                    if parent_flow.source_id not in collected_by:
                        collected_by.append(parent_flow.source_id)
        return relationships

    def get_object(self, object_id: str) -> MediaObjectRecord | None:
        with self._lock:
            return self._objects.get(object_id)

    def get_objects(self, object_ids: Iterable[str]) -> dict[str, MediaObjectRecord]:
        requested_ids = set(object_ids)
        with self._lock:
            return {
                object_id: media_object
                for object_id, media_object in self._objects.items()
                if object_id in requested_ids
            }

    def save_object(self, media_object: MediaObjectRecord) -> None:
        with self._lock:
            self._objects[media_object.id] = media_object

    def create_object(self, media_object: MediaObjectRecord) -> bool:
        with self._lock:
            if media_object.id in self._objects:
                return False
            self._objects[media_object.id] = media_object
            return True

    def create_objects(self, media_objects: Iterable[MediaObjectRecord]) -> set[str]:
        created: set[str] = set()
        with self._lock:
            for media_object in media_objects:
                if media_object.id in self._objects:
                    continue
                self._objects[media_object.id] = media_object
                created.add(media_object.id)
        return created

    def delete_object(self, object_id: str) -> None:
        with self._lock:
            self._objects.pop(object_id, None)

    def list_unreferenced_objects_created_before(
        self,
        *,
        before: datetime,
        limit: int,
    ) -> list[MediaObjectRecord]:
        if limit < 1:
            return []
        with self._lock:
            candidates = [
                media_object
                for media_object in self._objects.values()
                if not media_object.referenced_by_flows
                and media_object.created < before
            ]
        candidates.sort(
            key=lambda media_object: (media_object.created, media_object.id)
        )
        return candidates[:limit]

    def list_segments(self, flow_id: UUID) -> list[SegmentRecord]:
        with self._lock:
            return list(self._segments.get(flow_id, []))

    def list_segments_for_objects(
        self, *, flow_id: UUID, object_ids: Iterable[str]
    ) -> list[SegmentRecord]:
        requested_ids = set(object_ids)
        if not requested_ids:
            return []
        with self._lock:
            segments = [
                segment
                for segment in self._segments.get(flow_id, [])
                if segment.object_id in requested_ids
            ]
        segments.sort(key=segment_domain.segment_sort_key)
        return segments

    def segment_delete_timerange(self, delete_filter: SegmentDeleteFilter) -> str:
        with self._lock:
            matching = [
                segment
                for segment in self._segments.get(delete_filter.flow_id, [])
                if _matches_segment_delete_filter(segment, delete_filter)
            ]
        return segment_domain.timerange_union(matching)

    def delete_segment_batch(
        self, delete_filter: SegmentDeleteFilter, *, limit: int
    ) -> list[SegmentRecord]:
        if limit < 1 or delete_filter.timerange_is_empty:
            return []
        with self._lock:
            segments = list(self._segments.get(delete_filter.flow_id, []))
            matching = [
                segment
                for segment in segments
                if _matches_segment_delete_filter(segment, delete_filter)
            ]
            matching.sort(key=segment_domain.segment_sort_key)
            deleted = matching[:limit]
            deleted_keys = {
                (segment.object_id, segment.timerange) for segment in deleted
            }
            remaining = [
                segment
                for segment in segments
                if (segment.object_id, segment.timerange) not in deleted_keys
            ]
            if remaining:
                self._segments[delete_filter.flow_id] = remaining
            else:
                self._segments.pop(delete_filter.flow_id, None)
            return deleted

    def list_segments_overlapping(
        self,
        *,
        flow_id: UUID,
        timeranges: Iterable[SegmentTimerangeBounds],
    ) -> list[SegmentRecord]:
        bounds = list(timeranges)
        if not bounds:
            return []
        with self._lock:
            segments = list(self._segments.get(flow_id, []))

        matching: list[SegmentRecord] = []
        seen: set[tuple[UUID, str, str]] = set()
        for segment in segments:
            if not any(
                segment_domain.segment_overlaps_bounds(
                    segment,
                    start=timerange.start,
                    end=timerange.end,
                    requested_is_point=timerange.is_point,
                )
                for timerange in bounds
            ):
                continue
            key = (segment.flow_id, segment.object_id, segment.timerange)
            if key in seen:
                continue
            seen.add(key)
            matching.append(segment)
        matching.sort(key=segment_domain.segment_sort_key)
        return matching

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
        with self._lock:
            segments = list(self._segments.get(flow_id, []))

        if object_id is not None:
            segments = [
                segment for segment in segments if segment.object_id == object_id
            ]
        if timerange_is_empty:
            segments = []
        elif timerange_start is not None or timerange_end is not None:
            segments = [
                segment
                for segment in segments
                if segment_domain.segment_overlaps_bounds(
                    segment,
                    start=timerange_start,
                    end=timerange_end,
                    requested_is_point=timerange_is_point,
                )
            ]

        segments.sort(key=segment_domain.segment_sort_key, reverse=reverse_order)
        matched_timerange = segment_domain.timerange_union(segments)
        segment_page = page_sequence(segments, page=page, limit=limit)
        return Page(
            items=segment_page.items,
            limit=segment_page.limit,
            next_page=segment_page.next_page,
            timerange=matched_timerange,
        )

    def append_segment(self, segment: SegmentRecord) -> None:
        with self._lock:
            self._segments.setdefault(segment.flow_id, []).append(segment)

    def save_registered_segments(
        self,
        *,
        flow: FlowRecord,
        media_objects: Iterable[MediaObjectRecord],
        segments: Iterable[SegmentRecord],
    ) -> None:
        pending_segments = list(segments)
        with self._lock:
            segment_context: dict[UUID, list[SegmentRecord]] = {}
            for segment in pending_segments:
                known_segments = segment_context.setdefault(
                    segment.flow_id,
                    list(self._segments.get(segment.flow_id, [])),
                )
                segment_start, segment_end = segment_domain.segment_bounds(segment)
                if any(
                    segment_domain.segment_overlaps_bounds(
                        existing,
                        start=segment_start,
                        end=segment_end,
                        requested_is_point=False,
                    )
                    for existing in known_segments
                ):
                    raise SegmentOverlapError(SEGMENT_OVERLAP_MESSAGE)
                known_segments.append(segment)
            self._flows[flow.id] = flow
            for media_object in media_objects:
                self._objects[media_object.id] = media_object
            for segment in pending_segments:
                self._segments.setdefault(segment.flow_id, []).append(segment)

    def list_webhooks(self) -> list[WebhookRecord]:
        with self._lock:
            return list(self._webhooks.values())

    def list_webhooks_page(
        self,
        *,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[WebhookRecord]:
        with self._lock:
            webhooks = list(self._webhooks.values())
        webhooks = [
            webhook
            for webhook in webhooks
            if tags_match(webhook.tags, tag_values, tag_exists)
        ]
        webhooks.sort(key=lambda webhook: str(webhook.id))
        return page_sequence(webhooks, page=page, limit=limit)

    def list_flow_ids_matching_tags_page(
        self,
        *,
        flow_ids: Iterable[UUID],
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[UUID]:
        requested_ids = set(flow_ids)
        with self._lock:
            flows = [
                flow
                for flow_id, flow in self._flows.items()
                if flow_id in requested_ids
            ]
        matched_ids = [
            flow.id for flow in flows if tags_match(flow.tags, tag_values, tag_exists)
        ]
        matched_ids.sort(key=str)
        return page_sequence(matched_ids, page=page, limit=limit)

    def get_webhook(self, webhook_id: UUID) -> WebhookRecord | None:
        with self._lock:
            return self._webhooks.get(webhook_id)

    def save_webhook(self, webhook: WebhookRecord) -> None:
        with self._lock:
            _normalize_webhook_error(webhook)
            self._webhooks[webhook.id] = webhook

    def delete_webhook(self, webhook_id: UUID) -> None:
        with self._lock:
            self._webhooks.pop(webhook_id, None)

    def list_webhook_deliveries(self) -> list[WebhookDeliveryRecord]:
        with self._lock:
            return list(self._webhook_deliveries.values())

    def get_webhook_delivery(self, delivery_id: UUID) -> WebhookDeliveryRecord | None:
        with self._lock:
            return self._webhook_deliveries.get(delivery_id)

    def save_webhook_delivery(self, delivery: WebhookDeliveryRecord) -> None:
        with self._lock:
            delivery.error = DomainErrorPayload.from_json_dict(delivery.error)
            self._webhook_deliveries[delivery.id] = delivery

    def claim_webhook_deliveries(
        self, *, worker_id: str, limit: int, lease_seconds: int
    ) -> list[WebhookDeliveryRecord]:
        with self._lock:
            return _claim_worker_records(
                self._webhook_deliveries.values(),
                statuses={"pending", "started"},
                worker_id=worker_id,
                limit=limit,
                lease_seconds=lease_seconds,
            )

    def list_delete_requests(self) -> list[DeletionRequestRecord]:
        with self._lock:
            return list(self._delete_requests.values())

    def get_delete_request(self, request_id: UUID) -> DeletionRequestRecord | None:
        with self._lock:
            return self._delete_requests.get(request_id)

    def save_delete_request(self, request: DeletionRequestRecord) -> None:
        with self._lock:
            request.error = DomainErrorPayload.from_json_dict(request.error)
            self._delete_requests[request.id] = request

    def claim_delete_requests(
        self, *, worker_id: str, limit: int, lease_seconds: int
    ) -> list[DeletionRequestRecord]:
        with self._lock:
            return _claim_worker_records(
                self._delete_requests.values(),
                statuses={"created", "started", "error"},
                worker_id=worker_id,
                limit=limit,
                lease_seconds=lease_seconds,
            )

    def list_object_cleanups(
        self,
        *,
        delete_request_id: UUID | None = None,
        statuses: set[str] | None = None,
    ) -> list[ObjectCleanupRecord]:
        with self._lock:
            cleanups = list(self._object_cleanups.values())
        if delete_request_id is not None:
            cleanups = [
                cleanup
                for cleanup in cleanups
                if cleanup.delete_request_id == delete_request_id
            ]
        if statuses is not None:
            cleanups = [cleanup for cleanup in cleanups if cleanup.status in statuses]
        cleanups.sort(key=lambda cleanup: (cleanup.created, str(cleanup.id)))
        return cleanups

    def save_object_cleanup(self, cleanup: ObjectCleanupRecord) -> None:
        with self._lock:
            cleanup.error = DomainErrorPayload.from_json_dict(cleanup.error)
            self._object_cleanups[cleanup.id] = cleanup

    def claim_object_cleanups(
        self, *, worker_id: str, limit: int, lease_seconds: int
    ) -> list[ObjectCleanupRecord]:
        with self._lock:
            return _claim_worker_records(
                self._object_cleanups.values(),
                statuses={"pending", "started", "error"},
                worker_id=worker_id,
                limit=limit,
                lease_seconds=lease_seconds,
                unowned_only=True,
            )

    def list_object_copies(
        self, *, statuses: set[str] | None = None
    ) -> list[ObjectCopyRecord]:
        with self._lock:
            copies = list(self._object_copies.values())
        if statuses is not None:
            copies = [copy for copy in copies if copy.status in statuses]
        copies.sort(key=lambda copy: (copy.created, str(copy.id)))
        return copies

    def save_object_copy(self, copy: ObjectCopyRecord) -> None:
        with self._lock:
            copy.error = DomainErrorPayload.from_json_dict(copy.error)
            self._object_copies[copy.id] = copy

    def claim_object_copies(
        self, *, worker_id: str, limit: int, lease_seconds: int
    ) -> list[ObjectCopyRecord]:
        with self._lock:
            return _claim_worker_records(
                self._object_copies.values(),
                statuses={"pending", "started", "error"},
                worker_id=worker_id,
                limit=limit,
                lease_seconds=lease_seconds,
            )


def _claim_worker_records[WorkerRecordT: WorkerRecord](
    records: Iterable[WorkerRecordT],
    *,
    statuses: set[str],
    worker_id: str,
    limit: int,
    lease_seconds: int,
    unowned_only: bool = False,
) -> list[WorkerRecordT]:
    now = utc_now()
    lease_expires = now + timedelta(seconds=lease_seconds)
    claimed: list[WorkerRecordT] = []
    for record in sorted(records, key=lambda item: (item.created, str(item.id))):
        if len(claimed) >= limit:
            break
        if record.status not in statuses:
            continue
        if unowned_only and getattr(record, "delete_request_id", None) is not None:
            continue
        next_attempt_at = getattr(record, "next_attempt_at", None)
        if next_attempt_at is not None and next_attempt_at > now:
            continue
        if record.claim_expires_at is not None and record.claim_expires_at > now:
            continue
        record.status = "started"
        record.claimed_at = now
        record.claimed_by = worker_id
        record.claim_expires_at = lease_expires
        record.updated = now
        claimed.append(record)
    return claimed


def _normalize_webhook_error(webhook: WebhookRecord) -> None:
    if "error" not in webhook.data:
        return
    error = normalize_error_payload(webhook.data.get("error"))
    if error is None:
        webhook.data.pop("error", None)
    else:
        webhook.data["error"] = error


def _matches_segment_delete_filter(
    segment: SegmentRecord, delete_filter: SegmentDeleteFilter
) -> bool:
    if delete_filter.timerange_is_empty:
        return False
    if (
        delete_filter.object_id is not None
        and segment.object_id != delete_filter.object_id
    ):
        return False
    segment_start, segment_end = segment_domain.segment_bounds(segment)
    if (
        delete_filter.timerange_start is not None
        and segment_start < delete_filter.timerange_start
    ):
        return False
    return not (
        delete_filter.timerange_end is not None
        and segment_end > delete_filter.timerange_end
    )
