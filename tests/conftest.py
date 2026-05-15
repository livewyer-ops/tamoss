from __future__ import annotations

from collections.abc import Iterator
from urllib.parse import quote
from uuid import UUID

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient
from tamoss.app import create_app
from tamoss.application.use_cases import TamossUseCases, storage_backend_from_settings
from tamoss.domain.model import StorageBackend
from tamoss.settings import Settings, StorageBackendSettings

from tests.support.memory_repository import InMemoryRepository


@pytest.fixture
def tamoss_app() -> FastAPI:
    settings = _bbc_parity_settings()
    return create_app(settings, use_cases=_bbc_parity_use_cases(settings))


@pytest.fixture
def client(tamoss_app: FastAPI) -> Iterator[TestClient]:
    with TestClient(tamoss_app) as test_client:
        yield test_client


def _bbc_parity_settings() -> Settings:
    return Settings(
        auth_required=False,
        database_url=None,
        public_base_url="http://testserver",
        service_name="TAMOSS BBC parity",
        service_description="BBC API parity test instance",
        service_version="tamoss-bbc-parity",
        storage_backend=StorageBackendSettings(
            id="11111111-1111-4111-8111-111111111111",
            label="tamoss.storage.primary",
            provider="tamoss",
            region="us-east-1",
            store_product="s3",
            default_storage=True,
            bucket_name="tamoss-primary",
            endpoint_url="https://objects.internal.example.test",
            public_endpoint_url="https://objects.example.test",
            access_key="access",
            secret_key="secret",
        ),
    )


def _bbc_parity_use_cases(settings: Settings) -> TamossUseCases:
    storage_backend = storage_backend_from_settings(settings)
    assert storage_backend is not None
    return TamossUseCases(
        repository=InMemoryRepository(storage_backend),
        object_storage=_BbcParityObjectStorage(),
        settings=settings,
    )


class _BbcParityObjectStorage:
    def __init__(self) -> None:
        self._objects: dict[tuple[UUID, str], bytes] = {}

    def build_put_request(
        self, *, object_id: str, flow_container: str, backend: StorageBackend
    ) -> dict:
        return {
            "url": self._object_url(object_id=object_id, backend=backend),
            "content-type": flow_container,
            "headers": {"Content-Type": flow_container},
        }

    def build_get_url(self, *, object_id: str, backend: StorageBackend) -> str:
        return self._object_url(object_id=object_id, backend=backend)

    def build_get_urls(self, *, object_id: str, backend: StorageBackend) -> list[dict]:
        return [
            {
                "url": self._object_url(object_id=object_id, backend=backend),
                "label": backend.label,
                "presigned": True,
            }
        ]

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

    def delete(self, object_id: str, *, backend: StorageBackend | None = None) -> None:
        assert backend is not None
        self._objects.pop((backend.id, object_id), None)

    def _object_url(self, *, object_id: str, backend: StorageBackend) -> str:
        key = quote(object_id, safe="/")
        return f"https://objects.example.test/{backend.id}/{key}"
