from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from tests.support.fixtures import load_json_fixture
from tests.tams.support import PRIMARY_BACKEND_LABEL

pytestmark = [pytest.mark.tams_conformance, pytest.mark.tams_semantics]


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
    assert payload["api_version"] == "8.2"
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
    assert payload[0]["store_type"] == "http_object_store"
    assert payload[0]["provider"] == "tamoss"
    assert payload[0]["store_product"] == "s3"

    storage_head = client.head("/service/storage-backends")
    assert storage_head.status_code == 200
    assert storage_head.content == b""
