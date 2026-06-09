from __future__ import annotations

from dataclasses import dataclass
from typing import Any
from urllib.parse import urlparse
from uuid import UUID

from pydantic import ValidationError

from tamoss.application import webhooks as webhooking
from tamoss.application.contexts.flows import (
    ensure_flow_writable,
    query_timerange,
    require_flow,
)
from tamoss.application.contexts.object_get_urls import objects_get_urls
from tamoss.application.contexts.objects import (
    append_uncontrolled_instance,
    reserved_storage_labels,
    validate_uncontrolled_instance_append,
)
from tamoss.contract.generated import contract_models
from tamoss.domain.exceptions import SEGMENT_OVERLAP_MESSAGE, SegmentOverlapError
from tamoss.domain.model import FlowRecord, MediaObjectRecord, SegmentRecord, utc_now
from tamoss.domain.pagination import Page, page_sequence
from tamoss.domain.segments import (
    SegmentTimerangeBounds,
    object_timerange_from_segment_fields,
)
from tamoss.domain.timeranges import (
    finite_normalized_timerange_bounds,
    parse_timerange,
    parse_timestamp,
    timerange_union_strings,
)
from tamoss.errors import BadRequest
from tamoss.ports.object_storage import ObjectStorage
from tamoss.ports.repositories import (
    FlowLookupRepository,
    SegmentRepository,
    WebhookEventRepository,
)


@dataclass
class SegmentWriteResult:
    segment: SegmentRecord | None = None
    error: str | None = None


class SegmentUseCases:
    repository: SegmentRepository
    object_storage: ObjectStorage
    flow_repository: FlowLookupRepository
    webhook_repository: WebhookEventRepository

    def __init__(
        self,
        *,
        repository: SegmentRepository,
        object_storage: ObjectStorage,
        flow_repository: FlowLookupRepository,
        webhook_repository: WebhookEventRepository,
    ) -> None:
        self.repository = repository
        self.object_storage = object_storage
        self.flow_repository = flow_repository
        self.webhook_repository = webhook_repository

    def register_segment(
        self, *, flow_id: UUID, segment_post: dict[str, Any]
    ) -> SegmentWriteResult:
        return self.register_segments(flow_id=flow_id, segment_posts=[segment_post])[0]

    def register_segments(
        self, *, flow_id: UUID, segment_posts: list[dict[str, Any]]
    ) -> list[SegmentWriteResult]:
        try:
            with self.repository.unit_of_work():
                self.repository.lock_flow_segments(flow_id)
                return self._register_segments_locked(
                    flow_id=flow_id,
                    segment_posts=segment_posts,
                )
        except SegmentOverlapError:
            return [
                SegmentWriteResult(error=SEGMENT_OVERLAP_MESSAGE) for _ in segment_posts
            ]

    def _register_segments_locked(
        self, *, flow_id: UUID, segment_posts: list[dict[str, Any]]
    ) -> list[SegmentWriteResult]:
        flow = require_flow(self.flow_repository, flow_id)
        ensure_flow_writable(flow)
        if not flow.container:
            return [
                SegmentWriteResult(
                    error="Bad request. The Flow 'container' is not set."
                )
                for _ in segment_posts
            ]

        reserved_get_url_labels = reserved_storage_labels(self.repository)
        results = [SegmentWriteResult() for _ in segment_posts]
        candidates: list[tuple[int, dict[str, Any], SegmentTimerangeBounds]] = []
        for index, segment_post in enumerate(segment_posts):
            try:
                candidate_timerange = validate_segment_payload(
                    segment_post,
                    reserved_get_url_labels=reserved_get_url_labels,
                )
            except ValueError:
                results[index] = SegmentWriteResult(
                    error="Bad request. Invalid Flow Segment JSON."
                )
                continue
            candidates.append((index, segment_post, candidate_timerange))

        if not candidates:
            return results

        known_segments = self.repository.list_segments_overlapping(
            flow_id=flow_id,
            timeranges=(timerange for _, _, timerange in candidates),
        )
        media_objects_by_id = self.repository.get_objects(
            str(segment_post["object_id"]) for _, segment_post, _ in candidates
        )
        updated_media_objects: dict[str, MediaObjectRecord] = {}
        accepted_segments: list[SegmentRecord] = []

        for index, segment_post, _ in candidates:
            try:
                segment, media_object = self._prepare_segment_registration_or_raise(
                    flow=flow,
                    segment_post=segment_post,
                    known_segments=known_segments,
                    media_objects_by_id=media_objects_by_id,
                )
            except BadRequest as exc:
                results[index] = SegmentWriteResult(error=exc.detail)
                continue
            known_segments.append(segment)
            accepted_segments.append(segment)
            updated_media_objects[media_object.id] = media_object
            results[index] = SegmentWriteResult(segment=segment)

        if accepted_segments:
            flow.segments_updated = utc_now()
            self.repository.save_registered_segments(
                flow=flow,
                media_objects=updated_media_objects.values(),
                segments=accepted_segments,
            )
            webhooking.publish_flow_event(
                repository=self.webhook_repository,
                resource_repository=self.flow_repository,
                event_type="flows/updated",
                flow=flow,
            )
            webhooking.publish_segments_added(
                repository=self.webhook_repository,
                resource_repository=self.flow_repository,
                object_storage=self.object_storage,
                flow=flow,
                segments=accepted_segments,
                objects_by_id=media_objects_by_id,
            )

        return results

    def _prepare_segment_registration_or_raise(
        self,
        *,
        flow: FlowRecord,
        segment_post: dict[str, Any],
        known_segments: list[SegmentRecord],
        media_objects_by_id: dict[str, MediaObjectRecord],
    ) -> tuple[SegmentRecord, MediaObjectRecord]:
        self._ensure_segment_timerange_is_available(
            known_segments=known_segments,
            timerange=str(segment_post["timerange"]),
        )

        object_id = str(segment_post["object_id"])
        media_object = media_objects_by_id.get(object_id)
        existing_object_references = False
        if media_object is None:
            if not segment_post.get("get_urls"):
                raise BadRequest(
                    "Object must be allocated by this service or registered "
                    "with get_urls."
                )
            media_object = MediaObjectRecord(id=object_id)
            media_objects_by_id[media_object.id] = media_object
        else:
            existing_object_references = bool(media_object.referenced_by_flows)
            if (
                _has_controlled_instance(media_object)
                and not existing_object_references
            ):
                self._ensure_controlled_object_allocation_matches_flow(
                    media_object,
                    flow_id=flow.id,
                )
                self._ensure_controlled_object_uploaded(media_object)
            if existing_object_references:
                if segment_post.get("object_timerange") is not None:
                    raise BadRequest("Bad request. Invalid Flow Segment JSON.")
                if (
                    segment_post.get("key_frame_count") is not None
                    and segment_post.get("key_frame_count")
                    != media_object.key_frame_count
                ):
                    raise BadRequest("Bad request. Invalid Flow Segment JSON.")

        for get_url in segment_post.get("get_urls") or []:
            try:
                validate_uncontrolled_instance_append(
                    media_object,
                    url=str(get_url["url"]),
                    label=get_url.get("label"),
                    presigned=bool(get_url.get("presigned", False)),
                )
            except ValueError as exc:
                raise BadRequest("Bad request. Invalid Flow Segment JSON.") from exc

        if media_object.first_referenced_by_flow is None:
            media_object.first_referenced_by_flow = flow.id
        media_object.allocated_by_flow = None
        media_object.referenced_by_flows.add(flow.id)
        effective_object_timerange = object_timerange_from_segment_fields(
            timerange=str(segment_post["timerange"]),
            ts_offset=segment_post.get("ts_offset"),
            object_timerange=(
                media_object.timerange
                if existing_object_references
                else segment_post.get("object_timerange")
            ),
        )
        if not existing_object_references:
            media_object.timerange = timerange_union_strings(
                [
                    media_object.timerange,
                    effective_object_timerange,
                ]
            )
            media_object.key_frame_count = (
                segment_post.get("key_frame_count")
                if segment_post.get("key_frame_count") is not None
                else media_object.key_frame_count
            )

        for get_url in segment_post.get("get_urls") or []:
            append_uncontrolled_instance(
                media_object,
                url=str(get_url["url"]),
                label=get_url.get("label"),
                presigned=bool(get_url.get("presigned", False)),
            )

        segment = SegmentRecord(
            flow_id=flow.id,
            object_id=object_id,
            timerange=str(segment_post["timerange"]),
            ts_offset=segment_post.get("ts_offset"),
            last_duration=segment_post.get("last_duration"),
            object_timerange=(
                effective_object_timerange
                if effective_object_timerange != segment_post["timerange"]
                else None
            ),
            sample_offset=segment_post.get("sample_offset"),
            sample_count=segment_post.get("sample_count"),
            key_frame_count=segment_post.get("key_frame_count"),
        )
        return segment, media_object

    def _ensure_controlled_object_uploaded(
        self, media_object: MediaObjectRecord
    ) -> None:
        for instance in media_object.instances:
            if not instance.controlled or instance.storage_backend is None:
                continue
            metadata = self.object_storage.object_metadata(
                media_object.id,
                backend=instance.storage_backend,
            )
            if metadata is None:
                raise BadRequest("Bad request. Controlled object content is missing.")
            media_object.bytes_written = metadata.content_length or 0
            return
        raise BadRequest("Bad request. Invalid Flow Segment JSON.")

    def _ensure_controlled_object_allocation_matches_flow(
        self, media_object: MediaObjectRecord, *, flow_id: UUID
    ) -> None:
        if (
            media_object.allocated_by_flow is not None
            and media_object.allocated_by_flow != flow_id
        ):
            raise BadRequest(
                "Bad request. Allocated Object must be registered against the "
                "Flow that requested its storage."
            )

    def _ensure_segment_timerange_is_available(
        self, *, known_segments: list[SegmentRecord], timerange: str
    ) -> None:
        candidate = parse_timerange(
            timerange,
            field_name="timerange",
            finite=True,
        )
        for segment in known_segments:
            existing = parse_timerange(
                segment.timerange,
                field_name="timerange",
                finite=True,
            )
            if not candidate.intersect_with(existing).is_empty():
                raise BadRequest(SEGMENT_OVERLAP_MESSAGE)

    def list_segments(
        self,
        *,
        flow_id: UUID,
        object_id: str | None,
        timerange: str | None,
        reverse_order: bool,
        page: str | None,
        limit: int | None,
    ) -> Page[SegmentRecord]:
        if self.repository.get_flow(flow_id) is None:
            return page_sequence([], page=page, limit=limit)
        requested_timerange = query_timerange(timerange)
        return self.repository.list_segments_page(
            flow_id=flow_id,
            object_id=object_id,
            timerange_start=requested_timerange.start,
            timerange_end=requested_timerange.end,
            timerange_is_empty=requested_timerange.is_empty,
            timerange_is_point=requested_timerange.is_point,
            reverse_order=reverse_order,
            page=page,
            limit=limit,
        )

    def segment_get_urls(
        self,
        segments: list[SegmentRecord],
        *,
        accept_get_urls: set[str] | None = None,
        accept_storage_ids: set[str] | None = None,
        presigned: bool | None = None,
        verbose_storage: bool = False,
    ) -> dict[str, list[dict[str, Any]]]:
        objects_by_id = self.repository.get_objects(
            segment.object_id for segment in segments
        )
        return objects_get_urls(
            objects_by_id.values(),
            object_storage=self.object_storage,
            accept_get_urls=accept_get_urls,
            accept_storage_ids=accept_storage_ids,
            presigned=presigned,
            verbose_storage=verbose_storage,
        )


def _has_controlled_instance(media_object: MediaObjectRecord) -> bool:
    return any(instance.controlled for instance in media_object.instances)


def validate_segment_payload(
    payload: dict[str, Any], *, reserved_get_url_labels: set[str] | None = None
) -> SegmentTimerangeBounds:
    try:
        segment = contract_models.FlowSegmentPost.model_validate(payload)
    except ValidationError as exc:
        raise ValueError(
            "segment payload does not match the BBC TAMS contract"
        ) from exc

    parsed = parse_timerange(
        segment.timerange.root,
        field_name="timerange",
        finite=True,
    )
    bounds = finite_normalized_timerange_bounds(parsed)
    if not parsed.includes_start():
        raise ValueError("timerange start must be inclusive")

    if segment.ts_offset is not None:
        parse_timestamp(segment.ts_offset.root, field_name="ts_offset")
    if segment.last_duration is not None:
        last_duration = parse_timestamp(
            segment.last_duration.root, field_name="last_duration"
        )
        if int(last_duration.to_nanosec()) < 0:
            raise ValueError("last_duration must not be negative")
        if parsed.includes_end():
            raise ValueError(
                "timerange end must be exclusive when last_duration is set"
            )
    if segment.object_timerange is not None:
        object_timerange = parse_timerange(
            segment.object_timerange.root,
            field_name="object_timerange",
            finite=True,
        )
        finite_normalized_timerange_bounds(object_timerange)

    _validate_optional_nonnegative_int(payload, "sample_offset")
    _validate_optional_nonnegative_int(payload, "sample_count")
    _validate_optional_nonnegative_int(payload, "key_frame_count")

    if segment.get_urls is not None:
        _validate_get_urls(
            segment.get_urls,
            reserved_labels=reserved_get_url_labels or set(),
        )

    assert bounds.start is not None
    assert bounds.end is not None
    return SegmentTimerangeBounds(
        start=bounds.start,
        end=bounds.end,
        is_point=bounds.is_point,
    )


def _validate_get_urls(
    entries: list[contract_models.GetUrl1], *, reserved_labels: set[str]
) -> None:
    if not entries:
        raise ValueError("get_urls must be a non-empty array")
    labels: set[str] = set()
    for entry in entries:
        label = entry.label
        if not label:
            raise ValueError("get_urls entries require a label")
        if label in labels or label in reserved_labels:
            raise ValueError("get_urls labels must be unique and uncontrolled")
        labels.add(label)
        parsed = urlparse(entry.url)
        if parsed.scheme.lower() not in {"http", "https"} or not parsed.netloc:
            raise ValueError("get_urls entries require an HTTP URL")


def _validate_optional_nonnegative_int(
    payload: dict[str, Any], field_name: str
) -> None:
    value = payload.get(field_name)
    if value is None:
        return
    if not isinstance(value, int) or isinstance(value, bool):
        raise ValueError(f"{field_name} must be an integer")
    if value < 0:
        raise ValueError(f"{field_name} must not be negative")
