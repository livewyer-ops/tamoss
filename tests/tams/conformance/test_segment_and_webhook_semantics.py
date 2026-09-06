from __future__ import annotations

from uuid import UUID, uuid4

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient

from tests.tams.support import (
    create_video_flow,
    flow_collection_item,
    multi_flow_payload,
    register_segment,
    video_flow_payload,
    webhook_payload,
)

pytestmark = [pytest.mark.tams_conformance, pytest.mark.tams_semantics]


def test_segment_reads_emit_effective_object_timerange_only_when_requested(
    client: TestClient,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    equal_object_id = register_segment(client, flow_id)
    different_object_id = register_segment(
        client,
        flow_id,
        timerange="[20:0_30:0)",
        object_timerange="[100:0_110:0)",
    )

    omitted = client.get(f"/flows/{flow_id}/segments")
    false = client.get(
        f"/flows/{flow_id}/segments",
        params={"include_object_timerange": "false"},
    )
    included = client.get(
        f"/flows/{flow_id}/segments",
        params={"include_object_timerange": "true"},
    )

    assert omitted.status_code == 200
    assert false.status_code == 200
    assert included.status_code == 200
    assert all("object_timerange" not in item for item in omitted.json())
    assert all("object_timerange" not in item for item in false.json())
    timeranges = {
        item["object_id"]: item["object_timerange"] for item in included.json()
    }
    assert timeranges == {
        equal_object_id: "[0:0_10:0)",
        different_object_id: "[100:0_110:0)",
    }


def test_segments_sort_exclusive_range_before_adjacent_point_and_reverse(
    client: TestClient,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    range_object_id = register_segment(
        client,
        flow_id,
        timerange="[1:0_2:0)",
    )
    point_object_id = register_segment(
        client,
        flow_id,
        timerange="[2:0]",
    )

    forward = client.get(f"/flows/{flow_id}/segments")
    reverse = client.get(
        f"/flows/{flow_id}/segments",
        params={"reverse_order": "true"},
    )

    assert forward.status_code == 200
    assert reverse.status_code == 200
    assert [item["object_id"] for item in forward.json()] == [
        range_object_id,
        point_object_id,
    ]
    assert [item["object_id"] for item in reverse.json()] == [
        point_object_id,
        range_object_id,
    ]
    assert forward.headers["x-paging-reverse-order"] == "false"
    assert reverse.headers["x-paging-reverse-order"] == "true"


def test_segment_webhooks_emit_effective_object_timerange_only_when_requested(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    webhook_ids: dict[str, UUID] = {}
    for mode, option in (("omitted", None), ("false", False), ("true", True)):
        body = webhook_payload(events=["flows/segments_added"])
        if option is not None:
            body["include_object_timerange"] = option
        created = client.post("/service/webhooks", json=body)
        assert created.status_code == 201
        webhook_ids[mode] = UUID(created.json()["id"])

    equal_object_id = register_segment(client, flow_id)
    different_object_id = register_segment(
        client,
        flow_id,
        timerange="[20:0_30:0)",
        object_timerange="[100:0_110:0)",
    )

    deliveries = tamoss_app.state.tamoss_use_cases.repository.list_webhook_deliveries()
    segments_by_webhook = {
        webhook_id: {
            delivery.payload["event"]["segments"][0]["object_id"]: delivery.payload[
                "event"
            ]["segments"][0]
            for delivery in deliveries
            if delivery.webhook_id == webhook_id
        }
        for webhook_id in webhook_ids.values()
    }
    for mode in ("omitted", "false"):
        assert all(
            "object_timerange" not in segment
            for segment in segments_by_webhook[webhook_ids[mode]].values()
        )
    assert {
        object_id: segment["object_timerange"]
        for object_id, segment in segments_by_webhook[webhook_ids["true"]].items()
    } == {
        equal_object_id: "[0:0_10:0)",
        different_object_id: "[100:0_110:0)",
    }


def test_flow_collection_webhook_selector_distinguishes_omitted_empty_and_parent(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    child_flow_id, _, _ = create_video_flow(client)
    top_level_flow_id, _, _ = create_video_flow(client)
    parent_flow_id = uuid4()
    parent_source_id = uuid4()
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

    webhook_ids = _register_collection_selector_webhooks(
        client,
        events=["flows/updated"],
        selector_name="flow_collected_by_ids",
        parent_id=parent_flow_id,
    )

    child_update = client.put(f"/flows/{child_flow_id}/label", json="child")
    top_level_update = client.put(f"/flows/{top_level_flow_id}/label", json="top-level")
    assert child_update.status_code == 204
    assert top_level_update.status_code == 204

    event_ids = _event_resource_ids_by_webhook(
        tamoss_app,
        resource_name="flow",
    )
    assert event_ids[webhook_ids["omitted"]] == {
        str(child_flow_id),
        str(top_level_flow_id),
    }
    assert event_ids[webhook_ids["empty"]] == {str(top_level_flow_id)}
    assert event_ids[webhook_ids["parent"]] == {str(child_flow_id)}


def test_source_collection_webhook_selector_distinguishes_omitted_empty_and_parent(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    child_flow_id = uuid4()
    child_source_id = uuid4()
    top_level_flow_id = uuid4()
    top_level_source_id = uuid4()
    parent_flow_id = uuid4()
    parent_source_id = uuid4()
    for flow_id, source_id, payload_factory in (
        (child_flow_id, child_source_id, video_flow_payload),
        (top_level_flow_id, top_level_source_id, video_flow_payload),
        (parent_flow_id, parent_source_id, multi_flow_payload),
    ):
        created = client.put(
            f"/flows/{flow_id}",
            json=payload_factory(flow_id, source_id),
        )
        assert created.status_code == 201
    collection = client.put(
        f"/flows/{parent_flow_id}/flow_collection",
        json=[flow_collection_item(child_flow_id)],
    )
    assert collection.status_code == 204

    webhook_ids = _register_collection_selector_webhooks(
        client,
        events=["sources/updated"],
        selector_name="source_collected_by_ids",
        parent_id=parent_source_id,
    )

    child_update = client.put(f"/sources/{child_source_id}/label", json="child")
    top_level_update = client.put(
        f"/sources/{top_level_source_id}/label", json="top-level"
    )
    assert child_update.status_code == 204
    assert top_level_update.status_code == 204

    event_ids = _event_resource_ids_by_webhook(
        tamoss_app,
        resource_name="source",
    )
    assert event_ids[webhook_ids["omitted"]] == {
        str(child_source_id),
        str(top_level_source_id),
    }
    assert event_ids[webhook_ids["empty"]] == {str(top_level_source_id)}
    assert event_ids[webhook_ids["parent"]] == {str(child_source_id)}


def test_flow_deletion_uses_pre_delete_collection_selector_context(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    child_flow_id, _, _ = create_video_flow(client)
    top_level_flow_id, _, _ = create_video_flow(client)
    parent_flow_id = uuid4()
    parent_source_id = uuid4()
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
    register_segment(client, child_flow_id)

    webhook_ids = _register_collection_selector_webhooks(
        client,
        events=["flows/deleted"],
        selector_name="flow_collected_by_ids",
        parent_id=parent_flow_id,
    )

    assert client.delete(f"/flows/{child_flow_id}").status_code == 202
    assert (
        tamoss_app.state.tamoss_use_cases.deletion.process_pending_delete_requests()
        == 1
    )
    assert client.delete(f"/flows/{top_level_flow_id}").status_code == 204

    event_ids = _deleted_event_resource_ids_by_webhook(
        tamoss_app,
        id_name="flow_id",
    )
    assert event_ids[webhook_ids["omitted"]] == {
        str(child_flow_id),
        str(top_level_flow_id),
    }
    assert event_ids[webhook_ids["empty"]] == {str(top_level_flow_id)}
    assert event_ids[webhook_ids["parent"]] == {str(child_flow_id)}


def test_source_deletion_uses_context_from_its_deleted_flow(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    child_flow_id, child_source_id, _ = create_video_flow(client)
    top_level_flow_id, top_level_source_id, _ = create_video_flow(client)
    parent_flow_id = uuid4()
    parent_source_id = uuid4()
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

    webhook_ids = _register_collection_selector_webhooks(
        client,
        events=["sources/deleted"],
        selector_name="source_collected_by_ids",
        parent_id=parent_source_id,
    )

    assert client.delete(f"/flows/{child_flow_id}").status_code == 204
    assert client.delete(f"/flows/{top_level_flow_id}").status_code == 204
    assert client.get(f"/sources/{child_source_id}").status_code == 404
    assert client.get(f"/sources/{top_level_source_id}").status_code == 404

    event_ids = _deleted_event_resource_ids_by_webhook(
        tamoss_app,
        id_name="source_id",
    )
    assert event_ids[webhook_ids["omitted"]] == {
        str(child_source_id),
        str(top_level_source_id),
    }
    assert event_ids[webhook_ids["empty"]] == {str(top_level_source_id)}
    assert event_ids[webhook_ids["parent"]] == {str(child_source_id)}


def _register_collection_selector_webhooks(
    client: TestClient,
    *,
    events: list[str],
    selector_name: str,
    parent_id: UUID,
) -> dict[str, UUID]:
    selectors: tuple[tuple[str, list[str] | None], ...] = (
        ("omitted", None),
        ("empty", []),
        ("parent", [str(parent_id)]),
    )
    webhook_ids: dict[str, UUID] = {}
    for mode, selector in selectors:
        body = webhook_payload(
            url=f"https://{mode}.example.test/webhook",
            events=events,
        )
        if selector is not None:
            body[selector_name] = selector
        created = client.post("/service/webhooks", json=body)
        assert created.status_code == 201
        payload = created.json()
        if selector is None:
            assert selector_name not in payload
        else:
            assert payload[selector_name] == selector
        webhook_id = UUID(payload["id"])
        stored = client.get(f"/service/webhooks/{webhook_id}")
        assert stored.status_code == 200
        if selector is None:
            assert selector_name not in stored.json()
        else:
            assert stored.json()[selector_name] == selector
        webhook_ids[mode] = webhook_id
    return webhook_ids


def _event_resource_ids_by_webhook(
    tamoss_app: FastAPI,
    *,
    resource_name: str,
) -> dict[UUID, set[str]]:
    deliveries = tamoss_app.state.tamoss_use_cases.repository.list_webhook_deliveries()
    return {
        webhook_id: {
            delivery.payload["event"][resource_name]["id"]
            for delivery in deliveries
            if delivery.webhook_id == webhook_id
        }
        for webhook_id in {delivery.webhook_id for delivery in deliveries}
    }


def _deleted_event_resource_ids_by_webhook(
    tamoss_app: FastAPI,
    *,
    id_name: str,
) -> dict[UUID, set[str]]:
    deliveries = tamoss_app.state.tamoss_use_cases.repository.list_webhook_deliveries()
    return {
        webhook_id: {
            delivery.payload["event"][id_name]
            for delivery in deliveries
            if delivery.webhook_id == webhook_id
        }
        for webhook_id in {delivery.webhook_id for delivery in deliveries}
    }
