from __future__ import annotations

from typing import TYPE_CHECKING

from tamoss.application.contexts.deletion import DeletionUseCases
from tamoss.application.contexts.flows import FlowUseCases
from tamoss.application.contexts.objects import ObjectUseCases
from tamoss.application.contexts.profiles import ProfileUseCases
from tamoss.application.contexts.segments import SegmentUseCases
from tamoss.application.contexts.service import ServiceUseCases
from tamoss.application.contexts.sources import SourceUseCases
from tamoss.application.contexts.storage import StorageUseCases
from tamoss.application.contexts.webhooks import WebhookUseCases
from tamoss.settings import Settings

if TYPE_CHECKING:
    from tamoss.adapters.object_storage import ConfiguredObjectStorage
    from tamoss.adapters.postgres import PostgresRepository


class TamossUseCases:
    repository: PostgresRepository
    object_storage: ConfiguredObjectStorage
    settings: Settings
    service: ServiceUseCases
    profiles: ProfileUseCases
    webhooks: WebhookUseCases
    deletion: DeletionUseCases
    sources: SourceUseCases
    flows: FlowUseCases
    storage: StorageUseCases
    segments: SegmentUseCases
    objects: ObjectUseCases

    def __init__(
        self,
        *,
        repository: PostgresRepository,
        object_storage: ConfiguredObjectStorage,
        settings: Settings,
    ):
        self.repository = repository
        self.object_storage = object_storage
        self.settings = settings
        profile_repository = getattr(repository, "profile_repository", repository)
        self.service = ServiceUseCases(
            repository=repository.service_repository,
            settings=settings,
        )
        self.profiles = ProfileUseCases(repository=profile_repository)
        self.webhooks = WebhookUseCases(
            repository=repository.webhook_repository,
            settings=settings,
        )
        self.deletion = DeletionUseCases(
            repository=repository.deletion_repository,
            object_storage=object_storage,
            webhook_repository=repository.webhook_repository,
            settings=settings,
        )
        self.sources = SourceUseCases(
            repository=repository.source_repository,
            webhook_repository=repository.webhook_repository,
        )
        self.flows = FlowUseCases(
            repository=repository.flow_repository,
            profile_repository=profile_repository,
            webhook_repository=repository.webhook_repository,
        )
        self.storage = StorageUseCases(
            repository=repository.storage_repository,
            object_storage=object_storage,
            settings=settings,
            flow_repository=repository.flow_repository,
        )
        self.segments = SegmentUseCases(
            repository=repository.segment_repository,
            object_storage=object_storage,
            flow_repository=repository.flow_repository,
            webhook_repository=repository.webhook_repository,
        )
        self.objects = ObjectUseCases(
            repository=repository.object_repository,
            object_storage=object_storage,
            cleanup_repository=repository.deletion_repository,
        )
