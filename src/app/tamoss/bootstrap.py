from __future__ import annotations

from tamoss.adapters.object_storage import ConfiguredObjectStorage
from tamoss.adapters.postgres import PostgresRepository
from tamoss.application.use_cases import TamossUseCases
from tamoss.settings import Settings, get_settings


class StartupConfigurationError(RuntimeError):
    """Fatal startup configuration issue."""


def create_use_cases(settings: Settings | None = None) -> TamossUseCases:
    resolved_settings = settings or get_settings()
    storage_backend = resolved_settings.storage_backend_record()
    if storage_backend is None:
        raise StartupConfigurationError("S3 storage backend must be configured")
    database_url = resolved_settings.database_url_value()
    if database_url is None:
        raise StartupConfigurationError("POSTGRES_HOST is required")
    object_storage = ConfiguredObjectStorage(resolved_settings)
    repository = PostgresRepository(
        database_url=database_url,
        database_url_provider=resolved_settings.database_url_value,
        storage_backend=storage_backend,
        pool_min_size=resolved_settings.database_pool_min_size,
        pool_max_size=resolved_settings.database_pool_max_size,
        register_storage_backend=resolved_settings.storage_backend_registration_enabled,
    )
    return TamossUseCases(
        repository=repository,
        object_storage=object_storage,
        settings=resolved_settings,
    )
