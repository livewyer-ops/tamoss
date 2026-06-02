from __future__ import annotations

import hashlib
from collections.abc import Iterable
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import replace
from threading import RLock
from typing import Any
from urllib.parse import quote
from uuid import UUID

import boto3
from botocore.config import Config
from botocore.exceptions import ClientError

from tamoss.domain.model import (
    ObjectGetUrlBatchKey,
    ObjectGetUrlRequest,
    ObjectStorageMetadata,
    StorageBackend,
    utc_now,
)
from tamoss.errors import ConfigurationError
from tamoss.settings import Settings
from tamoss.storage_credentials import StorageBackendCredentialFile


class ConfiguredObjectStorage:
    def __init__(self, settings: Settings) -> None:
        self._settings = settings
        self._credentials = StorageBackendCredentialFile(
            settings.storage_backend_credentials_file
        )
        self._s3_clients: dict[tuple[UUID, bool, str, str], Any] = {}
        self._lock = RLock()

    def build_put_request(
        self, *, object_id: str, flow_container: str, backend: StorageBackend
    ) -> dict[str, object]:
        backend = self._resolve_backend(backend)
        return {
            "url": self._presign_put_url(
                backend=backend,
                object_id=object_id,
                content_type=flow_container,
            ),
            "content-type": flow_container,
            "headers": {"Content-Type": flow_container},
        }

    def build_get_urls(
        self, *, object_id: str, backend: StorageBackend
    ) -> list[dict[str, object]]:
        backend = self._resolve_backend(backend)
        return self._build_get_urls_for_resolved_backend(
            object_id=object_id,
            backend=backend,
        )

    def build_get_urls_batch(
        self, requests: Iterable[ObjectGetUrlRequest]
    ) -> dict[ObjectGetUrlBatchKey, list[dict[str, object]]]:
        unique_requests: dict[ObjectGetUrlBatchKey, ObjectGetUrlRequest] = {}
        for request in requests:
            backend = self._resolve_backend(request.backend)
            key = (backend.id, request.object_id)
            existing = unique_requests.get(key)
            if existing is None:
                unique_requests[key] = ObjectGetUrlRequest(
                    object_id=request.object_id,
                    backend=backend,
                    include_direct=request.include_direct,
                    include_presigned=request.include_presigned,
                )
                continue
            existing.include_direct = existing.include_direct or request.include_direct
            existing.include_presigned = (
                existing.include_presigned or request.include_presigned
            )
        if not unique_requests:
            return {}
        if len(unique_requests) == 1:
            key, request = next(iter(unique_requests.items()))
            return {
                key: self._build_get_urls_for_resolved_backend(
                    object_id=key[1],
                    backend=request.backend,
                    include_direct=request.include_direct,
                    include_presigned=request.include_presigned,
                )
            }

        max_workers = min(
            max(1, self._settings.s3_max_pool_connections), len(unique_requests)
        )
        results: dict[ObjectGetUrlBatchKey, list[dict[str, object]]] = {}
        with ThreadPoolExecutor(max_workers=max_workers) as executor:
            futures = {
                executor.submit(
                    self._build_get_urls_for_resolved_backend,
                    object_id=key[1],
                    backend=request.backend,
                    include_direct=request.include_direct,
                    include_presigned=request.include_presigned,
                ): key
                for key, request in unique_requests.items()
            }
            for future in as_completed(futures):
                results[futures[future]] = future.result()
        return results

    def _build_get_urls_for_resolved_backend(
        self,
        *,
        object_id: str,
        backend: StorageBackend,
        include_direct: bool = True,
        include_presigned: bool = True,
    ) -> list[dict[str, object]]:
        get_urls: list[dict[str, object]] = []
        if include_direct:
            get_urls.append(
                {
                    "url": _public_object_url(backend=backend, object_id=object_id),
                    "label": backend.label,
                    "presigned": False,
                }
            )
        if include_presigned:
            get_urls.append(
                {
                    "url": self._presign_get_url(
                        backend=backend,
                        object_id=object_id,
                    ),
                    "label": backend.label,
                    "presigned": True,
                }
            )
        return get_urls

    def write(
        self, object_id: str, data: bytes, *, backend: StorageBackend | None = None
    ) -> None:
        resolved_backend = self._resolve_backend(backend)
        self._s3_client(resolved_backend).put_object(
            Bucket=_require_bucket(resolved_backend),
            Key=object_id,
            Body=data,
        )

    def read(
        self, object_id: str, *, backend: StorageBackend | None = None
    ) -> bytes | None:
        resolved_backend = self._resolve_backend(backend)
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

    def object_metadata(
        self, object_id: str, *, backend: StorageBackend | None = None
    ) -> ObjectStorageMetadata | None:
        resolved_backend = self._resolve_backend(backend)
        try:
            response = self._s3_client(resolved_backend).head_object(
                Bucket=_require_bucket(resolved_backend),
                Key=object_id,
            )
        except ClientError as exc:
            if _client_error_code(exc) in {"404", "NoSuchKey", "NotFound"}:
                return None
            raise
        return ObjectStorageMetadata(
            content_length=response.get("ContentLength"),
            content_type=response.get("ContentType"),
            etag=_strip_etag(response.get("ETag")),
            checksum=(
                response.get("ChecksumSHA256")
                or response.get("ChecksumSHA1")
                or response.get("ChecksumCRC32C")
                or response.get("ChecksumCRC32")
            ),
            observed_at=utc_now(),
        )

    def copy(
        self,
        object_id: str,
        *,
        source_backend: StorageBackend,
        destination_backend: StorageBackend,
    ) -> None:
        source = self._resolve_backend(source_backend)
        destination = self._resolve_backend(destination_backend)
        if _same_storage_endpoint(source, destination):
            self._s3_client(destination).copy_object(
                Bucket=_require_bucket(destination),
                Key=object_id,
                CopySource={
                    "Bucket": _require_bucket(source),
                    "Key": object_id,
                },
                MetadataDirective="COPY",
            )
            return

        response = self._s3_client(source).get_object(
            Bucket=_require_bucket(source),
            Key=object_id,
        )
        body = response["Body"]
        try:
            extra_args = _copy_upload_args(response)
            destination_client = self._s3_client(destination)
            if extra_args:
                destination_client.upload_fileobj(
                    body,
                    _require_bucket(destination),
                    object_id,
                    ExtraArgs=extra_args,
                )
            else:
                destination_client.upload_fileobj(
                    body,
                    _require_bucket(destination),
                    object_id,
                )
        finally:
            body.close()

    def delete(self, object_id: str, *, backend: StorageBackend | None = None) -> None:
        self.delete_batch([object_id], backend=backend)

    def delete_batch(
        self, object_ids: Iterable[str], *, backend: StorageBackend | None = None
    ) -> None:
        resolved_backend = self._resolve_backend(backend)
        unique_object_ids = list(dict.fromkeys(object_ids))
        if not unique_object_ids:
            return
        client = self._s3_client(resolved_backend)
        bucket = _require_bucket(resolved_backend)
        for delete_batch in _chunked(unique_object_ids, 1000):
            client.delete_objects(
                Bucket=bucket,
                Delete={"Objects": [{"Key": object_id} for object_id in delete_batch]},
            )

    def check_backend(self, backend: StorageBackend) -> None:
        resolved_backend = self._resolve_backend(backend)
        self._s3_client(resolved_backend).head_bucket(
            Bucket=_require_bucket(resolved_backend),
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
            ExpiresIn=self._settings.presigned_put_ttl_seconds(),
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
        cache_key = (
            backend.id,
            public,
            endpoint_url or "",
            _credential_fingerprint(backend),
        )
        with self._lock:
            self._evict_stale_s3_clients(cache_key)
            client = self._s3_clients.get(cache_key)
            if client is None:
                client = self._new_s3_client(backend=backend, public=public)
                self._s3_clients[cache_key] = client
            return client

    def _evict_stale_s3_clients(self, cache_key: tuple[UUID, bool, str, str]) -> None:
        backend_id, public, endpoint_url, _fingerprint = cache_key
        stale_keys = [
            key
            for key in self._s3_clients
            if key[:3] == (backend_id, public, endpoint_url) and key != cache_key
        ]
        for stale_key in stale_keys:
            _close_client(self._s3_clients.pop(stale_key))

    def _new_s3_client(self, *, backend: StorageBackend, public: bool):
        endpoint_url = (
            backend.public_endpoint_url if public else backend.endpoint_url
        ) or backend.endpoint_url
        if not endpoint_url or not backend.access_key or not backend.secret_key:
            raise ConfigurationError(
                f"Storage backend {backend.id} is missing S3 endpoint or credentials."
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

    def _resolve_backend(self, backend: StorageBackend | None) -> StorageBackend:
        resolved = _configured_backend(backend, self._settings)
        credential = self._credentials.get(resolved.id)
        if credential is not None:
            return replace(
                resolved,
                access_key=credential.access_key,
                secret_key=credential.secret_key,
            )
        return resolved


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


def _strip_etag(value: Any) -> str | None:
    if value is None:
        return None
    return str(value).strip('"')


def _credential_fingerprint(backend: StorageBackend) -> str:
    if not backend.access_key or not backend.secret_key:
        return ""
    digest = hashlib.sha256()
    digest.update(backend.access_key.encode("utf-8"))
    digest.update(b"\0")
    digest.update(backend.secret_key.encode("utf-8"))
    return digest.hexdigest()[:16]


def _same_storage_endpoint(source: StorageBackend, destination: StorageBackend) -> bool:
    return (source.endpoint_url or "") == (
        destination.endpoint_url or ""
    ) and source.region == destination.region


def _copy_upload_args(response: dict[str, Any]) -> dict[str, Any]:
    extra_args: dict[str, Any] = {}
    if response.get("ContentType"):
        extra_args["ContentType"] = response["ContentType"]
    if response.get("Metadata"):
        extra_args["Metadata"] = response["Metadata"]
    return extra_args


def _chunked(items: list[str], size: int) -> Iterable[list[str]]:
    for index in range(0, len(items), size):
        yield items[index : index + size]


def _close_client(client: Any) -> None:
    close = getattr(client, "close", None)
    if callable(close):
        close()
