from __future__ import annotations

from tamoss.application.contexts._shared import (
    ServiceInfoUpdate,
    ServiceMetadata,
    StorageBackend,
    UseCaseContext,
)


class ServiceUseCases(UseCaseContext):
    def root_paths(self) -> list[str]:
        return ["service", "flows", "sources", "objects", "flow-delete-requests"]

    def service_info(self) -> dict:
        metadata = self.repository.get_service_metadata()
        info = {
            "type": "urn:x-tams:service.tamoss",
            "api_version": self.settings.api_version,
            "service_version": self.settings.service_version,
            "name": self.settings.service_name if metadata is None else metadata.name,
            "description": self.settings.service_description
            if metadata is None
            else metadata.description,
            "event_stream_mechanisms": [{"name": "webhooks"}],
            "min_object_timeout": self.settings.min_object_timeout,
            "min_presigned_url_timeout": self.settings.min_presigned_url_timeout,
        }
        return {key: value for key, value in info.items() if value is not None}

    def update_service_info(self, update: ServiceInfoUpdate) -> None:
        self.repository.save_service_metadata(
            ServiceMetadata(
                name=update.name or None,
                description=update.description or None,
            )
        )

    def list_storage_backends(self) -> list[StorageBackend]:
        return self.repository.list_storage_backends()
