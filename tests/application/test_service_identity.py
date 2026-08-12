from __future__ import annotations

from collections.abc import Iterator
from contextlib import contextmanager

import pytest
from fastapi.testclient import TestClient
from tamoss.app import create_app
from tamoss.application.use_cases import TamossUseCases
from tamoss.settings import Settings

from tests.support.memory_repository import FakeTamossRepository
from tests.support.object_storage import InMemoryObjectStorage
from tests.support.settings import bbc_parity_settings

pytestmark = pytest.mark.tamoss_extension

MANAGED_NAME = "Reuters External"
MANAGED_DESCRIPTION = "External-facing wire content, retained for 90 days."


@contextmanager
def _client(
    repository: FakeTamossRepository | None = None, **overrides: object
) -> Iterator[TestClient]:
    settings = bbc_parity_settings(**overrides)
    if repository is None:
        storage_backend = settings.storage_backend_record()
        assert storage_backend is not None
        repository = FakeTamossRepository(storage_backend)
    use_cases = TamossUseCases(
        repository=repository,
        object_storage=InMemoryObjectStorage(),
        settings=settings,
    )
    with TestClient(create_app(settings, use_cases=use_cases)) as client:
        yield client


def _managed_client() -> object:
    return _client(
        service_identity_managed=True,
        service_name=MANAGED_NAME,
        service_description=MANAGED_DESCRIPTION,
    )


def test_managed_identity_is_served_from_settings() -> None:
    with _managed_client() as client:
        service = client.get("/service")

    assert service.status_code == 200
    assert service.json()["name"] == MANAGED_NAME
    assert service.json()["description"] == MANAGED_DESCRIPTION


def test_managed_identity_rejects_updates() -> None:
    with _managed_client() as client:
        response = client.post("/service", json={"name": "Impostor"})

        assert response.status_code == 403
        assert response.json()["type"] == "forbidden"
        assert client.get("/service").json()["name"] == MANAGED_NAME


def test_managed_identity_ignores_previously_stored_metadata() -> None:
    """Identity set through the API before the operator claimed it must not win."""
    repository = FakeTamossRepository(
        bbc_parity_settings().storage_backend_record(),
    )
    with _client(repository) as client:
        stored = client.post(
            "/service",
            json={"name": "Set before management", "description": "stale"},
        )
        assert stored.status_code == 200

    with _client(
        repository,
        service_identity_managed=True,
        service_name=MANAGED_NAME,
        service_description=MANAGED_DESCRIPTION,
    ) as client:
        service = client.get("/service")

    assert service.json()["name"] == MANAGED_NAME
    assert service.json()["description"] == MANAGED_DESCRIPTION


def test_unmanaged_identity_still_accepts_updates() -> None:
    with _client() as client:
        response = client.post(
            "/service",
            json={"name": MANAGED_NAME, "description": MANAGED_DESCRIPTION},
        )

        assert response.status_code == 200
        assert client.get("/service").json()["name"] == MANAGED_NAME


def test_settings_read_operator_environment(monkeypatch: pytest.MonkeyPatch) -> None:
    """The operator sets these variables; the aliases are the contract between them."""
    monkeypatch.setenv("TAMOSS_SERVICE_NAME", MANAGED_NAME)
    monkeypatch.setenv("TAMOSS_SERVICE_DESCRIPTION", MANAGED_DESCRIPTION)
    monkeypatch.setenv("TAMOSS_SERVICE_IDENTITY_MANAGED", "true")

    settings = Settings()

    assert settings.service_name == MANAGED_NAME
    assert settings.service_description == MANAGED_DESCRIPTION
    assert settings.service_identity_managed is True
