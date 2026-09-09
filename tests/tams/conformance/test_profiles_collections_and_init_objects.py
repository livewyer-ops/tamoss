from __future__ import annotations

from copy import deepcopy
from uuid import UUID, uuid4

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient

from tests.tams.support import (
    PRIMARY_BACKEND_ID,
    multi_flow_payload,
    register_segment,
    upload_allocated_object,
    video_flow_payload,
    webhook_payload,
)

pytestmark = [pytest.mark.tams_conformance, pytest.mark.tams_semantics]


def _profile_payload(profile_id: UUID, *, label: str = "HD profile") -> dict:
    return {
        "id": str(profile_id),
        "label": label,
        "description": "Flow profile fixture",
        "tags": {"editorial_purpose": ["programme", "trailer"]},
        "flow_metadata": {
            "format": "urn:x-nmos:format:video",
            "codec": "video/h264",
            "container": "video/mp4",
            "avg_bit_rate": 8_000_000,
            "essence_parameters": {
                "frame_rate": {"numerator": 25, "denominator": 1},
                "frame_width": 1920,
                "frame_height": 1080,
                "init_segments": True,
            },
        },
    }


def _profile_flow_payload(flow_id: UUID, source_id: UUID, profile_id: UUID) -> dict:
    return {
        "id": str(flow_id),
        "source_id": str(source_id),
        "label": "Profiled flow",
        "status": "ingesting",
        "profile_id": str(profile_id),
    }


def test_profiles_are_immutable_filterable_paged_and_expand_flows(
    client: TestClient,
) -> None:
    profile_id = uuid4()
    profile = _profile_payload(profile_id)
    created = client.post(f"/service/profiles/{profile_id}", json=profile)
    assert created.status_code == 201, created.text
    assert created.json()["flow_metadata"]["container"] == "video/mp4"
    assert created.json()["tags"]["editorial_purpose"] == ["programme", "trailer"]

    listed = client.get(
        "/service/profiles",
        params={
            "format": "urn:x-nmos:format:video",
            "codec": "video/h264",
            "label": "HD profile",
            "limit": 1,
        },
    )
    assert listed.status_code == 200
    assert [item["id"] for item in listed.json()] == [str(profile_id)]
    assert listed.headers["x-paging-limit"] == "1"
    assert client.head("/service/profiles", params={"limit": 1}).status_code == 200
    assert client.head(f"/service/profiles/{profile_id}").status_code == 200
    assert client.get(f"/service/profiles/{profile_id}").json()["label"] == "HD profile"

    second_profile_id = uuid4()
    assert (
        client.post(
            f"/service/profiles/{second_profile_id}",
            json=_profile_payload(second_profile_id, label="Second profile"),
        ).status_code
        == 201
    )
    first_page = client.get("/service/profiles", params={"limit": 1})
    assert len(first_page.json()) == 1
    assert first_page.headers["x-paging-nextkey"]
    second_page = client.get(
        "/service/profiles",
        params={"limit": 1, "page": first_page.headers["x-paging-nextkey"]},
    )
    assert len(second_page.json()) == 1
    assert second_page.json()[0]["id"] != first_page.json()[0]["id"]

    replacement = deepcopy(profile)
    replacement["label"] = "Changed"
    duplicate = client.post(f"/service/profiles/{profile_id}", json=replacement)
    assert duplicate.status_code == 400
    assert client.get(f"/service/profiles/{profile_id}").json()["label"] == "HD profile"

    flow_id = uuid4()
    source_id = uuid4()
    flow = client.put(
        f"/flows/{flow_id}",
        json=_profile_flow_payload(flow_id, source_id, profile_id),
    )
    assert flow.status_code == 201, flow.text
    body = flow.json()
    assert body["profile_id"] == str(profile_id)
    assert body["format"] == "urn:x-nmos:format:video"
    assert body["container"] == "video/mp4"
    assert body["essence_parameters"]["init_segments"] is True
    assert body["status"] == "ingesting"

    metadata_update = _profile_flow_payload(flow_id, source_id, profile_id)
    metadata_update.pop("status")
    metadata_update["label"] = "Updated profiled flow"
    assert client.put(f"/flows/{flow_id}", json=metadata_update).status_code == 204
    assert client.get(f"/flows/{flow_id}").json()["status"] == "ingesting"

    by_profile = client.get("/flows", params={"profile_id": str(profile_id)})
    assert [item["id"] for item in by_profile.json()] == [str(flow_id)]
    by_status = client.get("/flows", params={"status": "ingesting"})
    assert [item["id"] for item in by_status.json()] == [str(flow_id)]
    override = _profile_flow_payload(uuid4(), uuid4(), profile_id)
    override["container"] = "video/mp2t"
    rejected = client.put(f"/flows/{override['id']}", json=override)
    assert rejected.status_code == 400


def test_flow_status_init_filters_sorting_and_missing_label_order(
    client: TestClient,
) -> None:
    flow_ids = [uuid4(), uuid4(), uuid4()]
    source_ids = [uuid4(), uuid4(), uuid4()]
    labels = ["Zulu", None, "Alpha"]
    statuses = ["awaiting_content", "ingesting", "closed_complete"]
    for flow_id, source_id, label, flow_status in zip(
        flow_ids, source_ids, labels, statuses, strict=True
    ):
        payload = video_flow_payload(flow_id, source_id, status=flow_status)
        if label is not None:
            payload["label"] = label
        if flow_status == "ingesting":
            payload["essence_parameters"]["init_segments"] = True
        response = client.put(f"/flows/{flow_id}", json=payload)
        assert response.status_code == 201, response.text

    status_result = client.get("/flows", params={"status": "ingesting"})
    assert [item["id"] for item in status_result.json()] == [str(flow_ids[1])]
    init_result = client.get("/flows", params={"init_segments": "true"})
    assert [item["id"] for item in init_result.json()] == [str(flow_ids[1])]

    alphabetical = client.get("/flows", params={"sort_by": "label"})
    assert [item.get("label") for item in alphabetical.json()] == [
        "Alpha",
        "Zulu",
        None,
    ]
    assert alphabetical.headers["x-paging-reverse-order"] == "false"
    reverse = client.get("/flows", params={"sort_by": "label", "reverse_order": "true"})
    assert [item.get("label") for item in reverse.json()] == [
        None,
        "Zulu",
        "Alpha",
    ]
    assert reverse.headers["x-paging-reverse-order"] == "true"


def test_optional_collection_roles_order_and_top_level_filters(
    client: TestClient,
) -> None:
    parent_flow = uuid4()
    parent_source = uuid4()
    child_flows = [uuid4(), uuid4()]
    child_sources = [uuid4(), uuid4()]
    assert (
        client.put(
            f"/flows/{parent_flow}",
            json=multi_flow_payload(parent_flow, parent_source, label="Middle"),
        ).status_code
        == 201
    )
    for flow_id, source_id, label in zip(
        child_flows, child_sources, ("Zulu", "Alpha"), strict=True
    ):
        assert (
            client.put(
                f"/flows/{flow_id}",
                json=video_flow_payload(flow_id, source_id, label=label),
            ).status_code
            == 201
        )

    collection = [
        {"id": str(child_flows[1])},
        {"id": str(child_flows[0]), "role": "main picture"},
    ]
    stored = client.put(f"/flows/{parent_flow}/flow_collection", json=collection)
    assert stored.status_code == 204
    assert client.get(f"/flows/{parent_flow}/flow_collection").json() == collection

    source = client.get(f"/sources/{parent_source}").json()
    assert source["source_collection"] == [
        {"id": str(child_sources[1])},
        {"id": str(child_sources[0]), "role": "main picture"},
    ]
    top_flows = client.get("/flows?collected_by_ids=").json()
    assert str(parent_flow) in {item["id"] for item in top_flows}
    assert not {str(item) for item in child_flows}.intersection(
        item["id"] for item in top_flows
    )
    top_sources = client.get("/sources?collected_by_ids=").json()
    assert str(parent_source) in {item["id"] for item in top_sources}
    assert not {str(item) for item in child_sources}.intersection(
        item["id"] for item in top_sources
    )
    sorted_sources = client.get("/sources", params={"sort_by": "label"})
    assert [item["label"] for item in sorted_sources.json()] == [
        "Alpha",
        "Middle",
        "Zulu",
    ]
    reversed_sources = client.get(
        "/sources", params={"sort_by": "label", "reverse_order": "true"}
    )
    assert [item["label"] for item in reversed_sources.json()] == [
        "Zulu",
        "Middle",
        "Alpha",
    ]
    assert reversed_sources.headers["x-paging-reverse-order"] == "true"


def test_init_object_lifecycle_direct_put_and_storage_tag_filtering(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    repository = tamoss_app.state.tamoss_use_cases.repository
    backend = repository.get_storage_backend(PRIMARY_BACKEND_ID)
    assert backend is not None
    backend.tags = {"access": ["programme", "archive"], "tier": "hot"}

    flow_id = uuid4()
    source_id = uuid4()
    payload = video_flow_payload(flow_id, source_id)
    payload["essence_parameters"]["init_segments"] = True
    assert client.put(f"/flows/{flow_id}", json=payload).status_code == 201

    media_id = f"objects/{uuid4()}.m4s"
    init_id = f"objects/{uuid4()}.mp4"
    media_allocation = client.post(
        f"/flows/{flow_id}/storage",
        json={"object_ids": [media_id], "presigned": False},
    )
    assert media_allocation.status_code == 201
    media_put = media_allocation.json()["media_objects"][0]
    assert media_put["presigned"] is False
    assert "X-Amz-Signature" not in media_put["put_url"]["url"]
    init_allocation = client.post(
        f"/flows/{flow_id}/storage",
        json={
            "object_ids": [init_id],
            "content_type": "video/mp4",
            "presigned": True,
        },
    )
    assert init_allocation.status_code == 201, init_allocation.text
    assert init_allocation.json()["media_objects"][0]["presigned"] is True
    upload_allocated_object(client, media_id)
    upload_allocated_object(client, init_id)

    registered = client.post(
        f"/flows/{flow_id}/segments",
        json={
            "object_id": media_id,
            "init_object_id": init_id,
            "timerange": "[10:0_20:0)",
            "object_timerange": "[0:0_10:0)",
        },
    )
    assert registered.status_code == 201, registered.text
    listed = client.get(
        f"/flows/{flow_id}/segments",
        params={
            "include_object_timerange": "true",
            "verbose_storage": "true",
            "storage_backend_tag.access": "programme",
        },
    )
    assert listed.status_code == 200
    segment = listed.json()[0]
    assert segment["object_timerange"] == "[0:0_10:0)"
    assert segment["init_object"]["object_id"] == init_id
    assert segment["get_urls"] and segment["init_object"]["get_urls"]

    filtered = client.get(
        f"/flows/{flow_id}/segments",
        params={"storage_backend_tag.access": "news"},
    ).json()[0]
    assert filtered["get_urls"] == []
    assert filtered["init_object"]["get_urls"] == []

    media_object = client.get(f"/objects/{media_id}").json()
    assert media_object["timerange"] == "[0:0_10:0)"
    assert media_object["init_object"]["id"] == init_id
    init_object = client.get(f"/objects/{init_id}").json()
    assert "timerange" not in init_object
    assert init_object["referenced_by_flows"] == [str(flow_id)]

    reused = client.post(
        f"/flows/{flow_id}/segments",
        json={"object_id": media_id, "timerange": "[20:0_30:0)"},
    )
    assert reused.status_code == 201
    assert client.get(f"/flows/{flow_id}/segments").json()[1]["init_object"]


def test_init_object_cross_use_and_missing_init_are_rejected(
    client: TestClient,
) -> None:
    flow_id = uuid4()
    source_id = uuid4()
    payload = video_flow_payload(flow_id, source_id)
    payload["essence_parameters"]["init_segments"] = True
    assert client.put(f"/flows/{flow_id}", json=payload).status_code == 201
    media_id = f"objects/{uuid4()}.m4s"
    init_id = f"objects/{uuid4()}.mp4"
    for object_id, content_type in ((media_id, None), (init_id, "video/mp4")):
        request = {"object_ids": [object_id]}
        if content_type is not None:
            request["content_type"] = content_type
        assert client.post(f"/flows/{flow_id}/storage", json=request).status_code == 201
        upload_allocated_object(client, object_id)

    missing = client.post(
        f"/flows/{flow_id}/segments",
        json={"object_id": media_id, "timerange": "[0:0_10:0)"},
    )
    assert missing.status_code == 400
    swapped = client.post(
        f"/flows/{flow_id}/segments",
        json={
            "object_id": init_id,
            "init_object_id": media_id,
            "timerange": "[0:0_10:0)",
        },
    )
    assert swapped.status_code == 400


def test_shared_init_object_is_retained_until_its_last_segment_is_deleted(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    flow_id = uuid4()
    payload = video_flow_payload(flow_id, uuid4())
    payload["essence_parameters"]["init_segments"] = True
    assert client.put(f"/flows/{flow_id}", json=payload).status_code == 201
    media_ids = [f"objects/{uuid4()}.m4s", f"objects/{uuid4()}.m4s"]
    init_id = f"objects/{uuid4()}.mp4"
    assert (
        client.post(
            f"/flows/{flow_id}/storage", json={"object_ids": media_ids}
        ).status_code
        == 201
    )
    assert (
        client.post(
            f"/flows/{flow_id}/storage",
            json={"object_ids": [init_id], "content_type": "video/mp4"},
        ).status_code
        == 201
    )
    for object_id in [*media_ids, init_id]:
        upload_allocated_object(client, object_id)
    for index, media_id in enumerate(media_ids):
        assert (
            client.post(
                f"/flows/{flow_id}/segments",
                json={
                    "object_id": media_id,
                    "init_object_id": init_id,
                    "timerange": f"[{index * 10}:0_{(index + 1) * 10}:0)",
                },
            ).status_code
            == 201
        )

    first = client.delete(
        f"/flows/{flow_id}/segments", params={"timerange": "[0:0_10:0)"}
    )
    assert first.status_code == 202
    assert (
        tamoss_app.state.tamoss_use_cases.deletion.process_pending_delete_requests()
        == 1
    )
    retained = client.get(f"/objects/{init_id}")
    assert retained.status_code == 200
    assert retained.json()["referenced_by_flows"] == [str(flow_id)]

    second = client.delete(
        f"/flows/{flow_id}/segments", params={"timerange": "[10:0_20:0)"}
    )
    assert second.status_code == 202
    assert (
        tamoss_app.state.tamoss_use_cases.deletion.process_pending_delete_requests()
        == 1
    )
    assert client.get(f"/objects/{init_id}").status_code == 404


def test_storage_webhook_and_delete_request_list_paging_and_reversal(
    tamoss_app: FastAPI,
    client: TestClient,
) -> None:
    backend = tamoss_app.state.tamoss_use_cases.repository.get_storage_backend(
        PRIMARY_BACKEND_ID
    )
    assert backend is not None
    backend.tags = {"owner": ["news", "sport"]}
    storage = client.get(
        "/service/storage-backends",
        params={"tag.owner": "sport", "limit": 1, "reverse_order": "true"},
    )
    assert storage.status_code == 200
    assert storage.json()[0]["tags"] == {"owner": ["news", "sport"]}
    assert storage.headers["x-paging-reverse-order"] == "true"

    first = client.post(
        "/service/webhooks",
        json=webhook_payload(url="https://a.example.test/hook"),
    )
    second = client.post(
        "/service/webhooks",
        json=webhook_payload(url="https://z.example.test/hook"),
    )
    assert first.status_code == second.status_code == 201
    webhooks = client.get(
        "/service/webhooks", params={"reverse_order": "true", "limit": 1}
    )
    assert [item["url"] for item in webhooks.json()] == ["https://z.example.test/hook"]
    assert webhooks.headers["x-paging-nextkey"]

    flow_ids = [uuid4(), uuid4()]
    for flow_id in flow_ids:
        assert (
            client.put(
                f"/flows/{flow_id}", json=video_flow_payload(flow_id, uuid4())
            ).status_code
            == 201
        )
        register_segment(client, flow_id)
        assert client.delete(f"/flows/{flow_id}").status_code == 202
    requests = client.get(
        "/flow-delete-requests",
        params={"sort_by": "expiry", "reverse_order": "true", "limit": 1},
    )
    assert requests.status_code == 200
    assert len(requests.json()) == 1
    assert requests.headers["x-paging-nextkey"]
    assert requests.headers["x-paging-reverse-order"] == "true"


def test_single_tag_endpoints_accept_array_values(client: TestClient) -> None:
    flow_id = uuid4()
    source_id = uuid4()
    assert (
        client.put(
            f"/flows/{flow_id}", json=video_flow_payload(flow_id, source_id)
        ).status_code
        == 201
    )
    values = ["programme", "trailer"]
    assert (
        client.put(f"/flows/{flow_id}/tags/editorial_purpose", json=values).status_code
        == 204
    )
    assert client.get(f"/flows/{flow_id}/tags/editorial_purpose").json() == values
    assert (
        client.put(
            f"/sources/{source_id}/tags/editorial_purpose", json=values
        ).status_code
        == 204
    )
    assert client.get(f"/sources/{source_id}/tags/editorial_purpose").json() == values
