from __future__ import annotations

from tamoss.adapters.object_storage import ConfiguredObjectStorage
from tamoss.adapters.postgres import PostgresRepository
from tamoss.application.use_cases import TamossUseCases, storage_backend_from_settings
from tamoss.settings import Settings, get_settings


def create_use_cases(settings: Settings | None = None) -> TamossUseCases:
    resolved_settings = settings or get_settings()
    storage_backend = storage_backend_from_settings(resolved_settings)
    if storage_backend is None:
        raise RuntimeError("S3 storage backend must be configured")
    if resolved_settings.database_url is None:
        raise RuntimeError("TAMOSS_DATABASE_URL or POSTGRES_HOST is required")
    object_storage = ConfiguredObjectStorage(resolved_settings)
    return TamossUseCases(
        repository=PostgresRepository(
            database_url=resolved_settings.database_url,
            storage_backend=storage_backend,
            pool_min_size=resolved_settings.database_pool_min_size,
            pool_max_size=resolved_settings.database_pool_max_size,
        ),
        object_storage=object_storage,
        settings=resolved_settings,
    )
