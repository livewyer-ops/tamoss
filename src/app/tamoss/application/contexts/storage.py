from __future__ import annotations

from tamoss.application.contexts._shared import (
    UUID,
    BadRequest,
    FlowStoragePost,
    Forbidden,
    MediaObjectRecord,
    ObjectInstance,
    StorageBackend,
    UseCaseContext,
    uuid4,
)


class StorageUseCases(UseCaseContext):
    def allocate_flow_storage(
        self, *, flow_id: UUID, request: FlowStoragePost
    ) -> list[dict]:
        flow = self.get_flow(flow_id)
        if flow.read_only:
            raise Forbidden(
                "Forbidden. You do not have permission to modify this Flow. "
                "It may be marked read-only."
            )
        if not flow.container:
            raise BadRequest("Bad request. The Flow 'container' is not set.")
        if request.limit is not None and request.object_ids is not None:
            raise BadRequest("Specify either limit or object_ids, not both.")

        backend = (
            self.repository.get_storage_backend(request.storage_id)
            if request.storage_id
            else self.repository.default_storage_backend()
        )
        if backend is None:
            raise BadRequest("The requested Storage Backend does not exist.")

        requested_object_ids = list(request.object_ids or [])
        if len(requested_object_ids) != len(set(requested_object_ids)):
            raise BadRequest("One or more supplied object_ids are duplicated.")

        allocations: list[dict] = []
        with self.repository.unit_of_work():
            object_ids = requested_object_ids
            if request.object_ids is None:
                object_ids = []
                while len(object_ids) < (request.limit or 1):
                    object_id = str(uuid4())
                    if self._reserve_allocated_object(
                        object_id=object_id,
                        backend=backend,
                    ):
                        object_ids.append(object_id)
            else:
                for object_id in object_ids:
                    if not self._reserve_allocated_object(
                        object_id=object_id,
                        backend=backend,
                    ):
                        raise BadRequest(
                            "One or more supplied object_ids already exist."
                        )

            for object_id in object_ids:
                allocations.append(
                    {
                        "object_id": object_id,
                        "put_url": self.object_storage.build_put_request(
                            object_id=object_id,
                            flow_container=flow.container,
                            backend=backend,
                        ),
                    }
                )
        return allocations

    def _reserve_allocated_object(
        self, *, object_id: str, backend: StorageBackend
    ) -> bool:
        media_object = MediaObjectRecord(id=object_id)
        media_object.instances.append(
            ObjectInstance(
                storage_backend=backend,
                url=None,
                label=backend.label,
                controlled=True,
            )
        )
        return self.repository.create_object(media_object)
