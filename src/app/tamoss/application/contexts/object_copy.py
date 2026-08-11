from __future__ import annotations

import logging
from collections.abc import Iterable
from uuid import UUID, uuid4

from tamoss.domain.model import (
    DomainErrorPayload,
    ObjectCopyRecord,
    ObjectInstance,
    utc_now,
)
from tamoss.ports.object_storage import ObjectStorage
from tamoss.ports.repositories import ObjectRepository

logger = logging.getLogger(__name__)

OBJECT_COPY_FAILED = "object_copy_failed"


class ObjectCopyFailed(RuntimeError):
    pass


def queue_controlled_object_copy(
    *,
    repository: ObjectRepository,
    object_id: str,
    source_storage_backend_id: UUID,
    destination_storage_backend_id: UUID,
) -> None:
    repository.save_object_copy(
        ObjectCopyRecord(
            id=uuid4(),
            object_id=object_id,
            source_storage_backend_id=source_storage_backend_id,
            destination_storage_backend_id=destination_storage_backend_id,
            status="pending",
        )
    )


def process_object_copies(
    *,
    repository: ObjectRepository,
    object_storage: ObjectStorage,
    copies: Iterable[ObjectCopyRecord],
) -> None:
    failed = False
    for copy in copies:
        _start_object_copy(repository, copy)
        try:
            _process_object_copy(
                repository=repository,
                object_storage=object_storage,
                copy=copy,
            )
        except Exception:
            _mark_object_copy_error(repository, copy)
            failed = True
    if failed:
        raise ObjectCopyFailed


def _process_object_copy(
    *,
    repository: ObjectRepository,
    object_storage: ObjectStorage,
    copy: ObjectCopyRecord,
) -> None:
    media_object = repository.get_object(copy.object_id)
    if media_object is None or not media_object.referenced_by_flows:
        _mark_object_copy_done(repository, copy)
        return
    source_backend = repository.get_storage_backend(copy.source_storage_backend_id)
    destination_backend = repository.get_storage_backend(
        copy.destination_storage_backend_id
    )
    if source_backend is None or destination_backend is None:
        raise RuntimeError("Object copy references an unknown storage backend.")
    if _controlled_instance(media_object.instances, destination_backend.id):
        _mark_object_copy_done(repository, copy)
        return
    if not _controlled_instance(media_object.instances, source_backend.id):
        raise RuntimeError("Object copy source instance no longer exists.")

    object_storage.copy(
        copy.object_id,
        source_backend=source_backend,
        destination_backend=destination_backend,
    )

    should_delete_copied_object = False
    with repository.unit_of_work():
        repository.lock_objects([copy.object_id])
        media_object = repository.get_object(copy.object_id)
        if media_object is None or not media_object.referenced_by_flows:
            should_delete_copied_object = True
        elif not _controlled_instance(media_object.instances, destination_backend.id):
            media_object.instances.append(
                ObjectInstance(
                    storage_backend=destination_backend,
                    url=None,
                    label=destination_backend.label,
                    controlled=True,
                )
            )
            repository.save_object(media_object)
        if not should_delete_copied_object:
            _mark_object_copy_done(repository, copy)

    if should_delete_copied_object:
        object_storage.delete(copy.object_id, backend=destination_backend)
        _mark_object_copy_done(repository, copy)


def _controlled_instance(
    instances: Iterable[ObjectInstance], storage_backend_id: UUID
) -> ObjectInstance | None:
    return next(
        (
            instance
            for instance in instances
            if (
                instance.controlled
                and instance.storage_backend is not None
                and instance.storage_backend.id == storage_backend_id
            )
        ),
        None,
    )


def _start_object_copy(repository: ObjectRepository, copy: ObjectCopyRecord) -> None:
    copy.status = "started"
    copy.error = None
    copy.attempt_count += 1
    copy.updated = utc_now()
    repository.save_object_copy(copy)


def _mark_object_copy_done(
    repository: ObjectRepository,
    copy: ObjectCopyRecord,
) -> None:
    copy.status = "done"
    copy.error = None
    copy.claimed_at = None
    copy.claimed_by = None
    copy.claim_expires_at = None
    copy.updated = utc_now()
    repository.save_object_copy(copy)


def _mark_object_copy_error(
    repository: ObjectRepository,
    copy: ObjectCopyRecord,
) -> None:
    logger.exception(
        "object copy failed",
        extra={
            "object_copy_id": str(copy.id),
            "object_id": copy.object_id,
            "source_storage_backend_id": str(copy.source_storage_backend_id),
            "destination_storage_backend_id": str(copy.destination_storage_backend_id),
        },
    )
    copy.status = "error"
    copy.error = DomainErrorPayload.create(
        OBJECT_COPY_FAILED,
        "Object copy failed; retry will continue.",
    )
    copy.claimed_at = None
    copy.claimed_by = None
    copy.updated = utc_now()
    repository.save_object_copy(copy)
