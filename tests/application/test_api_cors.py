from __future__ import annotations

from fastapi.testclient import TestClient
from tamoss.app import create_app
from tamoss.application.use_cases import TamossUseCases
from tamoss.settings import Settings

from tests.support.memory_repository import FakeTamossRepository
from tests.support.object_storage import InMemoryObjectStorage
from tests.support.settings import bbc_parity_settings


def test_api_cors_preflight_allows_configured_browser_origin() -> None:
    client = _client(
        bbc_parity_settings(
            auth_required=True,
            cors_allowed_origins=["https://cuttingroom.github.io"],
        )
    )

    response = client.options(
        "/sources?limit=1",
        headers={
            "Origin": "https://cuttingroom.github.io",
            "Access-Control-Request-Method": "GET",
            "Access-Control-Request-Headers": "authorization,content-type",
        },
    )

    assert response.status_code == 200
    assert response.headers["access-control-allow-origin"] == (
        "https://cuttingroom.github.io"
    )
    assert "GET" in response.headers["access-control-allow-methods"]
    assert "authorization" in response.headers["access-control-allow-headers"].lower()


def test_api_cors_headers_are_added_to_auth_errors() -> None:
    client = _client(
        bbc_parity_settings(
            auth_required=True,
            cors_allowed_origins=["https://cuttingroom.github.io"],
        )
    )

    response = client.get(
        "/sources?limit=1",
        headers={"Origin": "https://cuttingroom.github.io"},
    )

    assert response.status_code == 401
    assert response.headers["access-control-allow-origin"] == (
        "https://cuttingroom.github.io"
    )


def _client(settings: Settings) -> TestClient:
    storage_backend = settings.storage_backend_record()
    assert storage_backend is not None
    use_cases = TamossUseCases(
        repository=FakeTamossRepository(storage_backend),
        object_storage=InMemoryObjectStorage(),
        settings=settings,
    )
    return TestClient(create_app(settings, use_cases=use_cases))
