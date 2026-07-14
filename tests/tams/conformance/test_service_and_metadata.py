from __future__ import annotations

from uuid import UUID

import pytest
from fastapi.testclient import TestClient
from tamoss.app import create_app
from tamoss.application.use_cases import TamossUseCases
from tamoss.domain.model import StorageBackend

from tests.support.fixtures import load_json_fixture
from tests.support.memory_repository import FakeTamossRepository
from tests.support.object_storage import InMemoryObjectStorage
from tests.support.settings import bbc_parity_settings
from tests.tams.support import PRIMARY_BACKEND_LABEL

pytestmark = pytest.mark.tams_conformance


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
    assert payload["api_version"] == "8.1"
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


def test_storage_backend_listing_sorts_by_label_and_reverses() -> None:
    settings = bbc_parity_settings()
    primary = settings.storage_backend_record()
    assert primary is not None
    alpha = StorageBackend(
        id=UUID("22222222-2222-4222-8222-222222222222"),
        label="aaa-storage",
        provider="example",
        region="local",
        store_product="s3",
    )
    zulu = StorageBackend(
        id=UUID("33333333-3333-4333-8333-333333333333"),
        label="zzz-storage",
        provider="example",
        region="local",
        store_product="s3",
    )
    app = create_app(
        settings,
        use_cases=TamossUseCases(
            repository=FakeTamossRepository(
                primary,
                storage_backends=[zulu, primary, alpha],
            ),
            object_storage=InMemoryObjectStorage(),
            settings=settings,
        ),
    )

    with TestClient(app) as local_client:
        listed = local_client.get("/service/storage-backends")
        reversed_listing = local_client.get(
            "/service/storage-backends",
            params={"reverse_order": "true"},
        )
        head = local_client.head(
            "/service/storage-backends",
            params={"reverse_order": "true"},
        )

    assert [item["label"] for item in listed.json()] == [
        alpha.label,
        primary.label,
        zulu.label,
    ]
    assert [item["label"] for item in reversed_listing.json()] == [
        zulu.label,
        primary.label,
        alpha.label,
    ]
    assert head.status_code == 200
