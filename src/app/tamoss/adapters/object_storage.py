from __future__ import annotations

from threading import RLock
from typing import Any
from urllib.parse import quote
from uuid import UUID

import boto3
from botocore.config import Config
from botocore.exceptions import ClientError

from tamoss.domain.model import StorageBackend
from tamoss.settings import Settings


class ConfiguredObjectStorage:
    def __init__(self, settings: Settings):
        self._settings = settings
        self._ensured_s3_buckets: set[tuple[UUID, str]] = set()
        self._s3_clients: dict[tuple[UUID, bool, str], Any] = {}
        self._lock = RLock()

    def build_put_request(
        self, *, object_id: str, flow_container: str, backend: StorageBackend
    ) -> dict:
        content_type = _container_content_type(flow_container)
        self._ensure_s3_bucket(backend)
        return {
            "url": self._presign_put_url(
                backend=backend,
                object_id=object_id,
                content_type=content_type,
            ),
            "content-type": content_type,
            "headers": {"Content-Type": content_type},
        }

    def build_get_url(self, *, object_id: str, backend: StorageBackend) -> str:
        return _public_object_url(backend=backend, object_id=object_id)

    def build_get_urls(self, *, object_id: str, backend: StorageBackend) -> list[dict]:
        return [
            {
                "url": self._presign_get_url(
                    backend=backend,
                    object_id=object_id,
                ),
                "label": backend.label,
                "presigned": True,
            },
        ]

    def write(
        self, object_id: str, data: bytes, *, backend: StorageBackend | None = None
    ) -> None:
        resolved_backend = _configured_backend(backend, self._settings)
        self._ensure_s3_bucket(resolved_backend)
        self._s3_client(resolved_backend).put_object(
            Bucket=_require_bucket(resolved_backend),
            Key=object_id,
            Body=data,
        )

    def read(
        self, object_id: str, *, backend: StorageBackend | None = None
    ) -> bytes | None:
        resolved_backend = _configured_backend(backend, self._settings)
        try:
            response = self._s3_client(resolved_backend).get_object(
                Bucket=_require_bucket(resolved_backend),
                Key=object_id,
            )
        except ClientError as exc:
            if _client_error_code(exc) in {"404", "NoSuchKey"}:
                return None
            raise
        return response["Body"].read()

    def delete(self, object_id: str, *, backend: StorageBackend | None = None) -> None:
        resolved_backend = _configured_backend(backend, self._settings)
        self._s3_client(resolved_backend).delete_object(
            Bucket=_require_bucket(resolved_backend),
            Key=object_id,
        )

    def _presign_put_url(
        self, *, backend: StorageBackend, object_id: str, content_type: str
    ) -> str:
        return self._s3_presign_client(backend).generate_presigned_url(
            "put_object",
            Params={
                "Bucket": _require_bucket(backend),
                "Key": object_id,
                "ContentType": content_type,
            },
            ExpiresIn=self._settings.s3_presign_ttl_seconds,
        )

    def _presign_get_url(self, *, backend: StorageBackend, object_id: str) -> str:
        return self._s3_presign_client(backend).generate_presigned_url(
            "get_object",
            Params={
                "Bucket": _require_bucket(backend),
                "Key": object_id,
            },
            ExpiresIn=self._settings.s3_presign_ttl_seconds,
        )

    def _s3_client(self, backend: StorageBackend):
        return self._cached_s3_client(backend=backend, public=False)

    def _s3_presign_client(self, backend: StorageBackend):
        return self._cached_s3_client(backend=backend, public=True)

    def _cached_s3_client(self, *, backend: StorageBackend, public: bool):
        endpoint_url = (
            backend.public_endpoint_url if public else backend.endpoint_url
        ) or backend.endpoint_url
        cache_key = (backend.id, public, endpoint_url or "")
        with self._lock:
            client = self._s3_clients.get(cache_key)
            if client is None:
                client = self._new_s3_client(backend=backend, public=public)
                self._s3_clients[cache_key] = client
            return client

    def _ensure_s3_bucket(self, backend: StorageBackend) -> None:
        if not self._settings.s3_auto_create_bucket:
            return
        bucket = _require_bucket(backend)
        cache_key = (backend.id, bucket)
        with self._lock:
            if cache_key in self._ensured_s3_buckets:
                return

        client = self._s3_client(backend)
        try:
            client.head_bucket(Bucket=bucket)
        except ClientError as exc:
            if not _is_missing_bucket_error(exc):
                raise
            try:
                client.create_bucket(**_create_bucket_kwargs(backend, bucket))
            except ClientError as create_exc:
                if _client_error_code(create_exc) not in {
                    "BucketAlreadyExists",
                    "BucketAlreadyOwnedByYou",
                }:
                    raise

        with self._lock:
            self._ensured_s3_buckets.add(cache_key)

    def _new_s3_client(self, *, backend: StorageBackend, public: bool):
        endpoint_url = (
            backend.public_endpoint_url if public else backend.endpoint_url
        ) or backend.endpoint_url
        if not endpoint_url or not backend.access_key or not backend.secret_key:
            raise RuntimeError(
                f"Storage backend {backend.id} is missing S3 endpoint or credentials"
            )
        return boto3.client(
            "s3",
            endpoint_url=endpoint_url,
            aws_access_key_id=backend.access_key,
            aws_secret_access_key=backend.secret_key,
            region_name=backend.region or "us-east-1",
            config=Config(
                signature_version="s3v4",
                connect_timeout=self._settings.s3_connect_timeout_seconds,
                read_timeout=self._settings.s3_read_timeout_seconds,
                retries={"max_attempts": 2, "mode": "standard"},
                max_pool_connections=self._settings.s3_max_pool_connections,
                s3={"addressing_style": "path"},
            ),
        )


def _configured_backend(
    backend: StorageBackend | None, settings: Settings
) -> StorageBackend:
    if backend is not None:
        return backend
    configured = settings.storage_backend
    if configured is None:
        raise RuntimeError("S3 storage backend is not configured")
    return StorageBackend(
        id=configured.id,
        label=configured.label,
        provider=configured.provider,
        region=configured.region,
        store_product=configured.store_product,
        store_type=configured.store_type,
        default_storage=configured.default_storage,
        bucket_name=configured.bucket_name,
        endpoint_url=configured.endpoint_url,
        public_endpoint_url=configured.public_endpoint_url,
        access_key=configured.access_key,
        secret_key=configured.secret_key,
    )


def _require_bucket(backend: StorageBackend) -> str:
    if not backend.bucket_name:
        raise RuntimeError(f"Storage backend {backend.id} is missing bucket_name")
    return backend.bucket_name


def _public_object_url(*, backend: StorageBackend, object_id: str) -> str:
    endpoint = backend.public_endpoint_url or backend.endpoint_url
    if not endpoint:
        raise RuntimeError(f"Storage backend {backend.id} is missing public endpoint")
    bucket = quote(_require_bucket(backend), safe="")
    key = quote(object_id, safe="/")
    return f"{endpoint.rstrip('/')}/{bucket}/{key}"


def _client_error_code(exc: ClientError) -> str:
    return str(exc.response.get("Error", {}).get("Code", ""))


def _is_missing_bucket_error(exc: ClientError) -> bool:
    status_code = exc.response.get("ResponseMetadata", {}).get("HTTPStatusCode")
    return status_code == 404 or _client_error_code(exc) in {
        "404",
        "NoSuchBucket",
        "NotFound",
    }


def _create_bucket_kwargs(backend: StorageBackend, bucket: str) -> dict:
    kwargs: dict = {"Bucket": bucket}
    region = backend.region or "us-east-1"
    if region != "us-east-1":
        kwargs["CreateBucketConfiguration"] = {
            "LocationConstraint": region,
        }
    return kwargs


def _container_content_type(container: str) -> str:
    lowered = container.lower()
    if "/" in container:
        return container
    if "wave" in lowered or "wav" in lowered:
        return "audio/wav"
    if "mp4" in lowered:
        return "video/mp4"
    if "mxf" in lowered:
        return "application/mxf"
    return "video/mp2t"
