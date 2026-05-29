from __future__ import annotations

from collections.abc import Iterator

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient
from tamoss.app import create_app
from tamoss.application.use_cases import TamossUseCases
from tamoss.settings import Settings

from tests.support.memory_repository import FakeTamossRepository
from tests.support.object_storage import InMemoryObjectStorage
from tests.support.settings import bbc_parity_settings


@pytest.fixture
def tamoss_app() -> FastAPI:
    settings = bbc_parity_settings()
    return create_app(settings, use_cases=_bbc_parity_use_cases(settings))


@pytest.fixture
def client(tamoss_app: FastAPI) -> Iterator[TestClient]:
    with TestClient(tamoss_app) as test_client:
        yield test_client


def _bbc_parity_use_cases(settings: Settings) -> TamossUseCases:
    storage_backend = settings.storage_backend_record()
    if storage_backend is None:
        raise RuntimeError("BBC parity settings must configure a storage backend")
    return TamossUseCases(
        repository=FakeTamossRepository(storage_backend),
        object_storage=InMemoryObjectStorage(),
        settings=settings,
    )
