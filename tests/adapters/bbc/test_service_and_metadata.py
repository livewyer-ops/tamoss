from __future__ import annotations

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient
from tamoss.errors import ConfigurationError
from tamoss.settings import Settings

from tests.adapters.bbc.support import PRIMARY_BACKEND_LABEL
from tests.support.fixtures import load_json_fixture

pytestmark = pytest.mark.bbc


def test_service_root_and_metadata_follow_bbc_service_shape(
    client: TestClient,
) -> None:
    """GET / and GET /service expose the BBC service discovery shape."""
    root = client.get("/")
    assert root.status_code == 200
    assert root.json() == [
        "service",
        "flows",
        "sources",
        "objects",
        "flow-delete-requests",
    ]

    root_head = client.head("/")
    assert root_head.status_code == 200
    assert root_head.content == b""

    service = client.get("/service")
    assert service.status_code == 200
    payload = service.json()
    assert payload["type"] == "urn:x-tams:service.tamoss"
    assert payload["api_version"] == "8.0"
    assert payload["service_version"] == "tamoss-bbc-parity"
    assert payload["min_object_timeout"] == "300:0"
    assert payload["min_presigned_url_timeout"] == "30:0"
    assert {"name": "webhooks"} in payload["event_stream_mechanisms"]

    service_head = client.head("/service")
    assert service_head.status_code == 200
    assert service_head.content == b""


def test_service_metadata_update_is_reflected_in_service_resource(
    client: TestClient,
) -> None:
    """bbc-id: semantic.service.schema.required_fields"""
    updated = client.post(
        "/service",
        json=load_json_fixture("bbc/service_metadata_update.json"),
    )
    assert updated.status_code == 200

    service = client.get("/service")
    assert service.status_code == 200
    assert service.json()["name"] == "BBC parity service"
    assert service.json()["description"] == "metadata update"

    invalid_query = client.get("/service", params={"unexpected": "1"})
    assert invalid_query.status_code == 400
    assert invalid_query.json()["type"] == "bad_request"


def test_storage_backend_listing_returns_configured_s3_backend(
    client: TestClient,
) -> None:
    storage_backends = client.get("/service/storage-backends")
    assert storage_backends.status_code == 200
    payload = storage_backends.json()
    assert len(payload) == 1
    assert payload[0]["label"] == PRIMARY_BACKEND_LABEL
    assert payload[0]["default_storage"] is True
    assert all(item["store_type"] == "http_object_store" for item in payload)

    storage_head = client.head("/service/storage-backends")
    assert storage_head.status_code == 200
    assert storage_head.content == b""


def test_readyz_checks_repository_dependency(
    tamoss_app: FastAPI,
    client: TestClient,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    assert client.get("/readyz").status_code == 200

    repository = tamoss_app.state.tamoss_use_cases.repository

    def fail_metadata_check():
        raise ConfigurationError("repository unavailable")

    monkeypatch.setattr(repository, "get_service_metadata", fail_metadata_check)
    ready = client.get("/readyz")

    assert ready.status_code == 503
    assert ready.json()["status"] == "not_ready"
    assert ready.json()["checks"]["repository"]["ok"] is False
    assert client.get("/healthz").status_code == 200


def test_s3_storage_backend_inherits_secret_backed_connection_settings(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("TAMOSS_S3_ACCESS_KEY", "storage-access")
    monkeypatch.setenv("TAMOSS_S3_SECRET_KEY", "storage-secret")
    monkeypatch.setenv("TAMOSS_S3_ENDPOINT", "http://object-store.default:9000")
    monkeypatch.setenv("TAMOSS_S3_PUBLIC_ENDPOINT", "https://objects.example.test")
    monkeypatch.setenv("TAMOSS_S3_REGION", "eu-west-2")
    monkeypatch.setenv("TAMOSS_S3_BUCKET", "media-primary")
    monkeypatch.setenv("TAMOSS_STORAGE_LABEL", "example.primary:s3:media")
    monkeypatch.setenv("TAMOSS_STORAGE_PROVIDER", "example")

    settings = Settings(auth_required=False)
    backend = settings.storage_backend
    assert backend is not None

    assert backend.bucket_name == "media-primary"
    assert backend.label == "example.primary:s3:media"
    assert backend.provider == "example"
    assert backend.access_key == "storage-access"
    assert backend.secret_key == "storage-secret"
    assert backend.endpoint_url == "http://object-store.default:9000"
    assert backend.public_endpoint_url == "https://objects.example.test"
    assert backend.region == "eu-west-2"
