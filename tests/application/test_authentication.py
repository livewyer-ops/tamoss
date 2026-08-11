from __future__ import annotations

import base64

import pytest
import tamoss.auth as auth_module
from fastapi.routing import APIRoute, iter_route_contexts
from fastapi.testclient import TestClient
from tamoss.app import create_app
from tamoss.application.use_cases import TamossUseCases
from tamoss.auth import Identity
from tamoss.settings import Settings, StorageBackendSettings

from tests.support.memory_repository import FakeTamossRepository
from tests.support.object_storage import InMemoryObjectStorage

pytestmark = pytest.mark.tamoss_security

API_TOKEN = "bbc-token"
BASIC_USERNAME = "bbc-user"
BASIC_PASSWORD = "bbc-password"


def test_authentication_required_rejects_anonymous_requests() -> None:
    with _auth_client() as client:
        response = client.get("/service")

    assert response.status_code == 401
    assert response.headers["www-authenticate"] == "Bearer, Basic"


def test_public_metrics_path_is_not_exposed() -> None:
    with _auth_client() as client:
        response = client.get("/metrics")

    assert response.status_code == 401
    assert response.json()["type"] == "unauthorized"


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
    assert response.json()["api_version"] == "8.2"


def test_bbc_url_token_authentication() -> None:
    with _auth_client() as client:
        response = client.get("/service", params={"access_token": API_TOKEN})

    assert response.status_code == 200
    assert response.json()["api_version"] == "8.2"


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
    assert response.json()["api_version"] == "8.2"


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
    assert response.json()["api_version"] == "8.2"


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


def test_oauth2_route_scopes_are_enforced_independently_of_provider(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    flow_id = "00000000-0000-4000-8000-000000000001"
    with _oauth_client(
        monkeypatch,
        {
            "admin-token": {"tams-api/admin"},
            "read-token": {"tams-api/read"},
            "write-token": {"tams-api/write"},
            "delete-token": {"tams-api/delete"},
        },
    ) as client:
        read_service = client.get("/service", headers=_bearer("read-token"))
        read_write_response = client.put(
            f"/flows/{flow_id}/label",
            json="new label",
            headers=_bearer("read-token"),
        )
        write_response = client.put(
            f"/flows/{flow_id}/label",
            json="new label",
            headers=_bearer("write-token"),
        )
        write_delete_response = client.delete(
            f"/flows/{flow_id}",
            headers=_bearer("write-token"),
        )
        delete_response = client.delete(
            f"/flows/{flow_id}",
            headers=_bearer("delete-token"),
        )
        admin_service = client.get("/service", headers=_bearer("admin-token"))

    assert read_service.status_code == 200
    assert read_write_response.status_code == 403
    assert read_write_response.json()["type"] == "forbidden"
    assert write_response.status_code != 403
    assert write_delete_response.status_code == 403
    assert delete_response.status_code != 403
    assert admin_service.status_code == 200


def test_oauth2_profile_routes_require_read_and_write_scopes(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    profile_id = "00000000-0000-4000-8000-000000000001"
    with _oauth_client(
        monkeypatch,
        {
            "read-token": {"tams-api/read"},
            "write-token": {"tams-api/write"},
        },
    ) as client:
        read_list = client.get("/service/profiles", headers=_bearer("read-token"))
        write_list = client.get("/service/profiles", headers=_bearer("write-token"))
        read_create = client.post(
            f"/service/profiles/{profile_id}",
            json={},
            headers=_bearer("read-token"),
        )
        write_create = client.post(
            f"/service/profiles/{profile_id}",
            json={},
            headers=_bearer("write-token"),
        )

    assert read_list.status_code == 200
    assert write_list.status_code == 403
    assert read_create.status_code == 403
    assert write_create.status_code != 403


def test_oauth2_scope_names_are_configurable(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    settings = _auth_settings(
        api_token=None,
        basic_auth_password=None,
        oauth2_enabled=True,
        oauth2_read_scope="provider/read",
    )

    with _oauth_client(
        monkeypatch,
        {
            "default-read-token": {"tams-api/read"},
            "provider-read-token": {"provider/read"},
        },
        settings,
    ) as client:
        default_response = client.get(
            "/service",
            headers=_bearer("default-read-token"),
        )
        provider_response = client.get(
            "/service",
            headers=_bearer("provider-read-token"),
        )

    assert default_response.status_code == 403
    assert provider_response.status_code == 200


def test_oauth2_token_without_scope_claim_keeps_full_access(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    flow_id = "00000000-0000-4000-8000-000000000001"

    with _oauth_client(monkeypatch, {"no-scope-token": set()}) as client:
        service_response = client.get("/service", headers=_bearer("no-scope-token"))
        write_response = client.put(
            f"/flows/{flow_id}/label",
            json="new label",
            headers=_bearer("no-scope-token"),
        )
        delete_response = client.delete(
            f"/flows/{flow_id}",
            headers=_bearer("no-scope-token"),
        )

    assert service_response.status_code == 200
    assert write_response.status_code != 403
    assert delete_response.status_code != 403


def test_oauth2_unscoped_full_access_can_be_disabled(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    settings = _auth_settings(
        api_token=None,
        basic_auth_password=None,
        oauth2_enabled=True,
        oauth2_allow_unscoped_full_access=False,
    )

    with _oauth_client(monkeypatch, {"no-scope-token": set()}, settings) as client:
        response = client.get("/service", headers=_bearer("no-scope-token"))

    assert response.status_code == 403
    assert response.json()["type"] == "forbidden"


def test_oauth2_authorization_default_denies_unmapped_api_routes(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    client = _oauth_client(monkeypatch, {"admin-token": {"tams-api/admin"}})

    @client.app.get("/unmapped-test-route")
    def unmapped_test_route() -> dict[str, bool]:
        return {"ok": True}

    with client:
        response = client.get(
            "/unmapped-test-route",
            headers=_bearer("admin-token"),
        )

    assert response.status_code == 403
    assert response.json()["type"] == "forbidden"


def test_oauth2_scope_policy_covers_tamoss_api_routes() -> None:
    route_exemptions = {
        "/healthz",
        "/readyz",
    }
    client = _auth_client()

    missing = []
    for route_context in iter_route_contexts(client.app.routes):
        route = route_context.route
        if not isinstance(route, APIRoute) or route_context.path in route_exemptions:
            continue
        missing.extend(
            f"{method} {route_context.path}"
            for method in sorted((route_context.methods or set()) - {"OPTIONS"})
            if (method, route_context.path) not in auth_module.OAUTH2_ROUTE_SCOPE_GROUPS
        )

    assert missing == []


def test_static_token_authentication_keeps_full_access_without_oauth_scopes() -> None:
    flow_id = "00000000-0000-4000-8000-000000000001"

    with _auth_client() as client:
        response = client.put(
            f"/flows/{flow_id}/label",
            json="new label",
            headers=_bearer(API_TOKEN),
        )

    assert response.status_code != 403


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


def _oauth_client(
    monkeypatch: pytest.MonkeyPatch,
    scopes_by_token: dict[str, set[str]],
    settings: Settings | None = None,
) -> TestClient:
    def fake_bearer_identity(token: str, _settings: Settings) -> Identity | None:
        scopes = scopes_by_token.get(token)
        if scopes is None:
            return None
        return Identity(
            subject=f"oauth2:{token}",
            method="bearer-oauth2",
            scopes=frozenset(scopes),
        )

    monkeypatch.setattr(auth_module, "_bearer_identity", fake_bearer_identity)
    return _auth_client(
        settings
        or _auth_settings(
            api_token=None,
            basic_auth_password=None,
            oauth2_enabled=True,
        )
    )


def _bearer(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


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
