from __future__ import annotations

from uuid import UUID, uuid4

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient

from tests.support.fixtures import load_json_fixture
from tests.tams.support import (
    PRIMARY_BACKEND_ID,
    PRIMARY_BACKEND_LABEL,
    flow_collection_item,
    multi_flow_payload,
    register_segment,
    upload_allocated_object,
    video_flow_payload,
    webhook_payload,
)

pytestmark = pytest.mark.tams_conformance


def test_webhook_registration_lifecycle_hides_secret_material(
    client: TestClient,
) -> None:
    """bbc-id: semantic.runtime.webhooks_routed"""
    body = load_json_fixture("bbc/webhook_registration.json")

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

    updated_body = dict(body)
    updated_body.update({"id": webhook_id, "status": "disabled"})
    updated = client.put(
        f"/service/webhooks/{webhook_id}",
        json=updated_body,
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
        json=webhook_payload(events=["unknown"]),
    )
    assert invalid_event.status_code == 400
    assert invalid_event.json()["type"] == "bad_request"

    invalid_header = client.post(
        "/service/webhooks",
        json=webhook_payload(api_key_name="Content-Type", api_key_value="secret"),
    )
    assert invalid_header.status_code == 400

    created = client.post(
        "/service/webhooks",
        json=webhook_payload(),
    )
    assert created.status_code == 201
    mismatch = client.put(
        f"/service/webhooks/{created.json()['id']}",
        json=webhook_payload(
            id="00000000-0000-4000-8000-000000000001",
            status="created",
        ),
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
            json=webhook_payload(url=url),
        )
        assert response.status_code == 400


@pytest.mark.tamoss_security
def test_webhook_configuration_rejects_restricted_targets(
    client: TestClient,
) -> None:
    for url in [
        "http://127.0.0.1/bbc-webhook",
        "http://169.254.169.254/latest/meta-data",
        "https://receiver.default.svc.cluster.local/bbc-webhook",
    ]:
        response = client.post(
            "/service/webhooks",
            json=webhook_payload(url=url),
        )
        assert response.status_code == 400


@pytest.mark.tamoss_security
def test_webhook_configuration_accepts_allowlisted_private_target(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    tamoss_app.state.tamoss_settings.webhook_allowed_hosts = ["127.0.0.1"]

    response = client.post(
        "/service/webhooks",
        json=webhook_payload(url="http://127.0.0.1/bbc-webhook"),
    )

    assert response.status_code == 201


def test_webhook_put_replaces_optional_fields_and_tags(client: TestClient) -> None:
    created = client.post(
        "/service/webhooks",
        json=webhook_payload(
            api_key_name="x-api-key",
            api_key_value="secret",
            accept_get_urls=["primary"],
            presigned=True,
            tags={"owner": "bbc"},
        ),
    )
    assert created.status_code == 201
    webhook_id = created.json()["id"]

    replaced = client.put(
        f"/service/webhooks/{webhook_id}",
        json=webhook_payload(
            id=webhook_id,
            url="https://example.test/replacement",
            events=["flows/deleted"],
            status="created",
        ),
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
        json=webhook_payload(
            events=[
                "flows/created",
                "flows/updated",
                "flows/deleted",
                "flows/segments_added",
                "flows/segments_deleted",
                "sources/created",
                "sources/updated",
                "sources/deleted",
            ],
            source_ids=[str(source_id)],
            accept_get_urls=[PRIMARY_BACKEND_LABEL],
            accept_storage_ids=[str(PRIMARY_BACKEND_ID)],
            presigned=True,
            verbose_storage=True,
        ),
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
        "flows/updated",
        "flows/segments_added",
        "sources/updated",
        "flows/segments_deleted",
        "flows/updated",
        "flows/deleted",
        "sources/deleted",
    ]
    assert deliveries[1].payload["event"]["flow"]["id"] == str(flow_id)
    assert deliveries[2].payload["event"]["flow"]["segments_updated"]
    assert deliveries[3].payload["event"]["segments"][0]["object_id"] == object_id
    assert deliveries[3].payload["event"]["segments"][0]["get_urls"][0]["label"] == (
        PRIMARY_BACKEND_LABEL
    )
    assert deliveries[3].payload["event"]["segments"][0]["get_urls"][0][
        "storage_id"
    ] == str(PRIMARY_BACKEND_ID)
    assert deliveries[5].payload["event"]["timerange"] == "[0:0_10:0)"
    assert deliveries[6].payload["event"]["flow"]["segments_updated"]
    assert (
        client.get(f"/service/webhooks/{webhook.json()['id']}").json()["status"]
        == "started"
    )


def test_webhook_flow_event_keeps_bbc_projection_without_collection_timerange(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    child_flow_id = uuid4()
    child_source_id = uuid4()
    parent_flow_id = uuid4()
    parent_source_id = uuid4()

    created_child = client.put(
        f"/flows/{child_flow_id}",
        json=video_flow_payload(child_flow_id, child_source_id),
    )
    assert created_child.status_code == 201
    register_segment(client, child_flow_id, timerange="[0:0_10:0)")

    created_parent = client.put(
        f"/flows/{parent_flow_id}",
        json=multi_flow_payload(parent_flow_id, parent_source_id),
    )
    assert created_parent.status_code == 201
    webhook = client.post(
        "/service/webhooks",
        json=webhook_payload(events=["flows/updated"]),
    )
    assert webhook.status_code == 201

    updated_collection = client.put(
        f"/flows/{parent_flow_id}/flow_collection",
        json=[flow_collection_item(child_flow_id)],
    )
    assert updated_collection.status_code == 204

    deliveries = tamoss_app.state.tamoss_use_cases.repository.list_webhook_deliveries()
    assert [delivery.event_type for delivery in deliveries] == ["flows/updated"]
    payload = deliveries[0].payload["event"]["flow"]
    assert payload["id"] == str(parent_flow_id)
    assert "timerange" not in payload


def test_webhook_source_events_ignore_flow_only_filters(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    source_id = uuid4()
    flow_id = uuid4()
    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(flow_id, source_id),
    )
    assert created.status_code == 201
    webhook = client.post(
        "/service/webhooks",
        json=webhook_payload(
            events=["sources/updated"],
            flow_ids=["00000000-0000-4000-8000-000000000001"],
            flow_collected_by_ids=["00000000-0000-4000-8000-000000000002"],
            source_ids=[str(source_id)],
        ),
    )
    assert webhook.status_code == 201

    updated = client.put(f"/sources/{source_id}/label", json="updated")
    assert updated.status_code == 204

    deliveries = tamoss_app.state.tamoss_use_cases.repository.list_webhook_deliveries()
    assert [delivery.event_type for delivery in deliveries] == ["sources/updated"]
    assert deliveries[0].payload["event"]["source"]["id"] == str(source_id)


def test_webhook_empty_storage_filter_keeps_get_urls(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    source_id = uuid4()
    flow_id = uuid4()
    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(flow_id, source_id),
    )
    assert created.status_code == 201
    webhook = client.post(
        "/service/webhooks",
        json=webhook_payload(
            events=["flows/segments_added"],
            accept_storage_ids=[],
            verbose_storage=True,
        ),
    )
    assert webhook.status_code == 201

    register_segment(client, flow_id)

    deliveries = tamoss_app.state.tamoss_use_cases.repository.list_webhook_deliveries()
    assert [delivery.event_type for delivery in deliveries] == ["flows/segments_added"]
    get_urls = deliveries[0].payload["event"]["segments"][0]["get_urls"]
    assert get_urls
    assert {get_url["storage_id"] for get_url in get_urls} == {str(PRIMARY_BACKEND_ID)}


def test_webhook_segment_payload_omits_object_timerange(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    source_id = uuid4()
    flow_id = uuid4()
    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(flow_id, source_id),
    )
    assert created.status_code == 201
    webhook = client.post(
        "/service/webhooks",
        json=webhook_payload(events=["flows/segments_added"]),
    )
    assert webhook.status_code == 201

    object_id = register_segment(
        client,
        flow_id,
        object_timerange="[10:0_20:0)",
    )

    deliveries = tamoss_app.state.tamoss_use_cases.repository.list_webhook_deliveries()
    segment = deliveries[0].payload["event"]["segments"][0]
    assert segment["object_id"] == object_id
    assert "object_timerange" not in segment


def test_webhook_segment_payload_includes_object_timerange_and_init_object(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    source_id = uuid4()
    flow_id = uuid4()
    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(
            flow_id,
            source_id,
            essence_parameters={
                "frame_width": 1920,
                "frame_height": 1080,
                "frame_rate": {"numerator": 25, "denominator": 1},
                "init_segments": True,
            },
        ),
    )
    assert created.status_code == 201
    webhook = client.post(
        "/service/webhooks",
        json=webhook_payload(
            events=["flows/segments_added"],
            include_object_timerange=True,
            verbose_storage=True,
        ),
    )
    assert webhook.status_code == 201

    media_id = f"bbc/{uuid4()}.m4s"
    init_id = f"bbc/{uuid4()}.mp4"
    media_allocation = client.post(
        f"/flows/{flow_id}/storage",
        json={"object_ids": [media_id]},
    )
    assert media_allocation.status_code == 201
    init_allocation = client.post(
        f"/flows/{flow_id}/storage",
        json={
            "object_ids": [init_id],
            "content_type": "video/mp4",
        },
    )
    assert init_allocation.status_code == 201, init_allocation.text
    upload_allocated_object(client, media_id)
    upload_allocated_object(client, init_id)

    registered = client.post(
        f"/flows/{flow_id}/segments",
        json={
            "object_id": media_id,
            "init_object_id": init_id,
            "timerange": "[20:0_30:0)",
            "object_timerange": "[10:0_20:0)",
        },
    )
    assert registered.status_code == 201

    deliveries = tamoss_app.state.tamoss_use_cases.repository.list_webhook_deliveries()
    segment = deliveries[0].payload["event"]["segments"][0]
    assert segment["object_timerange"] == "[10:0_20:0)"
    assert segment["init_object"]["object_id"] == init_id
    assert segment["init_object"]["get_urls"]
    assert segment["get_urls"]


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
        json=[flow_collection_item(child_flow_id)],
    )
    assert collection.status_code == 204

    webhook = client.post(
        "/service/webhooks",
        json=webhook_payload(
            events=["flows/updated", "sources/updated"],
            source_collected_by_ids=[str(parent_source_id)],
        ),
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
    assert deliveries[0].payload["event"]["source"]["collected_by"] == [
        str(parent_source_id)
    ]
    assert deliveries[1].payload["event"]["flow"]["id"] == str(child_flow_id)


def test_webhook_registration_accepts_empty_event_list(client: TestClient) -> None:
    """bbc-id: semantic.webhooks.empty_event_list_is_valid"""
    body = webhook_payload(url="https://webhooks.example.test/empty", events=[])
    created = client.post("/service/webhooks", json=body)
    assert created.status_code == 201
    assert created.json()["events"] == []
