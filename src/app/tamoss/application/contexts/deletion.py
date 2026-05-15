from __future__ import annotations

from tamoss.application.contexts._shared import (
    DEFAULT_WORKER_ID,
    DEFAULT_WORKER_LEASE_SECONDS,
    UUID,
    DeletionRequestRecord,
    Identity,
    NotFound,
    SegmentRecord,
    UseCaseContext,
    _clear_worker_claim,
    _requested_delete_timerange,
    _segments_matching_delete_filters,
    _timerange_covering_segments,
    error_payload,
    utc_now,
    uuid4,
)


class DeletionUseCases(UseCaseContext):
    def list_delete_requests(self) -> list[DeletionRequestRecord]:
        requests = self.repository.list_delete_requests()
        requests.sort(key=lambda request: str(request.id))
        return requests

    def get_delete_request(self, request_id: UUID) -> DeletionRequestRecord:
        request = self.repository.get_delete_request(request_id)
        if request is None:
            raise NotFound("The requested flow delete request does not exist.")
        return request

    def delete_flow(
        self, *, flow_id: UUID, identity: Identity
    ) -> DeletionRequestRecord | None:
        flow = self.get_flow(flow_id)
        self._ensure_flow_writable(flow)
        segments = self.repository.list_segments(flow_id)
        if not segments:
            with self.repository.unit_of_work():
                self._unlink_flow_collection_references(flow)
                self.repository.delete_flow(flow_id)
                self._publish_flow_deleted(flow)
                self._delete_orphan_source(flow.source_id)
            return None

        timerange_to_delete = _timerange_covering_segments(segments)
        return self._record_deletion_request(
            flow_id=flow_id,
            timerange_to_delete=timerange_to_delete,
            delete_flow=True,
            segments_to_delete=segments,
            identity=identity,
        )

    def delete_segments(
        self,
        *,
        flow_id: UUID,
        timerange: str | None,
        object_id: str | None,
        identity: Identity,
    ) -> DeletionRequestRecord | None:
        flow = self.get_flow(flow_id)
        self._ensure_flow_writable(flow)
        segments = self.repository.list_segments(flow_id)
        segments_to_delete = _segments_matching_delete_filters(
            segments, timerange=timerange, object_id=object_id
        )
        if not segments_to_delete:
            return None
        if object_id is not None:
            with self.repository.unit_of_work():
                self._delete_segments(
                    flow_id=flow_id, segments_to_delete=segments_to_delete
                )
                flow.segments_updated = utc_now()
                self.repository.save_flow(flow)
            return None

        timerange_to_delete = _requested_delete_timerange(
            segments_to_delete, requested_timerange=timerange
        )
        return self._record_deletion_request(
            flow_id=flow_id,
            timerange_to_delete=timerange_to_delete,
            delete_flow=False,
            segments_to_delete=segments_to_delete,
            identity=identity,
        )

    def _record_deletion_request(
        self,
        *,
        flow_id: UUID,
        timerange_to_delete: str,
        delete_flow: bool,
        segments_to_delete: list[SegmentRecord],
        identity: Identity,
    ) -> DeletionRequestRecord:
        now = utc_now()
        request = DeletionRequestRecord(
            id=uuid4(),
            flow_id=flow_id,
            timerange_to_delete=timerange_to_delete,
            timerange_remaining=timerange_to_delete,
            delete_flow=delete_flow,
            created=now,
            updated=now,
            created_by=identity.subject,
            status="created",
            segments_to_delete=list(segments_to_delete),
        )
        self.repository.save_delete_request(request)
        return request

    def process_pending_delete_requests(
        self,
        *,
        max_requests: int = 50,
        worker_id: str = DEFAULT_WORKER_ID,
        lease_seconds: int = DEFAULT_WORKER_LEASE_SECONDS,
    ) -> int:
        processed = 0
        requests = self.repository.claim_delete_requests(
            worker_id=worker_id,
            limit=max_requests,
            lease_seconds=lease_seconds,
        )
        for request in requests:
            self.process_delete_request(request.id)
            processed += 1
        return processed

    def process_delete_request(self, request_id: UUID) -> DeletionRequestRecord | None:
        request = self.repository.get_delete_request(request_id)
        if request is None:
            return None
        if request.status not in {"created", "started", "error"}:
            return request

        try:
            with self.repository.unit_of_work():
                request = self.repository.get_delete_request(request_id)
                if request is None:
                    return None
                if request.status not in {"created", "started", "error"}:
                    return request

                request.status = "started"
                request.error = None
                request.updated = utc_now()
                self.repository.save_delete_request(request)
                if request.delete_flow:
                    self._process_flow_delete_request(request)
                else:
                    self._process_segment_delete_request(request)
                request.timerange_remaining = None
                request.status = "done"
                request.error = None
                _clear_worker_claim(request)
                request.updated = utc_now()
                self.repository.save_delete_request(request)
        except Exception as exc:
            request = self.repository.get_delete_request(request_id)
            if request is None:
                return None
            request.status = "error"
            request.error = error_payload(type(exc).__name__, str(exc))
            _clear_worker_claim(request)
            request.updated = utc_now()
            self.repository.save_delete_request(request)
        return request

    def _process_flow_delete_request(self, request: DeletionRequestRecord) -> None:
        flow = self.repository.get_flow(request.flow_id)
        if flow is None:
            return
        segments = request.segments_to_delete or self.repository.list_segments(
            request.flow_id
        )
        self._unlink_flow_collection_references(flow)
        self._delete_segments(
            flow_id=request.flow_id,
            segments_to_delete=segments,
            publish_event=False,
        )
        self.repository.delete_flow(request.flow_id)
        self._publish_flow_deleted(flow)
        self._delete_orphan_source(flow.source_id)

    def _process_segment_delete_request(self, request: DeletionRequestRecord) -> None:
        flow = self.repository.get_flow(request.flow_id)
        if flow is None:
            return
        segments_to_delete = request.segments_to_delete
        if not segments_to_delete:
            segments = self.repository.list_segments(request.flow_id)
            segments_to_delete = _segments_matching_delete_filters(
                segments,
                timerange=request.timerange_to_delete,
                object_id=None,
            )
        if segments_to_delete:
            self._delete_segments(
                flow_id=request.flow_id, segments_to_delete=segments_to_delete
            )
            flow.segments_updated = utc_now()
            self.repository.save_flow(flow)

    def _delete_segments(
        self,
        *,
        flow_id: UUID,
        segments_to_delete: list[SegmentRecord],
        publish_event: bool = True,
    ) -> None:
        if not segments_to_delete:
            return
        flow = self.repository.get_flow(flow_id)
        deleted_segment_ids = {
            (segment.object_id, segment.timerange) for segment in segments_to_delete
        }
        remaining = [
            segment
            for segment in self.repository.list_segments(flow_id)
            if (segment.object_id, segment.timerange) not in deleted_segment_ids
        ]
        self.repository.replace_segments(flow_id, remaining)
        self._refresh_deleted_object_references(flow_id, segments_to_delete)
        if publish_event and flow is not None:
            self._publish_segments_deleted(flow, segments_to_delete)

    def _refresh_deleted_object_references(
        self, flow_id: UUID, deleted_segments: list[SegmentRecord]
    ) -> None:
        for object_id in {segment.object_id for segment in deleted_segments}:
            media_object = self.repository.get_object(object_id)
            if media_object is None:
                continue
            if not any(
                segment.object_id == object_id
                for segment in self.repository.list_segments(flow_id)
            ):
                media_object.referenced_by_flows.discard(flow_id)
            if media_object.referenced_by_flows:
                media_object.timerange = self._object_timerange(media_object)
                self.repository.save_object(media_object)
            else:
                self._delete_controlled_object_content(media_object)
                self.repository.delete_object(object_id)

    def _delete_orphan_source(self, source_id: UUID | None) -> None:
        if source_id is None:
            return
        if any(flow.source_id == source_id for flow in self.repository.list_flows()):
            return
        source = self.repository.get_source(source_id)
        self.repository.delete_source(source_id)
        if source is not None:
            self._publish_source_deleted(source)
