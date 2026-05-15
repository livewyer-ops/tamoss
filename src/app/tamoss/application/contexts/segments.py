from __future__ import annotations

from tamoss.application.contexts._shared import (
    UUID,
    Any,
    BadRequest,
    FlowRecord,
    FlowSegmentPost,
    Forbidden,
    MediaObjectRecord,
    Page,
    SegmentRecord,
    SegmentRegistrationCandidate,
    SegmentWriteResult,
    UseCaseContext,
    _append_uncontrolled_instance,
    _optional_string_set,
    _query_timerange,
    _segment_timerange_bounds,
    _union_timerange_strings,
    _validate_uncontrolled_instance_append,
    page_sequence,
    utc_now,
    validation,
)


class SegmentUseCases(UseCaseContext):
    def register_segment(
        self, *, flow_id: UUID, segment_post: FlowSegmentPost
    ) -> SegmentWriteResult:
        return self.register_segments(flow_id=flow_id, segment_posts=[segment_post])[0]

    def register_segments(
        self, *, flow_id: UUID, segment_posts: list[FlowSegmentPost]
    ) -> list[SegmentWriteResult]:
        try:
            with self.repository.unit_of_work():
                self.repository.lock_flow_segments(flow_id)
                return self._register_segments_locked(
                    flow_id=flow_id,
                    segment_posts=segment_posts,
                )
        except ValueError as exc:
            if str(exc) == "Segment timerange overlaps with an existing segment":
                return [SegmentWriteResult(error=str(exc)) for _ in segment_posts]
            raise

    def _register_segments_locked(
        self, *, flow_id: UUID, segment_posts: list[FlowSegmentPost]
    ) -> list[SegmentWriteResult]:
        flow = self.get_flow(flow_id)
        if flow.read_only:
            raise Forbidden(
                "Forbidden. You do not have permission to modify this Flow. "
                "It may be marked read-only."
            )
        if not flow.container:
            return [
                SegmentWriteResult(
                    error="Bad request. The Flow 'container' is not set."
                )
                for _ in segment_posts
            ]

        reserved_get_url_labels = {
            backend.label
            for backend in self.repository.list_storage_backends()
            if backend.label
        }
        results = [SegmentWriteResult() for _ in segment_posts]
        candidates: list[SegmentRegistrationCandidate] = []
        for index, segment_post in enumerate(segment_posts):
            try:
                candidate_timerange = _segment_timerange_bounds(segment_post.timerange)
                validation.validate_segment_payload(
                    segment_post.model_dump(exclude_none=True, mode="json"),
                    reserved_get_url_labels=reserved_get_url_labels,
                )
            except ValueError:
                results[index] = SegmentWriteResult(
                    error="Bad request. Invalid Flow Segment JSON."
                )
                continue
            candidates.append(
                SegmentRegistrationCandidate(
                    index=index,
                    segment_post=segment_post,
                    timerange=candidate_timerange,
                )
            )

        if not candidates:
            return results

        known_segments = self.repository.list_segments_overlapping(
            flow_id=flow_id,
            timeranges=(candidate.timerange for candidate in candidates),
        )
        media_objects_by_id = self.repository.get_objects(
            candidate.segment_post.object_id for candidate in candidates
        )
        updated_media_objects: dict[str, MediaObjectRecord] = {}
        accepted_segments: list[SegmentRecord] = []

        for candidate in candidates:
            try:
                segment, media_object = self._prepare_segment_registration_or_raise(
                    flow=flow,
                    segment_post=candidate.segment_post,
                    known_segments=known_segments,
                    media_objects_by_id=media_objects_by_id,
                )
            except BadRequest as exc:
                results[candidate.index] = SegmentWriteResult(error=exc.detail)
                continue
            known_segments.append(segment)
            accepted_segments.append(segment)
            updated_media_objects[media_object.id] = media_object
            results[candidate.index] = SegmentWriteResult(segment=segment)

        if accepted_segments:
            flow.segments_updated = utc_now()
            self.repository.save_registered_segments(
                flow=flow,
                media_objects=updated_media_objects.values(),
                segments=accepted_segments,
            )
            self._publish_segments_added(flow, accepted_segments)

        return results

    def _prepare_segment_registration_or_raise(
        self,
        *,
        flow: FlowRecord,
        segment_post: FlowSegmentPost,
        known_segments: list[SegmentRecord],
        media_objects_by_id: dict[str, MediaObjectRecord],
    ) -> tuple[SegmentRecord, MediaObjectRecord]:
        self._ensure_segment_timerange_is_available(
            known_segments=known_segments,
            timerange=segment_post.timerange,
        )

        media_object = media_objects_by_id.get(segment_post.object_id)
        existing_object_references = False
        if media_object is None:
            if not segment_post.get_urls:
                raise BadRequest(
                    "Object must be allocated by this service or registered "
                    "with get_urls."
                )
            media_object = MediaObjectRecord(id=segment_post.object_id)
            media_objects_by_id[media_object.id] = media_object
        else:
            existing_object_references = bool(media_object.referenced_by_flows)
            if existing_object_references:
                if segment_post.object_timerange is not None:
                    raise BadRequest("Bad request. Invalid Flow Segment JSON.")
                if (
                    segment_post.key_frame_count is not None
                    and segment_post.key_frame_count != media_object.key_frame_count
                ):
                    raise BadRequest("Bad request. Invalid Flow Segment JSON.")

        for get_url in segment_post.get_urls or []:
            try:
                _validate_uncontrolled_instance_append(
                    media_object,
                    url=str(get_url["url"]),
                    label=get_url.get("label"),
                    presigned=bool(get_url.get("presigned", False)),
                )
            except ValueError as exc:
                raise BadRequest("Bad request. Invalid Flow Segment JSON.") from exc

        if media_object.first_referenced_by_flow is None:
            media_object.first_referenced_by_flow = flow.id
        media_object.referenced_by_flows.add(flow.id)
        if not existing_object_references:
            media_object.timerange = _union_timerange_strings(
                [
                    media_object.timerange,
                    segment_post.object_timerange or segment_post.timerange,
                ]
            )
            media_object.key_frame_count = (
                segment_post.key_frame_count
                if segment_post.key_frame_count is not None
                else media_object.key_frame_count
            )

        for get_url in segment_post.get_urls or []:
            _append_uncontrolled_instance(
                media_object,
                url=str(get_url["url"]),
                label=get_url.get("label"),
                presigned=bool(get_url.get("presigned", False)),
            )

        segment = SegmentRecord(
            flow_id=flow.id,
            object_id=segment_post.object_id,
            timerange=segment_post.timerange,
            ts_offset=segment_post.ts_offset,
            last_duration=segment_post.last_duration,
            object_timerange=segment_post.object_timerange,
            sample_offset=segment_post.sample_offset,
            sample_count=segment_post.sample_count,
            get_urls=list(segment_post.get_urls or []),
            key_frame_count=segment_post.key_frame_count,
        )
        return segment, media_object

    def _ensure_segment_timerange_is_available(
        self, *, known_segments: list[SegmentRecord], timerange: str
    ) -> None:
        candidate = validation.parse_timerange(
            timerange,
            field_name="timerange",
            finite=True,
        )
        for segment in known_segments:
            existing = validation.parse_timerange(
                segment.timerange,
                field_name="timerange",
                finite=True,
            )
            if not candidate.intersect_with(existing).is_empty():
                raise BadRequest("Segment timerange overlaps with an existing segment")

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
        query_timerange = _query_timerange(timerange)
        return self.repository.list_segments_page(
            flow_id=flow_id,
            object_id=object_id,
            timerange_start=query_timerange.start,
            timerange_end=query_timerange.end,
            timerange_is_empty=query_timerange.is_empty,
            timerange_is_point=query_timerange.is_point,
            reverse_order=reverse_order,
            page=page,
            limit=limit,
        )

    def _segment_payload(
        self, segment: SegmentRecord, webhook_data: dict[str, Any]
    ) -> dict[str, Any]:
        media_object = self.repository.get_object(segment.object_id)
        accepted_labels = _optional_string_set(webhook_data.get("accept_get_urls"))
        accepted_storage_ids = _optional_string_set(
            webhook_data.get("accept_storage_ids")
        )
        presigned = webhook_data.get("presigned")
        verbose_storage = bool(webhook_data.get("verbose_storage"))
        get_urls = (
            self.object_get_urls(
                media_object,
                accept_get_urls=accepted_labels,
                accept_storage_ids=accepted_storage_ids,
                presigned=presigned,
                verbose_storage=verbose_storage,
            )
            if media_object is not None
            else []
        )
        payload = {
            "object_id": segment.object_id,
            "timerange": segment.timerange,
            "ts_offset": segment.ts_offset,
            "last_duration": segment.last_duration,
            "object_timerange": segment.object_timerange,
            "sample_offset": segment.sample_offset,
            "sample_count": segment.sample_count,
            "get_urls": get_urls,
            "key_frame_count": segment.key_frame_count,
        }
        return {key: value for key, value in payload.items() if value is not None}
