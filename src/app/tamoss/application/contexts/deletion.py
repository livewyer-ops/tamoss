from __future__ import annotations

from contextlib import suppress
from datetime import timedelta
from uuid import UUID, uuid4

from tamoss.application import webhooks as webhooking
from tamoss.application.contexts import deletion_processor
from tamoss.application.contexts.flows import (
    ensure_flow_writable,
    require_flow,
    unlink_flow_collection_references,
)
from tamoss.auth import Identity
from tamoss.domain.listings import DeleteRequestSortBy
from tamoss.domain.model import (
    DeletionRequestRecord,
    MediaObjectRecord,
    utc_now,
)
from tamoss.domain.pagination import Page
from tamoss.domain.segments import segment_delete_filter
from tamoss.errors import BadRequest, NotFound
from tamoss.ports.object_storage import ObjectStorage
from tamoss.ports.repositories import DeletionRepository, WebhookEventRepository
from tamoss.settings import (
    DEFAULT_WORKER_ID,
    DEFAULT_WORKER_LEASE_SECONDS,
    Settings,
)


class DeletionUseCases:
    repository: DeletionRepository
    object_storage: ObjectStorage
    webhook_repository: WebhookEventRepository
    settings: Settings

    def __init__(
        self,
        *,
        repository: DeletionRepository,
        object_storage: ObjectStorage,
        webhook_repository: WebhookEventRepository,
        settings: Settings,
    ) -> None:
        self.repository = repository
        self.object_storage = object_storage
        self.webhook_repository = webhook_repository
        self.settings = settings

    def list_delete_requests(self) -> list[DeletionRequestRecord]:
        requests = self.repository.list_delete_requests()
        requests.sort(key=lambda request: str(request.id))
        return requests

    def list_delete_requests_page(
        self,
        *,
        sort_by: DeleteRequestSortBy,
        reverse_order: bool,
        page: str | None,
        limit: int | None,
    ) -> Page[DeletionRequestRecord]:
        return self.repository.list_delete_requests_page(
            sort_by=sort_by,
            reverse_order=reverse_order,
            retention_seconds=self.settings.worker_queue_retention_seconds,
            page=page,
            limit=limit,
        )

    def get_delete_request(self, request_id: UUID) -> DeletionRequestRecord:
        request = self.repository.get_delete_request(request_id)
        if request is None:
            raise NotFound("The requested flow delete request does not exist.")
        return request

    def delete_flow(
        self, *, flow_id: UUID, identity: Identity
    ) -> DeletionRequestRecord | None:
        flow = require_flow(self.repository, flow_id)
        ensure_flow_writable(flow)
        delete_filter = segment_delete_filter(
            flow_id=flow_id,
            timerange=None,
            object_id=None,
        )
        timerange_to_delete = self.repository.segment_delete_timerange(delete_filter)
        if timerange_to_delete == "()":
            with self.repository.unit_of_work():
                unlink_flow_collection_references(self.repository, flow)
                self.repository.delete_flow(flow_id)
                webhooking.publish_flow_deleted(
                    repository=self.webhook_repository,
                    resource_repository=self.repository,
                    flow=flow,
                )
                deletion_processor.delete_orphan_source(
                    repository=self.repository,
                    webhook_repository=self.webhook_repository,
                    source_id=flow.source_id,
                )
            return None

        return self._record_deletion_request(
            flow_id=flow_id,
            timerange_to_delete=timerange_to_delete,
            delete_flow=True,
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
        flow = require_flow(self.repository, flow_id)
        ensure_flow_writable(flow)
        try:
            delete_filter = segment_delete_filter(
                flow_id=flow_id,
                timerange=timerange,
                object_id=object_id,
            )
        except ValueError as exc:
            raise BadRequest("Bad request. Invalid query options.") from exc
        matching_timerange = self.repository.segment_delete_timerange(delete_filter)
        if matching_timerange == "()":
            return None
        if object_id is not None:
            with self.repository.unit_of_work():
                deletion_processor.delete_matching_segments(
                    repository=self.repository,
                    webhook_repository=self.webhook_repository,
                    delete_filter=delete_filter,
                    delete_request_id=None,
                    drain=True,
                )
                flow.segments_updated = utc_now()
                self.repository.save_flow(flow)
                webhooking.publish_flow_event(
                    repository=self.webhook_repository,
                    resource_repository=self.repository,
                    event_type="flows/updated",
                    flow=flow,
                )
            return None

        timerange_to_delete = matching_timerange
        if timerange not in (None, "", "_"):
            timerange_to_delete = timerange
        assert timerange_to_delete is not None
        return self._record_deletion_request(
            flow_id=flow_id,
            timerange_to_delete=timerange_to_delete,
            delete_flow=False,
            identity=identity,
        )

    def _record_deletion_request(
        self,
        *,
        flow_id: UUID,
        timerange_to_delete: str,
        delete_flow: bool,
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
            deletion_processor.process_delete_request(
                repository=self.repository,
                webhook_repository=self.webhook_repository,
                object_storage=self.object_storage,
                request_id=request.id,
            )
            processed += 1
        return processed

    def process_pending_object_cleanups(
        self,
        *,
        max_cleanups: int = 50,
        worker_id: str = DEFAULT_WORKER_ID,
        lease_seconds: int = DEFAULT_WORKER_LEASE_SECONDS,
    ) -> int:
        cleanups = self.repository.claim_object_cleanups(
            worker_id=worker_id,
            limit=max_cleanups,
            lease_seconds=lease_seconds,
        )
        with suppress(deletion_processor.ObjectCleanupFailed):
            deletion_processor.process_object_cleanups(
                repository=self.repository,
                object_storage=self.object_storage,
                cleanups=cleanups,
            )
        return len(cleanups)

    def queue_stale_allocated_object_cleanups(self, *, max_objects: int = 50) -> int:
        before = utc_now() - timedelta(
            seconds=self.settings.min_object_timeout_seconds()
        )
        with self.repository.unit_of_work():
            media_objects = self.repository.list_unreferenced_objects_created_before(
                before=before,
                limit=max_objects,
            )
            queued = 0
            for media_object in media_objects:
                if not _has_controlled_instance(media_object):
                    continue
                deletion_processor.queue_controlled_object_cleanup(
                    repository=self.repository,
                    media_object=media_object,
                    delete_request_id=None,
                )
                self.repository.delete_object(media_object.id)
                queued += 1
        return queued


def _has_controlled_instance(media_object: MediaObjectRecord) -> bool:
    return any(
        instance.controlled and instance.storage_backend is not None
        for instance in media_object.instances
    )
