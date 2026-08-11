from __future__ import annotations

from uuid import uuid4

import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient

from tests.tams.support import (
    PRIMARY_BACKEND_ID,
    upload_allocated_object,
    video_flow_payload,
)

pytestmark = pytest.mark.tams_conformance


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
