from __future__ import annotations

import base64

import pytest
from fastapi.testclient import TestClient
from tamoss.app import create_app
from tamoss.application.use_cases import TamossUseCases
from tamoss.settings import Settings, StorageBackendSettings

from tests.support.memory_repository import FakeTamossRepository
from tests.support.object_storage import InMemoryObjectStorage

pytestmark = pytest.mark.bbc

API_TOKEN = "bbc-token"
BASIC_USERNAME = "bbc-user"
BASIC_PASSWORD = "bbc-password"


def test_authentication_required_rejects_anonymous_requests() -> None:
    with _auth_client() as client:
        response = client.get("/service")

    assert response.status_code == 401
    assert response.headers["www-authenticate"] == "Bearer, Basic"


@pytest.mark.parametrize(
    ("method", "path"),
    [
        ("GET", "/service"),
        ("POST", "/service"),
        ("GET", "/sources"),
        ("GET", "/flows"),
        ("POST", "/flows/00000000-0000-4000-8000-000000000001/segments"),
        ("POST", "/flows/00000000-0000-4000-8000-000000000001/storage"),
        ("GET", "/objects/auth-denied-object"),
        ("POST", "/service/webhooks"),
    ],
)
def test_authentication_required_rejects_protected_endpoint_families(
    method: str, path: str
) -> None:
    with _auth_client() as client:
        response = client.request(method, path, json={})

    assert response.status_code == 401
    assert response.json()["type"] == "unauthorized"
    assert "www-authenticate" in response.headers


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
        f"{BASIC_USERNAME}:{BASIC_PASSWORD}".encode()
    ).decode("ascii")

    with _auth_client() as client:
        response = client.get(
            "/service",
            headers={"Authorization": f"Basic {credentials}"},
        )

    assert response.status_code == 200
    assert response.json()["api_version"] == "8.0"


def test_bbc_basic_authentication_does_not_use_static_token_as_password() -> None:
    credentials = base64.b64encode(f"{BASIC_USERNAME}:{API_TOKEN}".encode()).decode(
        "ascii"
    )
    settings = _auth_settings(basic_auth_password=None)

    with _auth_client(settings) as client:
        response = client.get(
            "/service",
            headers={"Authorization": f"Basic {credentials}"},
        )

    assert response.status_code == 401
    assert response.headers["www-authenticate"] == "Bearer"


def test_forward_auth_requires_shared_proof_setting() -> None:
    with pytest.raises(ValueError, match="TAMOSS_FORWARD_AUTH_SHARED_SECRET"):
        _auth_settings(trust_forward_auth_headers=True)


def test_forward_auth_rejects_identity_headers_without_matching_proof() -> None:
    settings = _auth_settings(
        trust_forward_auth_headers=True,
        forward_auth_shared_secret="proxy-proof",
    )

    with _auth_client(settings) as client:
        missing_response = client.get(
            "/service",
            headers={"Remote-User": "alice"},
        )
        wrong_response = client.get(
            "/service",
            headers={
                "Remote-User": "alice",
                "X-TAMOSS-Forward-Auth-Secret": "wrong-proof",
            },
        )

    assert missing_response.status_code == 401
    assert wrong_response.status_code == 401


def test_forward_auth_accepts_identity_headers_with_matching_proof() -> None:
    settings = _auth_settings(
        trust_forward_auth_headers=True,
        forward_auth_shared_secret="proxy-proof",
    )

    with _auth_client(settings) as client:
        response = client.get(
            "/service",
            headers={
                "Remote-User": "alice",
                "X-TAMOSS-Forward-Auth-Secret": "proxy-proof",
            },
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


def test_bbc_static_token_file_reloads_without_restarting_client(tmp_path) -> None:
    token_file = tmp_path / "api-token"
    token_file.write_text("first-token", encoding="utf-8")
    settings = _auth_settings(api_token_file=str(token_file), api_token=None)

    with _auth_client(settings) as client:
        assert (
            client.get(
                "/service",
                headers={"Authorization": "Bearer first-token"},
            ).status_code
            == 200
        )
        token_file.write_text("second-token", encoding="utf-8")
        assert (
            client.get(
                "/service",
                headers={"Authorization": "Bearer second-token"},
            ).status_code
            == 200
        )
        assert (
            client.get(
                "/service",
                headers={"Authorization": "Bearer first-token"},
            ).status_code
            == 401
        )


def _auth_client(settings: Settings | None = None) -> TestClient:
    settings = settings or _auth_settings()
    storage_backend = settings.storage_backend_record()
    assert storage_backend is not None
    use_cases = TamossUseCases(
        repository=FakeTamossRepository(storage_backend),
        object_storage=InMemoryObjectStorage(),
        settings=settings,
    )
    return TestClient(create_app(settings, use_cases=use_cases))


def _auth_settings(**overrides: object) -> Settings:
    values = {
        "auth_required": True,
        "api_token": API_TOKEN,
        "basic_auth_username": BASIC_USERNAME,
        "basic_auth_password": BASIC_PASSWORD,
        "storage_backend": StorageBackendSettings(
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
    }
    values.update(overrides)
    return Settings(**values)
