from __future__ import annotations

from typing import Protocol

from tamoss.domain.model import StorageBackend


class ObjectStorage(Protocol):
    def build_put_request(
        self, *, object_id: str, flow_container: str, backend: StorageBackend
    ) -> dict:
        raise NotImplementedError

    def build_get_url(self, *, object_id: str, backend: StorageBackend) -> str:
        raise NotImplementedError

    def build_get_urls(self, *, object_id: str, backend: StorageBackend) -> list[dict]:
        raise NotImplementedError

    def write(
        self, object_id: str, data: bytes, *, backend: StorageBackend | None = None
    ) -> None:
        raise NotImplementedError

    def read(
        self, object_id: str, *, backend: StorageBackend | None = None
    ) -> bytes | None:
        raise NotImplementedError

    def delete(self, object_id: str, *, backend: StorageBackend | None = None) -> None:
        raise NotImplementedError
