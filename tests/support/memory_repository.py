"""Test-only repository fake for fast API and application tests."""

from __future__ import annotations

from collections.abc import Iterable, Iterator
from contextlib import contextmanager
from copy import deepcopy
from dataclasses import replace
from datetime import timedelta
from threading import RLock
from uuid import UUID

from mediatimestamp import TimeRange
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
    utc_now,
)
from tamoss.domain.pagination import Page, page_sequence
from tamoss.domain.tags import tags_match
from tamoss.errors import normalize_error_payload
from tamoss.ports.repositories import SegmentTimerangeBounds


class InMemoryRepository:
    def __init__(self, storage_backend: StorageBackend):
        self._lock = RLock()
        self._storage_backend = storage_backend
        self._flows: dict[UUID, FlowRecord] = {}
        self._sources: dict[UUID, SourceRecord] = {}
        self._objects: dict[str, MediaObjectRecord] = {}
        self._segments: dict[UUID, list[SegmentRecord]] = {}
        self._webhooks: dict[UUID, WebhookRecord] = {}
        self._webhook_deliveries: dict[UUID, WebhookDeliveryRecord] = {}
        self._delete_requests: dict[UUID, DeletionRequestRecord] = {}
        self._service_metadata: ServiceMetadata | None = None

    @contextmanager
    def unit_of_work(self) -> Iterator[InMemoryRepository]:
        with self._lock:
            snapshot = (
                deepcopy(self._flows),
                deepcopy(self._sources),
                deepcopy(self._objects),
                deepcopy(self._segments),
                deepcopy(self._webhooks),
                deepcopy(self._webhook_deliveries),
                deepcopy(self._delete_requests),
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

    def list_storage_backends(self) -> list[StorageBackend]:
        with self._lock:
            return [self._storage_backend]

    def default_storage_backend(self) -> StorageBackend | None:
        with self._lock:
            return self._storage_backend

    def get_storage_backend(self, storage_id: UUID) -> StorageBackend | None:
        with self._lock:
            if storage_id == self._storage_backend.id:
                return self._storage_backend
            return None

    def list_flows(self) -> list[FlowRecord]:
        with self._lock:
            return list(self._flows.values())

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
        collected_by = _collected_by_by_flow_id(flows)
        flows = [
            _flow_with_collected_by(flow, collected_by.get(flow.id, []))
            for flow in flows
        ]
        if source_id is not None:
            flows = [flow for flow in flows if flow.source_id == source_id]
        if format is not None:
            flows = [flow for flow in flows if flow.format == format]
        if codec is not None:
            flows = [flow for flow in flows if _flow_data_value(flow, "codec") == codec]
        if label is not None:
            flows = [flow for flow in flows if flow.data.get("label") == label]
        if frame_width is not None:
            flows = [
                flow
                for flow in flows
                if _flow_data_value(flow, "frame_width") == frame_width
            ]
        if frame_height is not None:
            flows = [
                flow
                for flow in flows
                if _flow_data_value(flow, "frame_height") == frame_height
            ]
        if timerange_is_empty:
            flows = [flow for flow in flows if not segments.get(flow.id)]
        elif timerange_start is not None or timerange_end is not None:
            flows = [
                flow
                for flow in flows
                if _flow_timerange_matches(
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
            flow_id: _timerange_union(segments[flow_id]) for flow_id in requested_ids
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
            for item in _flow_collection(parent_flow):
                child_flow_id = _collection_child_id(item)
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

    def delete_object(self, object_id: str) -> None:
        with self._lock:
            self._objects.pop(object_id, None)

    def list_segments(self, flow_id: UUID) -> list[SegmentRecord]:
        with self._lock:
            return list(self._segments.get(flow_id, []))

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
                _segment_overlaps_bounds(
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
        matching.sort(key=_segment_sort_key)
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
                if _segment_overlaps_bounds(
                    segment,
                    start=timerange_start,
                    end=timerange_end,
                    requested_is_point=timerange_is_point,
                )
            ]

        segments.sort(key=_segment_sort_key, reverse=reverse_order)
        matched_timerange = _timerange_union(segments)
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
                segment_start, segment_end = _segment_bounds(segment)
                if any(
                    _segment_overlaps_bounds(
                        existing,
                        start=segment_start,
                        end=segment_end,
                        requested_is_point=segment_start == segment_end,
                    )
                    for existing in known_segments
                ):
                    raise ValueError(
                        "Segment timerange overlaps with an existing segment"
                    )
                known_segments.append(segment)
            self._flows[flow.id] = flow
            for media_object in media_objects:
                self._objects[media_object.id] = media_object
            for segment in pending_segments:
                self._segments.setdefault(segment.flow_id, []).append(segment)

    def replace_segments(self, flow_id: UUID, segments: list[SegmentRecord]) -> None:
        with self._lock:
            if segments:
                self._segments[flow_id] = segments
            else:
                self._segments.pop(flow_id, None)

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
            delivery.error = normalize_error_payload(delivery.error)
            self._webhook_deliveries[delivery.id] = delivery

    def claim_webhook_deliveries(
        self, *, worker_id: str, limit: int, lease_seconds: int
    ) -> list[WebhookDeliveryRecord]:
        now = utc_now()
        lease_expires = now + timedelta(seconds=lease_seconds)
        claimed: list[WebhookDeliveryRecord] = []
        with self._lock:
            deliveries = sorted(
                self._webhook_deliveries.values(),
                key=lambda delivery: (delivery.created, str(delivery.id)),
            )
            for delivery in deliveries:
                if len(claimed) >= limit:
                    break
                if delivery.status not in {"pending", "started"}:
                    continue
                if (
                    delivery.next_attempt_at is not None
                    and delivery.next_attempt_at > now
                ):
                    continue
                if (
                    delivery.claim_expires_at is not None
                    and delivery.claim_expires_at > now
                ):
                    continue
                delivery.status = "started"
                delivery.claimed_at = now
                delivery.claimed_by = worker_id
                delivery.claim_expires_at = lease_expires
                delivery.updated = now
                claimed.append(delivery)
        return claimed

    def list_delete_requests(self) -> list[DeletionRequestRecord]:
        with self._lock:
            return list(self._delete_requests.values())

    def get_delete_request(self, request_id: UUID) -> DeletionRequestRecord | None:
        with self._lock:
            return self._delete_requests.get(request_id)

    def save_delete_request(self, request: DeletionRequestRecord) -> None:
        with self._lock:
            request.error = normalize_error_payload(request.error)
            self._delete_requests[request.id] = request

    def claim_delete_requests(
        self, *, worker_id: str, limit: int, lease_seconds: int
    ) -> list[DeletionRequestRecord]:
        now = utc_now()
        lease_expires = now + timedelta(seconds=lease_seconds)
        claimed: list[DeletionRequestRecord] = []
        with self._lock:
            requests = sorted(
                self._delete_requests.values(),
                key=lambda request: (request.created, str(request.id)),
            )
            for request in requests:
                if len(claimed) >= limit:
                    break
                if request.status not in {"created", "started", "error"}:
                    continue
                if (
                    request.claim_expires_at is not None
                    and request.claim_expires_at > now
                ):
                    continue
                request.status = "started"
                request.claimed_at = now
                request.claimed_by = worker_id
                request.claim_expires_at = lease_expires
                request.updated = now
                claimed.append(request)
        return claimed


def _flow_data_value(flow: FlowRecord, name: str) -> object:
    if name in flow.data:
        return flow.data[name]
    essence_parameters = flow.data.get("essence_parameters")
    if isinstance(essence_parameters, dict):
        return essence_parameters.get(name)
    return None


def _flow_collection(flow: FlowRecord) -> list[dict]:
    collection = flow.data.get("flow_collection")
    if not isinstance(collection, list):
        return []
    return [dict(item) for item in collection if isinstance(item, dict)]


def _collection_child_id(item: dict) -> UUID | None:
    raw_id = item.get("id")
    if raw_id is None:
        return None
    try:
        return UUID(str(raw_id))
    except ValueError:
        return None


def _collected_by_by_flow_id(flows: list[FlowRecord]) -> dict[UUID, list[str]]:
    collected_by: dict[UUID, list[str]] = {}
    for parent in flows:
        parent_id = str(parent.id)
        for item in _flow_collection(parent):
            child_id = _collection_child_id(item)
            if child_id is None:
                continue
            parent_ids = collected_by.setdefault(child_id, [])
            if parent_id not in parent_ids:
                parent_ids.append(parent_id)
    return collected_by


def _flow_with_collected_by(flow: FlowRecord, collected_by: list[str]) -> FlowRecord:
    data = dict(flow.data)
    if collected_by:
        data["collected_by"] = collected_by
    else:
        data.pop("collected_by", None)
    return replace(flow, data=data)


def _flow_timerange_matches(
    segments: list[SegmentRecord],
    *,
    start: int | None,
    end: int | None,
    requested_is_point: bool,
) -> bool:
    if not segments:
        return False
    starts: list[int] = []
    ends: list[int] = []
    for segment in segments:
        segment_start, segment_end = _segment_bounds(segment)
        starts.append(segment_start)
        ends.append(segment_end)
    flow_start = min(starts)
    flow_end = max(ends)
    flow_is_point = flow_start == flow_end

    if requested_is_point and start is not None:
        if flow_is_point:
            return flow_start == start
        return flow_start <= start < flow_end
    if end is not None and flow_start >= end:
        return False
    if start is not None:
        if flow_is_point:
            return flow_start >= start
        return flow_end > start
    return True


def _segment_bounds(segment: SegmentRecord) -> tuple[int, int]:
    parsed = TimeRange.from_str(segment.timerange)
    if parsed.start is None or parsed.end is None:
        raise ValueError("Segment timerange must have finite start and end bounds.")
    return int(parsed.start.to_nanosec()), int(parsed.end.to_nanosec())


def _segment_overlaps_bounds(
    segment: SegmentRecord,
    *,
    start: int | None,
    end: int | None,
    requested_is_point: bool,
) -> bool:
    segment_start, segment_end = _segment_bounds(segment)
    segment_is_point = segment_start == segment_end

    if requested_is_point and start is not None:
        if segment_is_point:
            return segment_start == start
        return segment_start <= start < segment_end

    if end is not None and segment_start >= end:
        return False
    if start is not None:
        if segment_is_point:
            return segment_start >= start
        if segment_end <= start:
            return False
    return True


def _segment_sort_key(segment: SegmentRecord) -> tuple[int, int, str]:
    start, end = _segment_bounds(segment)
    return end, start, segment.object_id


def _timerange_union(segments: list[SegmentRecord]) -> str:
    ranges: list[TimeRange] = []
    for segment in segments:
        parsed = TimeRange.from_str(segment.timerange)
        if not parsed.is_empty():
            ranges.append(parsed)
    if not ranges:
        return "()"
    merged = ranges[0]
    for parsed in ranges[1:]:
        merged = merged.extend_to_encompass_timerange(parsed)
    return str(merged)


def _normalize_webhook_error(webhook: WebhookRecord) -> None:
    if "error" not in webhook.data:
        return
    error = normalize_error_payload(webhook.data.get("error"))
    if error is None:
        webhook.data.pop("error", None)
    else:
        webhook.data["error"] = error
