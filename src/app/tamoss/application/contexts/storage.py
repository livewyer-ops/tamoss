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
        limit = request.get("limit")
        object_ids = request.get("object_ids")
        storage_id = request.get("storage_id")
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

        allocations: list[dict[str, object]] = []
        with self.repository.unit_of_work():
            object_ids = requested_object_ids
            if request.get("object_ids") is None:
                object_ids = []
                while len(object_ids) < (limit or 1):
                    object_id = str(uuid4())
                    if self._reserve_allocated_object(
                        object_id=object_id,
                        backend=backend,
                        flow_id=flow_id,
                    ):
                        object_ids.append(object_id)
            else:
                for object_id in object_ids:
                    if not self._reserve_allocated_object(
                        object_id=object_id,
                        backend=backend,
                        flow_id=flow_id,
                    ):
                        raise BadRequest(
                            "One or more supplied object_ids already exist."
                        )

            allocations.extend(
                [
                    {
                        "object_id": object_id,
                        "storage_id": str(backend.id),
                        "put_url": self.object_storage.build_put_request(
                            object_id=object_id,
                            flow_container=flow.container,
                            backend=backend,
                        ),
                    }
                    for object_id in object_ids
                ]
            )
        return allocations

    def _reserve_allocated_object(
        self, *, object_id: str, backend: StorageBackend, flow_id: UUID
    ) -> bool:
        self._validate_storage_object_id(object_id)
        media_object = MediaObjectRecord(id=object_id, allocated_by_flow=flow_id)
        media_object.instances.append(
            ObjectInstance(
                storage_backend=backend,
                url=None,
                label=backend.label,
                controlled=True,
            )
        )
        return self.repository.create_object(media_object)

    def _validate_storage_object_id(self, object_id: str) -> None:
        if (
            not object_id
            or len(object_id) > self.settings.storage_object_id_max_length
            or object_id.startswith("/")
            or any(ord(character) < 32 for character in object_id)
            or any(part in {"", ".", ".."} for part in object_id.split("/"))
        ):
            raise BadRequest("Bad request. Invalid object_id.")
