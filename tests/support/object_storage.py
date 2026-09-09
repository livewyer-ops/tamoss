from __future__ import annotations

from collections.abc import Iterable
from urllib.parse import quote
from uuid import UUID

from tamoss.domain.model import (
    ObjectGetUrlBatchKey,
    ObjectGetUrlRequest,
    ObjectStorageMetadata,
    StorageBackend,
)


class InMemoryObjectStorage:
    def __init__(
        self,
        *,
        base_url: str = "https://objects.example.test",
        metadata_content_type: str = "video/mp2t",
        metadata_etag_prefix: str = "memory",
        check_backend_error: Exception | None = None,
        include_backend_in_url: bool = True,
        quote_object_ids: bool = True,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self.metadata_content_type = metadata_content_type
        self.metadata_etag_prefix = metadata_etag_prefix
        self.check_backend_error = check_backend_error
        self.include_backend_in_url = include_backend_in_url
        self.quote_object_ids = quote_object_ids
        self.deleted: list[tuple[UUID, str]] = []
        self.deleted_batches: list[list[tuple[UUID, str]]] = []
        self.checked_backend_ids: list[UUID] = []
        self.built_get_urls: list[tuple[UUID, str]] = []
        self.built_get_url_batches: list[list[tuple[UUID, str]]] = []
        self._objects: dict[tuple[UUID, str], bytes] = {}

    def build_put_request(
        self,
        *,
        object_id: str,
        content_type: str,
        backend: StorageBackend,
        presigned: bool,
    ) -> dict[str, object]:
        return {
            "url": self._object_url(object_id=object_id, backend=backend),
            "content-type": content_type,
            "headers": {"Content-Type": content_type},
        }

    def build_get_url(self, *, object_id: str, backend: StorageBackend) -> str:
        return self._object_url(object_id=object_id, backend=backend)

    def build_get_urls(
        self, *, object_id: str, backend: StorageBackend
    ) -> list[dict[str, object]]:
        self.built_get_urls.append((backend.id, object_id))
        return [
            {
                "url": self.build_get_url(object_id=object_id, backend=backend),
                "label": backend.label,
                "presigned": False,
            },
            {
                "url": self.build_get_url(object_id=object_id, backend=backend),
                "label": backend.label,
                "presigned": True,
            },
        ]

    def build_get_urls_batch(
        self, requests: Iterable[ObjectGetUrlRequest]
    ) -> dict[ObjectGetUrlBatchKey, list[dict[str, object]]]:
        unique_requests: dict[ObjectGetUrlBatchKey, ObjectGetUrlRequest] = {}
        for request in requests:
            key = (request.backend.id, request.object_id)
            existing = unique_requests.get(key)
            if existing is None:
                unique_requests[key] = request
                continue
            existing.include_direct = existing.include_direct or request.include_direct
            existing.include_presigned = (
                existing.include_presigned or request.include_presigned
            )
        self.built_get_url_batches.append(list(unique_requests))
        result: dict[ObjectGetUrlBatchKey, list[dict[str, object]]] = {}
        for key, request in unique_requests.items():
            all_urls = self.build_get_urls(object_id=key[1], backend=request.backend)
            result[key] = [
                item
                for item in all_urls
                if (request.include_direct and item.get("presigned") is False)
                or (request.include_presigned and item.get("presigned") is True)
            ]
        return result

    def write(
        self, object_id: str, data: bytes, *, backend: StorageBackend | None = None
    ) -> None:
        assert backend is not None
        self._objects[(backend.id, object_id)] = data

    def read(
        self, object_id: str, *, backend: StorageBackend | None = None
    ) -> bytes | None:
        assert backend is not None
        return self._objects.get((backend.id, object_id))

    def object_metadata(
        self, object_id: str, *, backend: StorageBackend | None = None
    ) -> ObjectStorageMetadata | None:
        data = self.read(object_id, backend=backend)
        if data is None:
            return None
        return ObjectStorageMetadata(
            content_length=len(data),
            content_type=self.metadata_content_type,
            etag=f"{self.metadata_etag_prefix}-{len(data)}",
        )

    def copy(
        self,
        object_id: str,
        *,
        source_backend: StorageBackend,
        destination_backend: StorageBackend,
    ) -> None:
        data = self.read(object_id, backend=source_backend)
        if data is None:
            raise FileNotFoundError(object_id)
        self.write(object_id, data, backend=destination_backend)

    def delete(self, object_id: str, *, backend: StorageBackend | None = None) -> None:
        self.delete_batch([object_id], backend=backend)

    def delete_batch(
        self, object_ids: Iterable[str], *, backend: StorageBackend | None = None
    ) -> None:
        assert backend is not None
        batch = [(backend.id, object_id) for object_id in dict.fromkeys(object_ids)]
        if not batch:
            return
        self.deleted_batches.append(batch)
        for _, object_id in batch:
            self.deleted.append((backend.id, object_id))
            self._objects.pop((backend.id, object_id), None)

    def check_backend(self, backend: StorageBackend) -> None:
        if self.check_backend_error is not None:
            raise self.check_backend_error
        self.checked_backend_ids.append(backend.id)

    def _object_url(self, *, object_id: str, backend: StorageBackend) -> str:
        key = quote(object_id, safe="/") if self.quote_object_ids else object_id
        if self.include_backend_in_url:
            return f"{self.base_url}/{backend.id}/{key}"
        return f"{self.base_url}/{key}"
