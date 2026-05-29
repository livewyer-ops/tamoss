from __future__ import annotations

from collections.abc import Iterable, Mapping
from typing import Protocol

from tamoss.domain.model import (
    ObjectGetUrlBatchKey,
    ObjectGetUrlRequest,
    ObjectStorageMetadata,
    StorageBackend,
)


class ObjectStorage(Protocol):
    def build_put_request(
        self,
        *,
        object_id: str,
        flow_container: str,
        backend: StorageBackend,
    ) -> dict[str, object]: ...

    def build_get_urls_batch(
        self,
        requests: Iterable[ObjectGetUrlRequest],
    ) -> Mapping[ObjectGetUrlBatchKey, Iterable[Mapping[str, object]]]: ...

    def object_metadata(
        self,
        object_id: str,
        *,
        backend: StorageBackend | None = None,
    ) -> ObjectStorageMetadata | None: ...

    def copy(
        self,
        object_id: str,
        *,
        source_backend: StorageBackend,
        destination_backend: StorageBackend,
    ) -> None: ...

    def delete(
        self,
        object_id: str,
        *,
        backend: StorageBackend | None = None,
    ) -> None: ...

    def delete_batch(
        self,
        object_ids: Iterable[str],
        *,
        backend: StorageBackend | None = None,
    ) -> None: ...
