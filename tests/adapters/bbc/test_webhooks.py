from __future__ import annotations

from uuid import UUID, uuid4

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient

from tests.adapters.bbc.support import (
    multi_flow_payload,
    register_segment,
    video_flow_payload,
)

pytestmark = pytest.mark.bbc


def test_webhook_registration_lifecycle_hides_secret_material(
    client: TestClient,
) -> None:
    """bbc-id: semantic.runtime.webhooks_routed"""
    body = {
        "url": "https://example.test/bbc-webhook",
        "events": ["flows/created", "flows/segments_added"],
        "api_key_name": "x-api-key",
        "api_key_value": "secret-value",
        "tags": {"owner": "bbc", "purpose": ["parity", "webhook"]},
    }

    created = client.post("/service/webhooks", json=body)
    assert created.status_code == 201
    payload = created.json()
    webhook_id = payload["id"]
    UUID(webhook_id)
    assert payload["status"] == "created"
    assert payload["api_key_name"] == "x-api-key"
    assert "api_key_value" not in payload

    listed = client.get("/service/webhooks", params={"tag.owner": "bbc"})
    assert listed.status_code == 200
    assert [item["id"] for item in listed.json()] == [webhook_id]

    list_head = client.head("/service/webhooks", params={"limit": "1"})
    assert list_head.status_code == 200
    assert list_head.headers["x-paging-limit"] == "1"
    detail_head = client.head(f"/service/webhooks/{webhook_id}")
    assert detail_head.status_code == 200

    updated = client.put(
        f"/service/webhooks/{webhook_id}",
        json={**body, "id": webhook_id, "status": "disabled"},
    )
    assert updated.status_code == 201
    assert updated.json()["status"] == "disabled"
    assert "api_key_value" not in updated.json()

    deleted = client.delete(f"/service/webhooks/{webhook_id}")
    assert deleted.status_code == 204
    missing = client.get(f"/service/webhooks/{webhook_id}")
    assert missing.status_code == 404


def test_webhook_configuration_rejects_invalid_bbc_event_or_header(
    client: TestClient,
) -> None:
    invalid_event = client.post(
        "/service/webhooks",
        json={"url": "https://example.test/bbc-webhook", "events": ["unknown"]},
    )
    assert invalid_event.status_code == 400
    assert invalid_event.json()["type"] == "bad_request"

    invalid_header = client.post(
        "/service/webhooks",
        json={
            "url": "https://example.test/bbc-webhook",
            "events": ["flows/created"],
            "api_key_name": "Content-Type",
            "api_key_value": "secret",
        },
    )
    assert invalid_header.status_code == 400

    created = client.post(
        "/service/webhooks",
        json={"url": "https://example.test/bbc-webhook", "events": ["flows/created"]},
    )
    assert created.status_code == 201
    mismatch = client.put(
        f"/service/webhooks/{created.json()['id']}",
        json={
            "id": "00000000-0000-4000-8000-000000000001",
            "url": "https://example.test/bbc-webhook",
            "events": ["flows/created"],
            "status": "created",
        },
    )
    assert mismatch.status_code == 404


def test_webhook_configuration_rejects_malformed_urls(client: TestClient) -> None:
    for url in [
        "ftp://example.test/bbc-webhook",
        "https:///bbc-webhook",
        "https://user:password@example.test/bbc-webhook",
    ]:
        response = client.post(
            "/service/webhooks",
            json={"url": url, "events": ["flows/created"]},
        )
        assert response.status_code == 400


def test_webhook_put_replaces_optional_fields_and_tags(client: TestClient) -> None:
    created = client.post(
        "/service/webhooks",
        json={
            "url": "https://example.test/bbc-webhook",
            "events": ["flows/created"],
            "api_key_name": "x-api-key",
            "api_key_value": "secret",
            "accept_get_urls": ["primary"],
            "presigned": True,
            "tags": {"owner": "bbc"},
        },
    )
    assert created.status_code == 201
    webhook_id = created.json()["id"]

    replaced = client.put(
        f"/service/webhooks/{webhook_id}",
        json={
            "id": webhook_id,
            "url": "https://example.test/replacement",
            "events": ["flows/deleted"],
            "status": "created",
        },
    )

    assert replaced.status_code == 201
    payload = replaced.json()
    assert payload["events"] == ["flows/deleted"]
    assert payload["tags"] == {}
    assert "api_key_name" not in payload
    assert "accept_get_urls" not in payload
    assert "presigned" not in payload


def test_webhook_queues_bbc_flow_source_and_segment_events(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    source_id = uuid4()
    flow_id = uuid4()
    webhook = client.post(
        "/service/webhooks",
        json={
            "url": "https://example.test/bbc-webhook",
            "events": [
                "flows/created",
                "flows/updated",
                "flows/deleted",
                "flows/segments_added",
                "flows/segments_deleted",
                "sources/created",
                "sources/updated",
                "sources/deleted",
            ],
            "source_ids": [str(source_id)],
        },
    )
    assert webhook.status_code == 201

    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(flow_id, source_id),
    )
    assert created.status_code == 201
    object_id = register_segment(client, flow_id, object_id=f"bbc/{uuid4()}.ts")

    updated_source = client.put(f"/sources/{source_id}/label", json="updated")
    assert updated_source.status_code == 204
    deleted_segment = client.delete(
        f"/flows/{flow_id}/segments", params={"object_id": object_id}
    )
    assert deleted_segment.status_code == 204
    deleted_flow = client.delete(f"/flows/{flow_id}")
    assert deleted_flow.status_code == 204

    deliveries = tamoss_app.state.tamoss_use_cases.repository.list_webhook_deliveries()
    event_types = [delivery.event_type for delivery in deliveries]
    assert event_types == [
        "sources/created",
        "flows/created",
        "flows/segments_added",
        "sources/updated",
        "flows/segments_deleted",
        "flows/deleted",
        "sources/deleted",
    ]
    assert deliveries[1].payload["event"]["flow"]["id"] == str(flow_id)
    assert deliveries[2].payload["event"]["segments"][0]["object_id"] == object_id
    assert deliveries[4].payload["event"]["timerange"] == "[0:0_10:0)"
    assert (
        client.get(f"/service/webhooks/{webhook.json()['id']}").json()["status"]
        == "started"
    )


def test_webhook_source_collection_filter_matches_child_events(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    parent_flow_id = uuid4()
    parent_source_id = uuid4()
    child_flow_id = uuid4()
    child_source_id = uuid4()

    child = client.put(
        f"/flows/{child_flow_id}",
        json=video_flow_payload(child_flow_id, child_source_id),
    )
    assert child.status_code == 201
    parent = client.put(
        f"/flows/{parent_flow_id}",
        json=multi_flow_payload(parent_flow_id, parent_source_id),
    )
    assert parent.status_code == 201
    collection = client.put(
        f"/flows/{parent_flow_id}/flow_collection",
        json=[{"id": str(child_flow_id), "role": "video"}],
    )
    assert collection.status_code == 204

    webhook = client.post(
        "/service/webhooks",
        json={
            "url": "https://example.test/bbc-webhook",
            "events": ["flows/updated", "sources/updated"],
            "source_collected_by_ids": [str(parent_source_id)],
        },
    )
    assert webhook.status_code == 201

    parent_update = client.put(f"/sources/{parent_source_id}/label", json="parent")
    assert parent_update.status_code == 204
    source_update = client.put(f"/sources/{child_source_id}/label", json="child")
    assert source_update.status_code == 204
    flow_update = client.put(f"/flows/{child_flow_id}/label", json="child flow")
    assert flow_update.status_code == 204

    deliveries = tamoss_app.state.tamoss_use_cases.repository.list_webhook_deliveries()
    event_types = [delivery.event_type for delivery in deliveries]
    assert event_types == ["sources/updated", "flows/updated"]
    assert deliveries[0].payload["event"]["source"]["id"] == str(child_source_id)
    assert deliveries[1].payload["event"]["flow"]["id"] == str(child_flow_id)
