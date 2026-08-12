from __future__ import annotations

from collections.abc import Mapping
from typing import Any

from tamoss.contract.payloads import JsonPayload, without_none
from tamoss.domain.model import ServiceMetadata, StorageBackend
from tamoss.domain.pagination import Page
from tamoss.errors import Forbidden
from tamoss.ports.repositories import ServiceRepository
from tamoss.settings import Settings


class ServiceUseCases:
    repository: ServiceRepository
    settings: Settings

    def __init__(
        self,
        *,
        repository: ServiceRepository,
        settings: Settings,
    ) -> None:
        self.repository = repository
        self.settings = settings

    def root_paths(self) -> list[str]:
        return ["service", "flows", "sources", "objects", "flow-delete-requests"]

    def service_info(self) -> JsonPayload:
        # Operator-managed identity ignores any stored metadata, so a value set
        # through the API before the Tamoss resource claimed identity cannot
        # shadow the declared one.
        metadata = (
            None
            if self.settings.service_identity_managed
            else self.repository.get_service_metadata()
        )
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
        return without_none(info)

    def update_service_info(self, update: Mapping[str, Any]) -> None:
        if self.settings.service_identity_managed:
            raise Forbidden(
                "Forbidden. Service identity is managed by the Tamoss resource. "
                "Update spec.serviceIdentity and reapply."
            )
        self.repository.save_service_metadata(
            ServiceMetadata(
                name=update.get("name") or None,
                description=update.get("description") or None,
            )
        )

    def list_storage_backends(self) -> list[StorageBackend]:
        return self.repository.list_storage_backends()

    def list_storage_backends_page(
        self,
        *,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        reverse_order: bool,
        page: str | None,
        limit: int | None,
    ) -> Page[StorageBackend]:
        return self.repository.list_storage_backends_page(
            tag_values=tag_values,
            tag_exists=tag_exists,
            reverse_order=reverse_order,
            page=page,
            limit=limit,
        )
