from __future__ import annotations

import os
from functools import lru_cache
from typing import overload
from uuid import UUID

from pydantic import BaseModel, Field, field_validator

DEFAULT_TAMOSS_S3_STORAGE_BACKEND_ID = UUID("f1ab5b54-9703-42ed-b181-11ba1c794a7f")


def _api_token_from_env() -> str | None:
    return _env_str("TAMOSS_API_TOKEN")


def _database_url_from_env() -> str | None:
    explicit = _env_str("TAMOSS_DATABASE_URL") or _env_str("DATABASE_URL")
    if explicit:
        return explicit
    host = _env_str("POSTGRES_HOST")
    if not host:
        return None
    database = _env_str("POSTGRES_DB", "tams")
    username = _env_str("POSTGRES_USER", "tams")
    password = _database_password()
    port = _env_str("POSTGRES_PORT", "5432")
    return f"postgresql://{username}:{password}@{host}:{port}/{database}"


def _s3_presign_ttl_seconds() -> int:
    return _env_int(
        "TAMOSS_S3_PRESIGN_TTL",
        3600,
    )


class StorageBackendSettings(BaseModel):
    id: UUID = DEFAULT_TAMOSS_S3_STORAGE_BACKEND_ID
    label: str = "tamoss.storage.primary"
    provider: str = "tamoss"
    region: str = "us-east-1"
    store_product: str = "s3"
    store_type: str = "http_object_store"
    default_storage: bool = True
    bucket_name: str | None = None
    endpoint_url: str | None = None
    public_endpoint_url: str | None = None
    access_key: str | None = None
    secret_key: str | None = None


def _s3_backend_from_env() -> StorageBackendSettings | None:
    access_key = _env_str(
        "TAMOSS_S3_ACCESS_KEY",
    )
    secret_key = _env_str(
        "TAMOSS_S3_SECRET_KEY",
    )
    if not access_key or not secret_key:
        return None

    region = _env_str(
        "TAMOSS_S3_REGION",
        "us-east-1",
    )
    bucket_name = _env_str(
        "TAMOSS_S3_BUCKET",
        "tamoss",
    )
    endpoint_url = _env_str(
        "TAMOSS_S3_ENDPOINT",
        "http://localhost:9000",
    )
    public_endpoint_url = _env_str(
        "TAMOSS_S3_PUBLIC_ENDPOINT",
        endpoint_url,
    )
    label = _env_str(
        "TAMOSS_STORAGE_LABEL",
        f"tamoss.{region}:s3:{bucket_name}",
    )
    storage_backend_id = UUID(
        _env_str(
            "TAMOSS_STORAGE_BACKEND_ID",
            str(DEFAULT_TAMOSS_S3_STORAGE_BACKEND_ID),
        )
    )
    return StorageBackendSettings(
        id=storage_backend_id,
        label=label,
        provider=_env_str(
            "TAMOSS_STORAGE_PROVIDER",
            "tamoss",
        ),
        region=region,
        store_product="s3",
        store_type="http_object_store",
        default_storage=True,
        bucket_name=bucket_name,
        endpoint_url=endpoint_url,
        public_endpoint_url=public_endpoint_url,
        access_key=access_key,
        secret_key=secret_key,
    )


class Settings(BaseModel):
    service_name: str = "TAMOSS"
    service_description: str = "Time Addressable Media Open Source Store"
    api_version: str = "8.0"
    service_version: str = "tamoss-dev"
    public_base_url: str = "http://testserver"
    auth_required: bool = Field(
        default_factory=lambda: _env_bool("TAMOSS_AUTH_REQUIRED")
    )
    api_token: str | None = Field(default_factory=_api_token_from_env)
    basic_auth_username: str = Field(
        default_factory=lambda: (
            _env_str("TAMOSS_BASIC_AUTH_USERNAME", "tamoss") or "tamoss"
        )
    )
    basic_auth_password: str | None = Field(
        default_factory=lambda: (
            _env_str("TAMOSS_BASIC_AUTH_PASSWORD") or _api_token_from_env()
        )
    )
    trust_forward_auth_headers: bool = Field(
        default_factory=lambda: _env_bool("TAMOSS_TRUST_FORWARD_AUTH_HEADERS")
    )
    oauth2_enabled: bool = Field(
        default_factory=lambda: _env_bool("TAMOSS_OAUTH2_ENABLED")
    )
    oauth2_issuer: str | None = Field(
        default_factory=lambda: _env_str("TAMOSS_OAUTH2_ISSUER")
    )
    oauth2_jwks_uri: str | None = Field(
        default_factory=lambda: _env_str("TAMOSS_OAUTH2_JWKS_URI")
    )
    oauth2_audience: str | None = Field(
        default_factory=lambda: _env_str("TAMOSS_OAUTH2_AUDIENCE")
    )
    oauth2_algorithms: list[str] = Field(
        default_factory=lambda: _env_csv("TAMOSS_OAUTH2_ALGORITHMS") or ["RS256"]
    )
    oauth2_required_scopes: list[str] = Field(
        default_factory=lambda: _env_csv("TAMOSS_OAUTH2_REQUIRED_SCOPES")
    )
    oauth2_jwks_cache_seconds: int = Field(
        default_factory=lambda: _env_int("TAMOSS_OAUTH2_JWKS_CACHE_SECONDS", 300)
    )
    oauth2_jwks_timeout_seconds: float = Field(
        default_factory=lambda: _env_float("TAMOSS_OAUTH2_JWKS_TIMEOUT_SECONDS", 5.0)
    )
    database_url: str | None = Field(default_factory=_database_url_from_env)
    database_pool_min_size: int = Field(
        default_factory=lambda: _env_int("TAMOSS_DATABASE_POOL_MIN_SIZE", 1)
    )
    database_pool_max_size: int = Field(
        default_factory=lambda: _env_int("TAMOSS_DATABASE_POOL_MAX_SIZE", 10)
    )
    min_object_timeout: str = "0:300"
    min_presigned_url_timeout: str = "0:30"
    s3_presign_ttl_seconds: int = Field(default_factory=_s3_presign_ttl_seconds)
    s3_connect_timeout_seconds: float = Field(
        default_factory=lambda: _env_float(
            "TAMOSS_S3_CONNECT_TIMEOUT_SECONDS",
            1.0,
        )
    )
    s3_read_timeout_seconds: float = Field(
        default_factory=lambda: _env_float(
            "TAMOSS_S3_READ_TIMEOUT_SECONDS",
            2.0,
        )
    )
    s3_max_pool_connections: int = Field(
        default_factory=lambda: _env_int(
            "TAMOSS_S3_MAX_POOL_CONNECTIONS",
            40,
        )
    )
    s3_auto_create_bucket: bool = Field(
        default_factory=lambda: _env_bool("TAMOSS_S3_AUTO_CREATE_BUCKET")
    )
    storage_backend: StorageBackendSettings | None = Field(
        default_factory=_s3_backend_from_env
    )

    @field_validator("storage_backend")
    @classmethod
    def validate_storage_backend(
        cls, value: StorageBackendSettings | None
    ) -> StorageBackendSettings | None:
        if value is None:
            return None
        if value.store_product.lower() != "s3":
            raise ValueError("storage backend must be S3")
        return value.model_copy(update={"store_product": "s3", "default_storage": True})

    @field_validator("database_pool_min_size", "database_pool_max_size")
    @classmethod
    def validate_database_pool_size(cls, value: int) -> int:
        if value < 1:
            raise ValueError("database pool size must be greater than zero")
        return value


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    return Settings()


def _database_password() -> str:
    password = _env_str("POSTGRES_PASSWORD", "tams") or "tams"
    password_file = _env_str("TAMOSS_DB_PASSWORD_FILE")
    for candidate in [password_file, "/run/secrets/db_password", ".local/db_password"]:
        if not candidate:
            continue
        try:
            with open(candidate, encoding="utf-8") as handle:
                return handle.read().strip()
        except OSError:
            continue
    return password


@overload
def _env_str(name: str) -> str | None:
    pass


@overload
def _env_str(name: str, default: str) -> str:
    pass


def _env_str(name: str, default: str | None = None) -> str | None:
    raw = os.getenv(name)
    return default if raw is None else raw


def _env_bool(name: str, default: bool = False) -> bool:
    raw = _env_str(name)
    if raw is None:
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


def _env_csv(name: str) -> list[str]:
    raw = _env_str(name) or ""
    return [item.strip() for item in raw.split(",") if item.strip()]


def _env_int(name: str, default: int) -> int:
    raw = _env_str(name)
    if raw is None:
        return default
    try:
        value = int(raw)
    except ValueError:
        return default
    return value if value > 0 else default


def _env_float(name: str, default: float) -> float:
    raw = _env_str(name)
    if raw is None:
        return default
    try:
        value = float(raw)
    except ValueError:
        return default
    return value if value > 0 else default
