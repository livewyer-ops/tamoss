from __future__ import annotations

import base64

import pytest
from fastapi.testclient import TestClient
from tamoss.app import create_app
from tamoss.application.use_cases import TamossUseCases, storage_backend_from_settings
from tamoss.settings import Settings, StorageBackendSettings

from tests.conftest import _BbcParityObjectStorage
from tests.support.memory_repository import InMemoryRepository

pytestmark = pytest.mark.bbc

API_TOKEN = "bbc-token"
BASIC_USERNAME = "bbc-user"
BASIC_PASSWORD = "bbc-password"


def test_authentication_required_rejects_anonymous_requests() -> None:
    with _auth_client() as client:
        response = client.get("/service")

    assert response.status_code == 401
    assert response.headers["www-authenticate"] == "Bearer, Basic"


def test_bbc_bearer_token_authentication() -> None:
    with _auth_client() as client:
        response = client.get(
            "/service",
            headers={"Authorization": f"Bearer {API_TOKEN}"},
        )

    assert response.status_code == 200
    assert response.json()["api_version"] == "8.0"


def test_bbc_url_token_authentication() -> None:
    with _auth_client() as client:
        response = client.get("/service", params={"access_token": API_TOKEN})

    assert response.status_code == 200
    assert response.json()["api_version"] == "8.0"


def test_bbc_basic_authentication() -> None:
    credentials = base64.b64encode(
        f"{BASIC_USERNAME}:{BASIC_PASSWORD}".encode("utf-8")
    ).decode("ascii")

    with _auth_client() as client:
        response = client.get(
            "/service",
            headers={"Authorization": f"Basic {credentials}"},
        )

    assert response.status_code == 200
    assert response.json()["api_version"] == "8.0"


def test_bbc_authentication_rejects_invalid_credentials() -> None:
    with _auth_client() as client:
        bearer_response = client.get(
            "/service",
            headers={"Authorization": "Bearer wrong-token"},
        )
        url_response = client.get("/service", params={"access_token": "wrong-token"})

    assert bearer_response.status_code == 401
    assert url_response.status_code == 401


def _auth_client() -> TestClient:
    settings = Settings(
        auth_required=True,
        api_token=API_TOKEN,
        basic_auth_username=BASIC_USERNAME,
        basic_auth_password=BASIC_PASSWORD,
        storage_backend=StorageBackendSettings(
            id="11111111-1111-4111-8111-111111111111",
            label="tamoss.storage.primary",
            provider="tamoss",
            region="us-east-1",
            store_product="s3",
            default_storage=True,
            bucket_name="tamoss-auth",
            endpoint_url="https://objects.internal.example.test",
            public_endpoint_url="https://objects.example.test",
            access_key="access",
            secret_key="secret",
        ),
    )
    storage_backend = storage_backend_from_settings(settings)
    assert storage_backend is not None
    use_cases = TamossUseCases(
        repository=InMemoryRepository(storage_backend),
        object_storage=_BbcParityObjectStorage(),
        settings=settings,
    )
    return TestClient(create_app(settings, use_cases=use_cases))
