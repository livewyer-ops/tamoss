from __future__ import annotations

from tamoss.application.contexts._shared import (
    DEFAULT_WORKER_ID,
    DEFAULT_WORKER_LEASE_SECONDS,
    ObjectStorage,
    QueryTimerange,
    SegmentRegistrationCandidate,
    SegmentWriteResult,
    Settings,
    SourceRelationships,
    TamossRepository,
    storage_backend_from_settings,
    tags_from_flow_data,
)
from tamoss.application.contexts.deletion import DeletionUseCases
from tamoss.application.contexts.flows import FlowUseCases
from tamoss.application.contexts.objects import ObjectUseCases
from tamoss.application.contexts.segments import SegmentUseCases
from tamoss.application.contexts.service import ServiceUseCases
from tamoss.application.contexts.sources import SourceUseCases
from tamoss.application.contexts.storage import StorageUseCases
from tamoss.application.contexts.webhooks import WebhookUseCases

__all__ = [
    "TamossUseCases",
    "DEFAULT_WORKER_ID",
    "DEFAULT_WORKER_LEASE_SECONDS",
    "ObjectStorage",
    "QueryTimerange",
    "SegmentRegistrationCandidate",
    "SegmentWriteResult",
    "Settings",
    "SourceRelationships",
    "TamossRepository",
    "storage_backend_from_settings",
    "tags_from_flow_data",
]


class TamossUseCases(
    ServiceUseCases,
    WebhookUseCases,
    DeletionUseCases,
    SourceUseCases,
    FlowUseCases,
    StorageUseCases,
    SegmentUseCases,
    ObjectUseCases,
):
    def __init__(
        self,
        *,
        repository: TamossRepository,
        object_storage: ObjectStorage,
        settings: Settings,
    ):
        self.repository = repository
        self.object_storage = object_storage
        self.settings = settings
