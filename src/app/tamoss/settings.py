from __future__ import annotations

import os
import socket
from functools import lru_cache
from pathlib import Path
from typing import Annotated, overload
from urllib.parse import quote, urlparse
from uuid import UUID

from mediatimestamp import Timestamp
from pydantic import (
    BaseModel,
    Field,
    ValidationInfo,
    field_validator,
    model_validator,
)
from pydantic_settings import BaseSettings, NoDecode, SettingsConfigDict

from tamoss.domain.model import StorageBackend
from tamoss.version import BBC_TAMS_API_VERSION, TAMOSS_VERSION

DEFAULT_TAMOSS_S3_STORAGE_BACKEND_ID = UUID("f1ab5b54-9703-42ed-b181-11ba1c794a7f")
DEFAULT_WORKER_ID = "tamoss-worker"
DEFAULT_WORKER_LEASE_SECONDS = 300
NANOSECONDS_PER_SECOND = 1_000_000_000
MIN_OBJECT_TIMEOUT_SECONDS = 300
MIN_PRESIGNED_URL_TIMEOUT_SECONDS = 30

_OAUTH2_JWT_ALGORITHMS = frozenset(
    {
        "RS256",
        "RS384",
        "RS512",
        "PS256",
        "PS384",
        "PS512",
        "ES256",
        "ES384",
        "ES512",
        "EdDSA",
    }
)


class SecretFileError(ValueError):
    pass


def _worker_id_from_env() -> str:
    return _env_str("TAMOSS_WORKER_ID") or f"{socket.gethostname()}:{os.getpid()}"


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

    def to_storage_backend(self) -> StorageBackend:
        return StorageBackend(
            id=self.id,
            label=self.label,
            provider=self.provider,
            region=self.region,
            store_product=self.store_product,
            store_type=self.store_type,
            default_storage=self.default_storage,
            bucket_name=self.bucket_name,
            endpoint_url=self.endpoint_url,
            public_endpoint_url=self.public_endpoint_url,
            access_key=self.access_key,
            secret_key=self.secret_key,
        )


def _s3_backend_from_env() -> StorageBackendSettings | None:
    endpoint_url = _env_url("TAMOSS_S3_ENDPOINT", "http://localhost:9000")
    public_endpoint_url = _env_url("TAMOSS_S3_PUBLIC_ENDPOINT", endpoint_url)
    access_key = _env_str("TAMOSS_S3_ACCESS_KEY")
    secret_key = _env_str("TAMOSS_S3_SECRET_KEY")
    if not access_key or not secret_key:
        return None

    region = _env_str("TAMOSS_S3_REGION", "us-east-1")
    bucket_name = _env_str("TAMOSS_S3_BUCKET", "tamoss")
    label = _env_str("TAMOSS_STORAGE_LABEL", f"tamoss.{region}:s3:{bucket_name}")
    storage_backend_id = UUID(
        _env_str(
            "TAMOSS_STORAGE_BACKEND_ID",
            str(DEFAULT_TAMOSS_S3_STORAGE_BACKEND_ID),
        )
    )
    return StorageBackendSettings(
        id=storage_backend_id,
        label=label,
        provider=_env_str("TAMOSS_STORAGE_PROVIDER", "tamoss"),
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


class Settings(BaseSettings):
    model_config = SettingsConfigDict(extra="ignore", populate_by_name=True)

    service_name: str = "TAMOSS"
    service_description: str = "Time Addressable Media Open Source Store"
    tamoss_version: str = Field(
        default=TAMOSS_VERSION,
        validation_alias="TAMOSS_VERSION",
    )
    api_version: str = Field(
        default=BBC_TAMS_API_VERSION,
        validation_alias="TAMOSS_TAMS_API_VERSION",
    )
    service_version: str = Field(
        default=TAMOSS_VERSION,
        validation_alias="TAMOSS_SERVICE_VERSION",
    )
    auth_required: bool = Field(
        default=False,
        validation_alias="TAMOSS_AUTH_REQUIRED",
    )
    api_token: str | None = Field(
        default=None,
        validation_alias="TAMOSS_API_TOKEN",
    )
    api_token_file: str | None = Field(
        default=None,
        validation_alias="TAMOSS_API_TOKEN_FILE",
    )
    basic_auth_username: str = Field(
        default="tamoss",
        validation_alias="TAMOSS_BASIC_AUTH_USERNAME",
    )
    basic_auth_password: str | None = Field(
        default=None,
        validation_alias="TAMOSS_BASIC_AUTH_PASSWORD",
    )
    basic_auth_password_file: str | None = Field(
        default=None,
        validation_alias="TAMOSS_BASIC_AUTH_PASSWORD_FILE",
    )
    trust_forward_auth_headers: bool = Field(
        default=False,
        validation_alias="TAMOSS_TRUST_FORWARD_AUTH_HEADERS",
    )
    forward_auth_shared_secret: str | None = Field(
        default=None,
        validation_alias="TAMOSS_FORWARD_AUTH_SHARED_SECRET",
    )
    forward_auth_shared_secret_file: str | None = Field(
        default=None,
        validation_alias="TAMOSS_FORWARD_AUTH_SHARED_SECRET_FILE",
    )
    oauth2_enabled: bool = Field(
        default=False,
        validation_alias="TAMOSS_OAUTH2_ENABLED",
    )
    oauth2_issuer: str | None = Field(
        default=None,
        validation_alias="TAMOSS_OAUTH2_ISSUER",
    )
    oauth2_jwks_uri: str | None = Field(
        default=None,
        validation_alias="TAMOSS_OAUTH2_JWKS_URI",
    )
    oauth2_audience: str | None = Field(
        default=None,
        validation_alias="TAMOSS_OAUTH2_AUDIENCE",
    )
    oauth2_algorithms: Annotated[list[str], NoDecode] = Field(
        default_factory=list,
        validation_alias="TAMOSS_OAUTH2_ALGORITHMS",
    )
    oauth2_allow_unscoped_full_access: bool = Field(
        default=True,
        validation_alias="TAMOSS_OAUTH2_ALLOW_UNSCOPED_FULL_ACCESS",
    )
    oauth2_admin_scope: str = Field(
        default="tams-api/admin",
        validation_alias="TAMOSS_OAUTH2_ADMIN_SCOPE",
    )
    oauth2_read_scope: str = Field(
        default="tams-api/read",
        validation_alias="TAMOSS_OAUTH2_READ_SCOPE",
    )
    oauth2_write_scope: str = Field(
        default="tams-api/write",
        validation_alias="TAMOSS_OAUTH2_WRITE_SCOPE",
    )
    oauth2_delete_scope: str = Field(
        default="tams-api/delete",
        validation_alias="TAMOSS_OAUTH2_DELETE_SCOPE",
    )
    oauth2_jwks_cache_seconds: int = Field(
        default=300,
        validation_alias="TAMOSS_OAUTH2_JWKS_CACHE_SECONDS",
    )
    oauth2_jwks_timeout_seconds: float = Field(
        default=5.0,
        validation_alias="TAMOSS_OAUTH2_JWKS_TIMEOUT_SECONDS",
    )
    database_pool_min_size: int = Field(
        default=1,
        validation_alias="TAMOSS_DATABASE_POOL_MIN_SIZE",
    )
    database_pool_max_size: int = Field(
        default=20,
        validation_alias="TAMOSS_DATABASE_POOL_MAX_SIZE",
    )
    # Sync route handlers and threaded auth share the anyio thread pool, so
    # this is the per-process request concurrency ceiling. Size it together
    # with database_pool_max_size and PostgreSQL max_connections.
    api_thread_pool_tokens: int = Field(
        default=80,
        ge=8,
        validation_alias="TAMOSS_API_THREAD_POOL_TOKENS",
    )
    readiness_cache_ttl_seconds: float = Field(
        default=2.0,
        ge=0.0,
        validation_alias="TAMOSS_READINESS_CACHE_TTL_SECONDS",
    )
    min_object_timeout: str = Field(
        default="300:0",
        validation_alias="TAMOSS_MIN_OBJECT_TIMEOUT",
    )
    min_presigned_url_timeout: str = Field(
        default="30:0",
        validation_alias="TAMOSS_MIN_PRESIGNED_URL_TIMEOUT",
    )
    s3_presign_ttl_seconds: int = Field(
        default=3600,
        validation_alias="TAMOSS_S3_PRESIGN_TTL",
    )
    s3_connect_timeout_seconds: float = Field(
        default=1.0,
        validation_alias="TAMOSS_S3_CONNECT_TIMEOUT_SECONDS",
    )
    s3_read_timeout_seconds: float = Field(
        default=2.0,
        validation_alias="TAMOSS_S3_READ_TIMEOUT_SECONDS",
    )
    s3_max_pool_connections: int = Field(
        default=40,
        validation_alias="TAMOSS_S3_MAX_POOL_CONNECTIONS",
    )
    storage_backend_credentials_file: str | None = Field(
        default=None,
        validation_alias="TAMOSS_STORAGE_BACKEND_CREDENTIALS_FILE",
    )
    storage_backend_registration_enabled: bool = Field(
        default=False,
        validation_alias="TAMOSS_STORAGE_BACKEND_REGISTRATION_ENABLED",
    )
    storage_backend: StorageBackendSettings | None = Field(
        default_factory=_s3_backend_from_env
    )
    log_level: str = Field(
        default="INFO",
        validation_alias="TAMOSS_LOG_LEVEL",
    )
    worker_poll_interval_seconds: int = Field(
        default=5,
        validation_alias="TAMOSS_WORKER_POLL_INTERVAL_SECONDS",
    )
    worker_max_requests: int = Field(
        default=50,
        validation_alias="TAMOSS_WORKER_MAX_REQUESTS",
    )
    worker_lease_seconds: int = Field(
        default=DEFAULT_WORKER_LEASE_SECONDS,
        validation_alias="TAMOSS_WORKER_LEASE_SECONDS",
    )
    worker_id: str = Field(
        default_factory=_worker_id_from_env,
        validation_alias="TAMOSS_WORKER_ID",
    )
    worker_enable_delete: bool = Field(
        default=True,
        validation_alias="TAMOSS_WORKER_ENABLE_DELETE",
    )
    worker_enable_webhook: bool = Field(
        default=True,
        validation_alias="TAMOSS_WORKER_ENABLE_WEBHOOK",
    )
    webhook_timeout_seconds: float = Field(
        default=30.0,
        validation_alias="TAMOSS_WEBHOOK_TIMEOUT_SECONDS",
    )
    webhook_max_attempts: int = Field(
        default=5,
        validation_alias="TAMOSS_WORKER_MAX_ATTEMPTS",
    )
    webhook_delivery_concurrency: int = Field(
        default=8,
        ge=1,
        validation_alias="TAMOSS_WEBHOOK_DELIVERY_CONCURRENCY",
    )
    # Retention for terminal queue rows (done webhook deliveries, delete
    # requests, cleanups, copies); 0 disables purging.
    worker_queue_retention_seconds: int = Field(
        default=7 * 24 * 60 * 60,
        ge=0,
        validation_alias="TAMOSS_WORKER_QUEUE_RETENTION_SECONDS",
    )
    webhook_allow_private_targets: bool = Field(
        default=False,
        validation_alias="TAMOSS_WEBHOOK_ALLOW_PRIVATE_TARGETS",
    )
    webhook_allowed_hosts: Annotated[list[str], NoDecode] = Field(
        default_factory=list,
        validation_alias="TAMOSS_WEBHOOK_ALLOWED_HOSTS",
    )
    storage_allocation_max_objects: int = Field(
        default=1000,
        validation_alias="TAMOSS_STORAGE_ALLOCATION_MAX_OBJECTS",
    )
    storage_object_id_max_length: int = Field(
        default=1024,
        validation_alias="TAMOSS_STORAGE_OBJECT_ID_MAX_LENGTH",
    )

    @field_validator(
        "api_token",
        "api_token_file",
        "basic_auth_password",
        "basic_auth_password_file",
        "forward_auth_shared_secret",
        "forward_auth_shared_secret_file",
        "oauth2_audience",
        "storage_backend_credentials_file",
        mode="before",
    )
    @classmethod
    def blank_strings_are_unset(cls, value: object) -> object:
        if isinstance(value, str) and not value.strip():
            return None
        return value

    @field_validator("oauth2_issuer", "oauth2_jwks_uri", mode="before")
    @classmethod
    def validate_optional_url_env(cls, value: object, info: ValidationInfo) -> object:
        if value is None or (isinstance(value, str) and not value.strip()):
            return None
        assert info.field_name is not None
        setting_name = {
            "oauth2_issuer": "TAMOSS_OAUTH2_ISSUER",
            "oauth2_jwks_uri": "TAMOSS_OAUTH2_JWKS_URI",
        }[info.field_name]
        return _require_url(setting_name, str(value))

    @field_validator(
        "oauth2_algorithms",
        "webhook_allowed_hosts",
        mode="before",
    )
    @classmethod
    def csv_env_values(cls, value: object) -> object:
        if value is None:
            return []
        if isinstance(value, str):
            return [item.strip() for item in value.split(",") if item.strip()]
        return value

    @field_validator(
        "oauth2_admin_scope",
        "oauth2_read_scope",
        "oauth2_write_scope",
        "oauth2_delete_scope",
        mode="before",
    )
    @classmethod
    def validate_oauth2_api_scope_name(cls, value: object) -> object:
        if isinstance(value, str):
            scope_name = value.strip()
            if not scope_name:
                raise ValueError("OAuth2 API scope names must not be blank")
            return scope_name
        return value

    @field_validator(
        "database_pool_min_size",
        "database_pool_max_size",
        "worker_poll_interval_seconds",
        "worker_max_requests",
        "worker_lease_seconds",
        "webhook_max_attempts",
        "storage_allocation_max_objects",
        "storage_object_id_max_length",
        "s3_presign_ttl_seconds",
        "s3_max_pool_connections",
        mode="before",
    )
    @classmethod
    def parse_positive_int_env(cls, value: object) -> object:
        if isinstance(value, str):
            try:
                value = int(value)
            except ValueError as exc:
                raise ValueError("value must be a positive integer") from exc
        if isinstance(value, int) and not isinstance(value, bool) and value >= 1:
            return value
        if isinstance(value, int):
            raise ValueError("value must be a positive integer")
        return value

    @field_validator(
        "oauth2_jwks_timeout_seconds",
        "s3_connect_timeout_seconds",
        "s3_read_timeout_seconds",
        "webhook_timeout_seconds",
        mode="before",
    )
    @classmethod
    def parse_positive_float_env(cls, value: object) -> object:
        if isinstance(value, str):
            try:
                value = float(value)
            except ValueError as exc:
                raise ValueError("value must be a positive number") from exc
        if isinstance(value, int | float) and not isinstance(value, bool) and value > 0:
            return value
        if isinstance(value, int | float):
            raise ValueError("value must be a positive number")
        return value

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

    @field_validator("oauth2_algorithms")
    @classmethod
    def validate_oauth2_algorithms(cls, value: list[str]) -> list[str]:
        algorithms = [item.strip() for item in value if item.strip()]
        invalid = sorted(set(algorithms) - _OAUTH2_JWT_ALGORITHMS)
        if invalid:
            invalid_text = ", ".join(invalid)
            raise ValueError(f"unsupported OAuth2 JWT algorithm(s): {invalid_text}")
        return algorithms or ["RS256"]

    @model_validator(mode="after")
    def validate_forward_auth_boundary(self) -> Settings:
        if (
            self.trust_forward_auth_headers
            and not self.forward_auth_shared_secret_value()
        ):
            raise ValueError(
                "TAMOSS_TRUST_FORWARD_AUTH_HEADERS requires "
                "TAMOSS_FORWARD_AUTH_SHARED_SECRET or "
                "TAMOSS_FORWARD_AUTH_SHARED_SECRET_FILE"
            )
        return self

    @model_validator(mode="after")
    def validate_timeout_contract(self) -> Settings:
        min_object_timeout = _timestamp_duration_seconds(
            self.min_object_timeout,
            setting_name="min_object_timeout",
        )
        min_presigned_url_timeout = _timestamp_duration_seconds(
            self.min_presigned_url_timeout,
            setting_name="min_presigned_url_timeout",
        )
        if min_object_timeout < MIN_OBJECT_TIMEOUT_SECONDS:
            raise ValueError("min_object_timeout must be at least 300:0")
        if min_presigned_url_timeout < MIN_PRESIGNED_URL_TIMEOUT_SECONDS:
            raise ValueError("min_presigned_url_timeout must be at least 30:0")
        if min_presigned_url_timeout > min_object_timeout:
            raise ValueError(
                "min_presigned_url_timeout must be less than or equal to "
                "min_object_timeout"
            )
        if self.s3_presign_ttl_seconds < min_presigned_url_timeout:
            raise ValueError(
                "TAMOSS_S3_PRESIGN_TTL must be greater than or equal to "
                "min_presigned_url_timeout"
            )
        return self

    def min_object_timeout_seconds(self) -> int:
        return _timestamp_duration_seconds(
            self.min_object_timeout,
            setting_name="min_object_timeout",
        )

    def presigned_put_ttl_seconds(self) -> int:
        return min(self.s3_presign_ttl_seconds, self.min_object_timeout_seconds())

    def api_token_value(self) -> str | None:
        return (
            read_secret_file(
                self.api_token_file,
                required=bool(self.api_token_file),
                setting_name="TAMOSS_API_TOKEN_FILE",
            )
            or self.api_token
        )

    def basic_auth_password_value(self) -> str | None:
        return (
            read_secret_file(
                self.basic_auth_password_file,
                required=bool(self.basic_auth_password_file),
                setting_name="TAMOSS_BASIC_AUTH_PASSWORD_FILE",
            )
            or self.basic_auth_password
        )

    def forward_auth_shared_secret_value(self) -> str | None:
        return (
            read_secret_file(
                self.forward_auth_shared_secret_file,
                required=bool(self.forward_auth_shared_secret_file),
                setting_name="TAMOSS_FORWARD_AUTH_SHARED_SECRET_FILE",
            )
            or self.forward_auth_shared_secret
        )

    def database_url_value(self) -> str | None:
        host = _env_str("POSTGRES_HOST")
        if not host:
            return None
        return _postgresql_url(
            _env_str("POSTGRES_USER", "tams"),
            _env_str("POSTGRES_PASSWORD", "tams"),
            host,
            _env_str("POSTGRES_PORT", "5432"),
            _env_str("POSTGRES_DB", "tams"),
        )

    def storage_backend_record(self) -> StorageBackend | None:
        storage_backend = self.storage_backend
        if storage_backend is None:
            return None
        return storage_backend.to_storage_backend()


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    return Settings()


def read_secret_file(
    path: str | None,
    *,
    required: bool = False,
    setting_name: str = "secret file",
) -> str | None:
    if not path:
        return None
    try:
        with Path(path).open(encoding="utf-8") as handle:
            return handle.read().strip()
    except OSError as exc:
        if required:
            raise SecretFileError(f"{setting_name} is not readable: {path}") from exc
        return None


def _postgresql_url(
    username: str, password: str, host: str, port: str, database: str
) -> str:
    encoded_username = quote(username, safe="")
    encoded_password = quote(password, safe="")
    encoded_database = quote(database, safe="")
    return (
        f"postgresql://{encoded_username}:{encoded_password}"
        f"@{host}:{port}/{encoded_database}"
    )


@overload
def _env_str(name: str) -> str | None:
    pass


@overload
def _env_str(name: str, default: str) -> str:
    pass


def _env_str(name: str, default: str | None = None) -> str | None:
    raw = os.getenv(name)
    return default if raw is None else raw


def _env_url(name: str, default: str | None = None) -> str | None:
    raw = _env_str(name)
    if raw is None or not raw.strip():
        return default
    return _require_url(name, raw)


def _require_url(name: str, value: str | None) -> str:
    if value is None:
        raise ValueError(f"{name} must be an absolute URL")
    candidate = value.strip()
    parsed = urlparse(candidate)
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        raise ValueError(f"{name} must be an absolute http(s) URL")
    return candidate


def _timestamp_duration_seconds(value: str, *, setting_name: str) -> int:
    try:
        nanoseconds = int(Timestamp.from_str(value).to_nanosec())
    except Exception as exc:
        raise ValueError(f"{setting_name} must be a valid TAMS timestamp") from exc
    if nanoseconds < 0:
        raise ValueError(f"{setting_name} must not be negative")
    seconds, remainder = divmod(nanoseconds, NANOSECONDS_PER_SECOND)
    if remainder:
        raise ValueError(f"{setting_name} must be a whole-second TAMS timestamp")
    return seconds
