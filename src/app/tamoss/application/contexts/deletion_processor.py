from __future__ import annotations

import logging
from collections.abc import Iterable
from uuid import UUID, uuid4

from tamoss.application import webhooks as webhooking
from tamoss.application.contexts.flows import unlink_flow_collection_references
from tamoss.domain.model import (
    DeletionRequestRecord,
    DomainErrorPayload,
    MediaObjectRecord,
    ObjectCleanupRecord,
    SegmentRecord,
    StorageBackend,
    utc_now,
)
from tamoss.domain.segments import (
    SegmentDeleteFilter,
    segment_delete_filter,
    segment_object_timerange,
)
from tamoss.domain.timeranges import timerange_union_strings
from tamoss.ports.object_storage import ObjectStorage
from tamoss.ports.repositories import DeletionRepository, WebhookEventRepository

logger = logging.getLogger(__name__)

DELETE_REQUEST_FAILED = "delete_request_failed"
DELETE_CLEANUP_FAILED = "object_cleanup_failed"
DELETE_SEGMENT_BATCH_SIZE = 1000
ACTIVE_CLEANUP_STATUSES = {"pending", "started", "error"}


class ObjectCleanupFailed(RuntimeError):
    pass


def process_delete_request(
    *,
    repository: DeletionRepository,
    webhook_repository: WebhookEventRepository,
    object_storage: ObjectStorage,
    request_id: UUID,
) -> DeletionRequestRecord | None:
    request = repository.get_delete_request(request_id)
    if request is None:
        return None
    if request.status not in {"created", "started", "error"}:
        return request

    try:
        request = _apply_delete_metadata(
            repository=repository,
            webhook_repository=webhook_repository,
            request_id=request_id,
        )
        if request is None:
            return None
        process_object_cleanups(
            repository=repository,
            object_storage=object_storage,
            cleanups=repository.list_object_cleanups(
                delete_request_id=request.id,
                statuses=ACTIVE_CLEANUP_STATUSES,
            ),
        )
        if request.timerange_remaining is not None:
            _release_delete_request_claim(repository, request.id)
            return repository.get_delete_request(request_id)
        _mark_delete_request_done(repository, request.id)
    except ObjectCleanupFailed:
        _mark_delete_request_error(
            repository,
            request_id,
            error_type=DELETE_CLEANUP_FAILED,
            summary="Object storage cleanup failed; retry will continue.",
        )
    except Exception:
        logger.exception(
            "delete request processing failed",
            extra={"delete_request_id": str(request_id)},
        )
        _mark_delete_request_error(
            repository,
            request_id,
            error_type=DELETE_REQUEST_FAILED,
            summary="Delete request processing failed; retry will continue.",
        )
    return repository.get_delete_request(request_id)


def delete_matching_segments(
    *,
    repository: DeletionRepository,
    webhook_repository: WebhookEventRepository,
    delete_filter: SegmentDeleteFilter,
    delete_request_id: UUID | None = None,
    publish_event: bool = True,
    drain: bool = False,
) -> str:
    flow = repository.get_flow(delete_filter.flow_id)
    while True:
        deleted_segments = repository.delete_segment_batch(
            delete_filter,
            limit=DELETE_SEGMENT_BATCH_SIZE,
        )
        if not deleted_segments:
            return repository.segment_delete_timerange(delete_filter)
        _refresh_deleted_object_references(
            repository=repository,
            flow_id=delete_filter.flow_id,
            deleted_segments=deleted_segments,
            delete_request_id=delete_request_id,
        )
        if publish_event and flow is not None:
            webhooking.publish_segments_deleted(
                repository=webhook_repository,
                resource_repository=repository,
                flow=flow,
                segments=deleted_segments,
            )
        remaining_timerange = repository.segment_delete_timerange(delete_filter)
        if remaining_timerange == "()" or not drain:
            return remaining_timerange


def delete_orphan_source(
    *,
    repository: DeletionRepository,
    webhook_repository: WebhookEventRepository,
    source_id: UUID | None,
) -> None:
    if source_id is None:
        return
    if repository.list_flows_by_source(source_id):
        return
    source = repository.get_source(source_id)
    repository.delete_source(source_id)
    if source is not None:
        webhooking.publish_source_deleted(
            repository=webhook_repository,
            resource_repository=repository,
            source=source,
        )


def _apply_delete_metadata(
    *,
    repository: DeletionRepository,
    webhook_repository: WebhookEventRepository,
    request_id: UUID,
) -> DeletionRequestRecord | None:
    with repository.unit_of_work():
        request = repository.get_delete_request(request_id)
        if request is None:
            return None
        if request.status not in {"created", "started", "error"}:
            return request
        request.status = "started"
        request.error = None
        request.updated = utc_now()
        repository.save_delete_request(request)
        if request.timerange_remaining is not None:
            delete_filter = segment_delete_filter(
                flow_id=request.flow_id,
                timerange=request.timerange_remaining,
                object_id=None,
            )
            if request.delete_flow:
                request.timerange_remaining = _process_flow_delete_request(
                    repository=repository,
                    webhook_repository=webhook_repository,
                    request=request,
                    delete_filter=delete_filter,
                )
            else:
                request.timerange_remaining = _process_segment_delete_request(
                    repository=repository,
                    webhook_repository=webhook_repository,
                    request=request,
                    delete_filter=delete_filter,
                )
            request.updated = utc_now()
            repository.save_delete_request(request)
    return request


def _mark_delete_request_done(
    repository: DeletionRepository,
    request_id: UUID,
) -> None:
    with repository.unit_of_work():
        request = repository.get_delete_request(request_id)
        if request is None:
            return
        if repository.list_object_cleanups(
            delete_request_id=request_id,
            statuses=ACTIVE_CLEANUP_STATUSES,
        ):
            raise ObjectCleanupFailed
        request.timerange_remaining = None
        request.status = "done"
        request.error = None
        _clear_delete_request_claim(request)
        request.updated = utc_now()
        repository.save_delete_request(request)


def _release_delete_request_claim(
    repository: DeletionRepository,
    request_id: UUID,
) -> None:
    with repository.unit_of_work():
        request = repository.get_delete_request(request_id)
        if request is None:
            return
        _clear_delete_request_claim(request)
        request.updated = utc_now()
        repository.save_delete_request(request)


def _mark_delete_request_error(
    repository: DeletionRepository,
    request_id: UUID,
    *,
    error_type: str,
    summary: str,
) -> None:
    request = repository.get_delete_request(request_id)
    if request is None:
        return
    request.status = "error"
    request.error = DomainErrorPayload.create(error_type, summary)
    request.claimed_at = None
    request.claimed_by = None
    request.updated = utc_now()
    repository.save_delete_request(request)


def _clear_delete_request_claim(request: DeletionRequestRecord) -> None:
    request.claimed_at = None
    request.claimed_by = None
    request.claim_expires_at = None


def _process_flow_delete_request(
    *,
    repository: DeletionRepository,
    webhook_repository: WebhookEventRepository,
    request: DeletionRequestRecord,
    delete_filter: SegmentDeleteFilter,
) -> str | None:
    remaining_timerange = delete_matching_segments(
        repository=repository,
        webhook_repository=webhook_repository,
        delete_filter=delete_filter,
        publish_event=False,
        delete_request_id=request.id,
    )
    if remaining_timerange != "()":
        return remaining_timerange
    flow = repository.get_flow(request.flow_id)
    if flow is None:
        return None
    unlink_flow_collection_references(repository, flow)
    repository.delete_flow(request.flow_id)
    webhooking.publish_flow_deleted(
        repository=webhook_repository,
        resource_repository=repository,
        flow=flow,
    )
    delete_orphan_source(
        repository=repository,
        webhook_repository=webhook_repository,
        source_id=flow.source_id,
    )
    return None


def _process_segment_delete_request(
    *,
    repository: DeletionRepository,
    webhook_repository: WebhookEventRepository,
    request: DeletionRequestRecord,
    delete_filter: SegmentDeleteFilter,
) -> str | None:
    flow = repository.get_flow(request.flow_id)
    if flow is None:
        return None
    remaining_timerange = delete_matching_segments(
        repository=repository,
        webhook_repository=webhook_repository,
        delete_filter=delete_filter,
        delete_request_id=request.id,
    )
    flow.segments_updated = utc_now()
    repository.save_flow(flow)
    webhooking.publish_flow_event(
        repository=webhook_repository,
        resource_repository=repository,
        event_type="flows/updated",
        flow=flow,
    )
    if remaining_timerange == "()":
        return None
    return remaining_timerange


def _refresh_deleted_object_references(
    *,
    repository: DeletionRepository,
    flow_id: UUID,
    deleted_segments: list[SegmentRecord],
    delete_request_id: UUID | None,
) -> None:
    deleted_object_ids = {segment.object_id for segment in deleted_segments}
    remaining_segments = repository.list_segments_for_objects(
        flow_id=flow_id,
        object_ids=deleted_object_ids,
    )
    remaining_object_ids = {segment.object_id for segment in remaining_segments}
    media_objects = repository.get_objects(deleted_object_ids)
    segments_by_flow_id = {flow_id: remaining_segments}
    for object_id in deleted_object_ids:
        media_object = media_objects.get(object_id)
        if media_object is None:
            continue
        if object_id not in remaining_object_ids:
            media_object.referenced_by_flows.discard(flow_id)
        if media_object.referenced_by_flows:
            media_object.timerange = object_timerange(
                repository,
                media_object,
                segments_by_flow_id=segments_by_flow_id,
            )
            repository.save_object(media_object)
            continue
        queue_controlled_object_cleanup(
            repository=repository,
            media_object=media_object,
            delete_request_id=delete_request_id,
        )
        repository.delete_object(object_id)


def object_timerange(
    repository: DeletionRepository,
    media_object: MediaObjectRecord,
    *,
    segments_by_flow_id: dict[UUID, list[SegmentRecord]] | None = None,
) -> str | None:
    return timerange_union_strings(
        segment_object_timerange(segment)
        for flow_id in media_object.referenced_by_flows
        for segment in _object_segments_for_flow(
            repository=repository,
            flow_id=flow_id,
            object_id=media_object.id,
            segments_by_flow_id=segments_by_flow_id,
        )
    )


def _object_segments_for_flow(
    *,
    repository: DeletionRepository,
    flow_id: UUID,
    object_id: str,
    segments_by_flow_id: dict[UUID, list[SegmentRecord]] | None,
) -> list[SegmentRecord]:
    if segments_by_flow_id is not None and flow_id in segments_by_flow_id:
        return [
            segment
            for segment in segments_by_flow_id[flow_id]
            if segment.object_id == object_id
        ]
    return repository.list_segments_for_objects(flow_id=flow_id, object_ids=[object_id])


def queue_controlled_object_cleanup(
    *,
    repository: DeletionRepository,
    media_object: MediaObjectRecord,
    delete_request_id: UUID | None,
) -> None:
    seen_backend_ids: set[UUID] = set()
    for instance in media_object.instances:
        if (
            not instance.controlled
            or instance.storage_backend is None
            or instance.storage_backend.id in seen_backend_ids
        ):
            continue
        seen_backend_ids.add(instance.storage_backend.id)
        repository.save_object_cleanup(
            ObjectCleanupRecord(
                id=uuid4(),
                delete_request_id=delete_request_id,
                object_id=media_object.id,
                storage_backend_id=instance.storage_backend.id,
                status="pending",
            )
        )


def process_object_cleanups(
    *,
    repository: DeletionRepository,
    object_storage: ObjectStorage,
    cleanups: Iterable[ObjectCleanupRecord],
) -> None:
    cleanup_batches: dict[UUID, list[ObjectCleanupRecord]] = {}
    backends: dict[UUID, StorageBackend] = {}
    failed = False
    for cleanup in cleanups:
        _start_object_cleanup(repository, cleanup)
        backend = repository.get_storage_backend(cleanup.storage_backend_id)
        if backend is None:
            _mark_object_cleanup_error(repository, cleanup, exc_info=False)
            failed = True
            continue
        try:
            media_object = repository.get_objects([cleanup.object_id]).get(
                cleanup.object_id
            )
        except Exception:
            _mark_object_cleanup_error(repository, cleanup, exc_info=True)
            failed = True
            continue
        if media_object is not None and _has_controlled_instance(
            media_object,
            cleanup.storage_backend_id,
        ):
            logger.warning(
                "skipping object cleanup because backend instance remains advertised",
                extra={
                    "object_id": cleanup.object_id,
                    "storage_backend_id": str(cleanup.storage_backend_id),
                },
            )
            _mark_object_cleanup_done(repository, cleanup)
            continue
        backends[backend.id] = backend
        cleanup_batches.setdefault(backend.id, []).append(cleanup)

    for backend_id, batch in cleanup_batches.items():
        try:
            object_storage.delete_batch(
                [cleanup.object_id for cleanup in batch],
                backend=backends[backend_id],
            )
        except Exception:
            for cleanup in batch:
                _mark_object_cleanup_error(repository, cleanup, exc_info=True)
            failed = True
            continue
        for cleanup in batch:
            _mark_object_cleanup_done(repository, cleanup)

    if failed:
        raise ObjectCleanupFailed


def _has_controlled_instance(
    media_object: MediaObjectRecord,
    storage_backend_id: UUID,
) -> bool:
    return any(
        instance.controlled
        and instance.storage_backend is not None
        and instance.storage_backend.id == storage_backend_id
        for instance in media_object.instances
    )


def _mark_object_cleanup_done(
    repository: DeletionRepository,
    cleanup: ObjectCleanupRecord,
) -> None:
    cleanup.status = "done"
    cleanup.error = None
    _clear_object_cleanup_claim(cleanup)
    cleanup.updated = utc_now()
    repository.save_object_cleanup(cleanup)


def _mark_object_cleanup_error(
    repository: DeletionRepository,
    cleanup: ObjectCleanupRecord,
    *,
    exc_info: bool,
) -> None:
    logger.error(
        "object storage cleanup failed",
        extra={
            "object_cleanup_id": str(cleanup.id),
            "object_id": cleanup.object_id,
            "storage_backend_id": str(cleanup.storage_backend_id),
        },
        exc_info=exc_info,
    )
    cleanup.status = "error"
    cleanup.error = DomainErrorPayload.create(
        DELETE_CLEANUP_FAILED,
        "Object storage cleanup failed; retry will continue.",
    )
    cleanup.claimed_at = None
    cleanup.claimed_by = None
    cleanup.updated = utc_now()
    repository.save_object_cleanup(cleanup)


def _start_object_cleanup(
    repository: DeletionRepository,
    cleanup: ObjectCleanupRecord,
) -> None:
    cleanup.status = "started"
    cleanup.error = None
    cleanup.attempt_count += 1
    cleanup.updated = utc_now()
    repository.save_object_cleanup(cleanup)


def _clear_object_cleanup_claim(cleanup: ObjectCleanupRecord) -> None:
    cleanup.claimed_at = None
    cleanup.claimed_by = None
    cleanup.claim_expires_at = None
