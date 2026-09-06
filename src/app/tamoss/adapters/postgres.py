from __future__ import annotations

from collections.abc import Callable
from contextlib import AbstractContextManager
from contextvars import ContextVar
from typing import Any

from psycopg_pool import ConnectionPool

from tamoss.adapters.postgres_repository.connection import PostgresConnectionMixin
from tamoss.adapters.postgres_repository.flows_sources import PostgresFlowSourceMixin
from tamoss.adapters.postgres_repository.objects_segments import (
    PostgresObjectSegmentMixin,
)
from tamoss.adapters.postgres_repository.profiles import PostgresProfileMixin
from tamoss.adapters.postgres_repository.queues import PostgresQueueMixin
from tamoss.adapters.postgres_repository.storage_service import (
    PostgresStorageServiceMixin,
)
from tamoss.domain.model import StorageBackend


class _PostgresStoreBase:
    """Focused store base sharing one repository-owned connection manager."""

    def __init__(self, connection: PostgresConnectionMixin) -> None:
        self._connection = connection

    @property
    def _configured_storage_backend(self) -> StorageBackend:
        return self._connection._configured_storage_backend

    @property
    def _transaction_connection(self) -> ContextVar[Any | None]:
        return self._connection._transaction_connection

    def _connect(self) -> AbstractContextManager[Any]:
        return self._connection._connect()

    def unit_of_work(self) -> AbstractContextManager[Any]:
        return self._connection.unit_of_work()


class _PostgresServiceStore(PostgresStorageServiceMixin, _PostgresStoreBase):
    pass


class _PostgresProfileStore(PostgresProfileMixin, _PostgresStoreBase):
    pass


class _PostgresWebhookStore(PostgresQueueMixin, _PostgresStoreBase):
    pass


class _PostgresDeletionStore(
    PostgresFlowSourceMixin,
    PostgresObjectSegmentMixin,
    PostgresQueueMixin,
    PostgresStorageServiceMixin,
    _PostgresStoreBase,
):
    pass


class _PostgresSourceStore(PostgresFlowSourceMixin, _PostgresStoreBase):
    pass


class _PostgresFlowStore(
    PostgresStorageServiceMixin,
    PostgresFlowSourceMixin,
    PostgresObjectSegmentMixin,
    _PostgresStoreBase,
):
    pass


class _PostgresStorageStore(
    PostgresStorageServiceMixin,
    PostgresObjectSegmentMixin,
    _PostgresStoreBase,
):
    pass


class _PostgresSegmentStore(
    PostgresStorageServiceMixin,
    PostgresFlowSourceMixin,
    PostgresObjectSegmentMixin,
    _PostgresStoreBase,
):
    pass


class _PostgresObjectStore(
    PostgresStorageServiceMixin,
    PostgresObjectSegmentMixin,
    PostgresQueueMixin,
    _PostgresStoreBase,
):
    pass


class PostgresRepository(PostgresConnectionMixin):
    """PostgreSQL connection manager and focused repository bundle."""

    service_repository: _PostgresServiceStore
    profile_repository: _PostgresProfileStore
    webhook_repository: _PostgresWebhookStore
    deletion_repository: _PostgresDeletionStore
    source_repository: _PostgresSourceStore
    flow_repository: _PostgresFlowStore
    storage_repository: _PostgresStorageStore
    segment_repository: _PostgresSegmentStore
    object_repository: _PostgresObjectStore

    def __init__(
        self,
        *,
        storage_backend: StorageBackend,
        database_url: str | None = None,
        database_url_provider: Callable[[], str | None] | None = None,
        connection: Any | None = None,
        pool: ConnectionPool | None = None,
        pool_min_size: int = 1,
        pool_max_size: int = 10,
        register_storage_backend: bool = False,
    ) -> None:
        super().__init__(
            storage_backend=storage_backend,
            database_url=database_url,
            database_url_provider=database_url_provider,
            connection=connection,
            pool=pool,
            pool_min_size=pool_min_size,
            pool_max_size=pool_max_size,
        )
        self.service_repository = _PostgresServiceStore(self)
        self.profile_repository = _PostgresProfileStore(self)
        self.webhook_repository = _PostgresWebhookStore(self)
        self.deletion_repository = _PostgresDeletionStore(self)
        self.source_repository = _PostgresSourceStore(self)
        self.flow_repository = _PostgresFlowStore(self)
        self.storage_repository = _PostgresStorageStore(self)
        self.segment_repository = _PostgresSegmentStore(self)
        self.object_repository = _PostgresObjectStore(self)
        if register_storage_backend:
            self.storage_repository._upsert_configured_storage_backend(storage_backend)
