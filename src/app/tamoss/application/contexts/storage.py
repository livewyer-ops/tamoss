from __future__ import annotations

from collections.abc import Mapping
from typing import Any
from uuid import UUID, uuid4

from tamoss.application.contexts.flows import ensure_flow_writable, require_flow
from tamoss.domain.model import MediaObjectRecord, ObjectInstance, StorageBackend
from tamoss.errors import BadRequest
from tamoss.ports.object_storage import ObjectStorage
from tamoss.ports.repositories import FlowLookupRepository, StorageRepository
from tamoss.settings import Settings


class StorageUseCases:
    repository: StorageRepository
    object_storage: ObjectStorage
    settings: Settings
    flow_repository: FlowLookupRepository

    def __init__(
        self,
        *,
        repository: StorageRepository,
        object_storage: ObjectStorage,
        settings: Settings,
        flow_repository: FlowLookupRepository,
    ) -> None:
        self.repository = repository
        self.object_storage = object_storage
        self.settings = settings
        self.flow_repository = flow_repository

    def allocate_flow_storage(
        self, *, flow_id: UUID, request: Mapping[str, Any]
    ) -> list[dict[str, object]]:
        flow = require_flow(self.flow_repository, flow_id)
        ensure_flow_writable(flow)
        if not flow.container:
            raise BadRequest("Bad request. The Flow 'container' is not set.")
        if "content-type" in request:
            raise BadRequest(
                "Bad request. Use content_type instead of the unsupported "
                "content-type field."
            )
        limit = request.get("limit")
        object_ids = request.get("object_ids")
        storage_id = request.get("storage_id")
        requested_content_type = request.get("content_type")
        if requested_content_type is not None and (
            not flow.init_segments or str(requested_content_type) == flow.container
        ):
            raise BadRequest(
                "Bad request. content_type is only valid for init Objects "
                "whose type differs from the Flow container."
            )
        content_type = str(requested_content_type or flow.container)
        presigned = request.get("presigned") is not False
        if limit is not None and object_ids is not None:
            raise BadRequest("Specify either limit or object_ids, not both.")
        if (limit or 1) > self.settings.storage_allocation_max_objects:
            raise BadRequest("Bad request. Storage allocation limit is too high.")

        backend = (
            self.repository.get_storage_backend(UUID(str(storage_id)))
            if storage_id
            else self.repository.default_storage_backend()
        )
        if backend is None:
            raise BadRequest("The requested Storage Backend does not exist.")

        requested_object_ids = [str(object_id) for object_id in object_ids or []]
        if len(requested_object_ids) > self.settings.storage_allocation_max_objects:
            raise BadRequest("Bad request. Storage allocation limit is too high.")
        if len(requested_object_ids) != len(set(requested_object_ids)):
            raise BadRequest("One or more supplied object_ids are duplicated.")
        for object_id in requested_object_ids:
            self._validate_storage_object_id(object_id)

        with self.repository.unit_of_work():
            object_ids = requested_object_ids
            if request.get("object_ids") is None:
                object_ids = []
                while len(object_ids) < (limit or 1):
                    batch_ids = [
                        str(uuid4()) for _ in range((limit or 1) - len(object_ids))
                    ]
                    self.repository.lock_objects(batch_ids)
                    created = self.repository.create_objects(
                        self._allocated_object(
                            object_id=object_id,
                            backend=backend,
                            flow_id=flow_id,
                            content_type=content_type,
                        )
                        for object_id in batch_ids
                    )
                    object_ids.extend(
                        object_id for object_id in batch_ids if object_id in created
                    )
            else:
                self.repository.lock_objects(object_ids)
                created = self.repository.create_objects(
                    self._allocated_object(
                        object_id=object_id,
                        backend=backend,
                        flow_id=flow_id,
                        content_type=content_type,
                    )
                    for object_id in object_ids
                )
                if len(created) != len(object_ids):
                    raise BadRequest("One or more supplied object_ids already exist.")

        # Presigning is deliberately outside the transaction: the reservation
        # rows do not depend on the URL strings, and a failure here leaves
        # unallocated reservations to the stale-allocation cleanup worker.
        return [
            {
                "object_id": object_id,
                "storage_id": str(backend.id),
                "put_url": self.object_storage.build_put_request(
                    object_id=object_id,
                    content_type=content_type,
                    backend=backend,
                    presigned=presigned,
                ),
                "presigned": presigned,
            }
            for object_id in object_ids
        ]

    def _allocated_object(
        self,
        *,
        object_id: str,
        backend: StorageBackend,
        flow_id: UUID,
        content_type: str,
    ) -> MediaObjectRecord:
        self._validate_storage_object_id(object_id)
        media_object = MediaObjectRecord(
            id=object_id,
            allocated_by_flow=flow_id,
            content_type=content_type,
        )
        media_object.instances.append(
            ObjectInstance(
                storage_backend=backend,
                url=None,
                label=backend.label,
                controlled=True,
            )
        )
        return media_object

    def _validate_storage_object_id(self, object_id: str) -> None:
        if (
            not object_id
            or len(object_id) > self.settings.storage_object_id_max_length
            or object_id.startswith("/")
            or any(ord(character) < 32 for character in object_id)
            or any(part in {"", ".", ".."} for part in object_id.split("/"))
        ):
            raise BadRequest("Bad request. Invalid object_id.")
