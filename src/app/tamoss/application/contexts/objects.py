from __future__ import annotations

from collections.abc import Mapping
from contextlib import suppress
from typing import Any
from urllib.parse import urlparse
from uuid import UUID

from tamoss.application.contexts import deletion_processor, object_copy
from tamoss.application.contexts.object_get_urls import objects_get_urls
from tamoss.domain.model import (
    MediaObjectRecord,
    ObjectInstance,
)
from tamoss.errors import BadRequest, NotFound
from tamoss.ports.object_storage import ObjectStorage
from tamoss.ports.repositories import (
    ObjectCleanupRepository,
    ObjectRepository,
    StorageBackendRepository,
)
from tamoss.worker_claims import WorkerClaimLost, keep_worker_claims


def reserved_storage_labels(repository: StorageBackendRepository) -> set[str]:
    """Storage backend labels reserved for service-controlled instances."""
    return {
        backend.label for backend in repository.list_storage_backends() if backend.label
    }


class ObjectUseCases:
    repository: ObjectRepository
    object_storage: ObjectStorage
    cleanup_repository: ObjectCleanupRepository

    def __init__(
        self,
        *,
        repository: ObjectRepository,
        object_storage: ObjectStorage,
        cleanup_repository: ObjectCleanupRepository,
    ) -> None:
        self.repository = repository
        self.object_storage = object_storage
        self.cleanup_repository = cleanup_repository

    def register_object_instance(
        self, *, object_id: str, registration: Mapping[str, Any]
    ) -> None:
        storage_id = registration.get("storage_id")
        url = registration.get("url")
        label = registration.get("label")
        has_controlled = storage_id is not None
        has_uncontrolled = url is not None or label is not None
        if has_controlled == has_uncontrolled:
            raise BadRequest("Bad request. Invalid request JSON.")
        if has_controlled:
            self._queue_controlled_object_copy(object_id, UUID(str(storage_id)))
            return
        with self.repository.unit_of_work():
            self.repository.lock_objects([object_id])
            media_object = self.get_object(object_id)
            self._register_uncontrolled_object_instance(
                media_object,
                url=str(url) if url is not None else None,
                label=str(label) if label is not None else None,
            )
            self.repository.save_object(media_object)

    def delete_object_instance(
        self, *, object_id: str, storage_id: UUID | None, label: str | None
    ) -> None:
        if (storage_id is None and label is None) or (
            storage_id is not None and label is not None
        ):
            raise BadRequest("Bad request. Invalid query options.")
        with self.repository.unit_of_work():
            self.repository.lock_objects([object_id])
            media_object = self.repository.get_object(object_id)
            if media_object is None or not media_object.referenced_by_flows:
                raise NotFound("The requested Media Object does not exist.")
            matches = [
                instance
                for instance in media_object.instances
                if _object_instance_matches(
                    instance, storage_id=storage_id, label=label
                )
            ]
            controlled_match_backend_ids = {
                instance.storage_backend.id
                for instance in matches
                if instance.controlled and instance.storage_backend is not None
            }
            if controlled_match_backend_ids:
                matches = [
                    instance
                    for instance in media_object.instances
                    if instance in matches
                    or (
                        instance.controlled
                        and instance.storage_backend is not None
                        and instance.storage_backend.id in controlled_match_backend_ids
                    )
                ]
            if not matches:
                raise NotFound("The requested Object instance does not exist.")
            if len(matches) == len(media_object.instances):
                raise BadRequest(
                    "Bad request. All instances would be deleted. "
                    "Use Flow Segment deletion instead."
                )

            media_object.instances = [
                instance
                for instance in media_object.instances
                if instance not in matches
            ]
            cleanup_object = MediaObjectRecord(id=object_id, instances=matches)
            deletion_processor.queue_controlled_object_cleanup(
                repository=self.cleanup_repository,
                media_object=cleanup_object,
                delete_request_id=None,
            )
            self.repository.save_object(media_object)

    def process_pending_object_copies(
        self,
        *,
        max_copies: int = 50,
        worker_id: str,
        lease_seconds: int,
    ) -> int:
        copies = self.repository.claim_object_copies(
            worker_id=worker_id,
            limit=max_copies,
            lease_seconds=lease_seconds,
        )
        with (
            suppress(object_copy.ObjectCopyFailed, WorkerClaimLost),
            keep_worker_claims(
                copies,
                renew=self.repository.renew_worker_claim,
                lease_seconds=lease_seconds,
            ),
        ):
            object_copy.process_object_copies(
                repository=self.repository,
                object_storage=self.object_storage,
                copies=copies,
            )
        return len(copies)

    def _queue_controlled_object_copy(
        self, object_id: str, destination_storage_id: UUID
    ) -> None:
        with self.repository.unit_of_work():
            self.repository.lock_objects([object_id])
            media_object = self.repository.get_object(object_id)
            if media_object is None or not media_object.referenced_by_flows:
                raise NotFound("The requested Media Object does not exist.")
            destination_backend = self.repository.get_storage_backend(
                destination_storage_id
            )
            if destination_backend is None:
                raise BadRequest("The requested Storage Backend does not exist.")
            source_instance = next(
                (
                    instance
                    for instance in media_object.instances
                    if instance.controlled and instance.storage_backend is not None
                ),
                None,
            )
            if source_instance is None or source_instance.storage_backend is None:
                raise BadRequest("Bad request. Invalid request JSON.")
            if any(
                instance.controlled
                and instance.storage_backend is not None
                and instance.storage_backend.id == destination_storage_id
                for instance in media_object.instances
            ):
                raise BadRequest("Bad request. Invalid request JSON.")
            object_copy.queue_controlled_object_copy(
                repository=self.repository,
                object_id=object_id,
                object_created=media_object.created,
                source_storage_backend_id=source_instance.storage_backend.id,
                destination_storage_backend_id=destination_storage_id,
            )

    def _register_uncontrolled_object_instance(
        self, media_object: MediaObjectRecord, *, url: str | None, label: str | None
    ) -> None:
        if not label or not url or not _is_http_url(url):
            raise BadRequest("Bad request. Invalid request JSON.")
        if label in reserved_storage_labels(self.repository):
            raise BadRequest("Bad request. Invalid request JSON.")
        try:
            append_uncontrolled_instance(
                media_object,
                url=url,
                label=label,
                presigned=False,
            )
        except ValueError as exc:
            raise BadRequest("Bad request. Invalid request JSON.") from exc

    def get_object(self, object_id: str) -> MediaObjectRecord:
        media_object = self.repository.get_object(object_id)
        if media_object is None or not media_object.referenced_by_flows:
            raise NotFound("The requested Media Object does not exist.")
        return media_object

    def object_get_urls(
        self,
        media_object: MediaObjectRecord,
        *,
        accept_get_urls: set[str] | None = None,
        accept_storage_ids: set[str] | None = None,
        presigned: bool | None = None,
        verbose_storage: bool = False,
        storage_tag_values: dict[str, set[str]] | None = None,
        storage_tag_exists: dict[str, bool] | None = None,
    ) -> list[dict[str, Any]]:
        return objects_get_urls(
            [media_object],
            object_storage=self.object_storage,
            accept_get_urls=accept_get_urls,
            accept_storage_ids=accept_storage_ids,
            presigned=presigned,
            verbose_storage=verbose_storage,
            storage_tag_values=storage_tag_values,
            storage_tag_exists=storage_tag_exists,
        ).get(media_object.id, [])

    def object_with_init(
        self, media_object: MediaObjectRecord
    ) -> MediaObjectRecord | None:
        if media_object.init_object_id is None:
            return None
        return self.repository.get_object(media_object.init_object_id)


def _object_instance_matches(
    instance: ObjectInstance, *, storage_id: UUID | None, label: str | None
) -> bool:
    if storage_id is not None and (
        instance.storage_backend is None or instance.storage_backend.id != storage_id
    ):
        return False
    return not (label is not None and instance.label != label)


def _is_http_url(value: str) -> bool:
    parsed = urlparse(value)
    return parsed.scheme in {"http", "https"} and bool(parsed.netloc)


def append_uncontrolled_instance(
    media_object: MediaObjectRecord, *, url: str, label: str | None, presigned: bool
) -> None:
    validate_uncontrolled_instance_append(
        media_object,
        url=url,
        label=label,
        presigned=presigned,
    )
    for instance in media_object.instances:
        if (
            not instance.controlled
            and instance.label == label
            and instance.url == url
            and instance.presigned is presigned
        ):
            return
    media_object.instances.append(
        ObjectInstance(
            storage_backend=None,
            url=url,
            label=label,
            controlled=False,
            presigned=presigned,
        )
    )


def validate_uncontrolled_instance_append(
    media_object: MediaObjectRecord, *, url: str, label: str | None, presigned: bool
) -> None:
    for instance in media_object.instances:
        if instance.controlled or instance.label != label:
            continue
        if instance.url == url and instance.presigned is presigned:
            return
        raise ValueError("conflicting object instance label")
