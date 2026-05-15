from __future__ import annotations

from uuid import uuid4

import pytest
from fastapi.testclient import TestClient

from tests.adapters.bbc.support import (
    IMAGE_FORMAT,
    PRIMARY_BACKEND_ID,
    PRIMARY_BACKEND_LABEL,
    VIDEO_FORMAT,
    allocate_objects,
    assert_bbc_error,
    image_flow_payload,
    multi_flow_payload,
    register_segment,
    video_flow_payload,
)

pytestmark = pytest.mark.bbc


def test_video_flow_write_creates_source_and_supports_read_filters(
    client: TestClient,
) -> None:
    """bbc-id: semantic.example.flow_video_schema_compliance"""
    flow_id = uuid4()
    source_id = uuid4()
    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(
            flow_id,
            source_id,
            label="main camera",
            description="primary programme video",
            tags={"editorial": "programme", "roles": ["video", "main"]},
        ),
    )
    assert created.status_code == 201
    assert created.json()["id"] == str(flow_id)
    assert created.json()["source_id"] == str(source_id)
    assert created.json()["format"] == VIDEO_FORMAT
    assert created.json()["read_only"] is False

    flow_head = client.head(f"/flows/{flow_id}")
    assert flow_head.status_code == 200
    assert flow_head.content == b""

    listed_flows = client.get(
        "/flows",
        params={
            "source_id": str(source_id),
            "format": VIDEO_FORMAT,
            "codec": "video/h264",
            "label": "main camera",
            "frame_width": "1920",
            "frame_height": "1080",
            "tag.editorial": "programme",
        },
    )
    assert listed_flows.status_code == 200
    assert listed_flows.headers["x-paging-limit"] == "100"
    assert [item["id"] for item in listed_flows.json()] == [str(flow_id)]

    flows_head = client.head("/flows", params={"limit": "1"})
    assert flows_head.status_code == 200
    assert flows_head.headers["x-paging-limit"] == "1"

    source = client.get(f"/sources/{source_id}")
    assert source.status_code == 200
    assert source.json()["id"] == str(source_id)
    assert source.json()["format"] == VIDEO_FORMAT
    assert source.json()["label"] == "main camera"
    assert source.json()["tags"] == {
        "editorial": "programme",
        "roles": ["video", "main"],
    }

    listed_sources = client.get(
        "/sources",
        params={"format": VIDEO_FORMAT, "tag_exists.editorial": "true"},
    )
    assert listed_sources.status_code == 200
    assert [item["id"] for item in listed_sources.json()] == [str(source_id)]


def test_list_flows_can_include_timerange_extension(client: TestClient) -> None:
    flow_id = uuid4()
    idle_flow_id = uuid4()
    source_id = uuid4()
    idle_source_id = uuid4()

    assert (
        client.put(
            f"/flows/{flow_id}",
            json=video_flow_payload(flow_id, source_id),
        ).status_code
        == 201
    )
    assert (
        client.put(
            f"/flows/{idle_flow_id}",
            json=video_flow_payload(idle_flow_id, idle_source_id),
        ).status_code
        == 201
    )
    register_segment(client, flow_id, timerange="[0:0_10:0)")

    default_list = client.get("/flows")
    assert default_list.status_code == 200
    assert all("timerange" not in item for item in default_list.json())

    listed = client.get("/flows", params={"include_timerange": "true"})
    assert listed.status_code == 200
    payload = {item["id"]: item for item in listed.json()}
    assert payload[str(flow_id)]["timerange"] == "[0:0_10:0)"
    assert payload[str(idle_flow_id)]["timerange"] == "()"

    head = client.head("/flows", params={"include_timerange": "true"})
    assert head.status_code == 200

    invalid = client.get(
        "/flows",
        params={"include_timerange": "true", "include_ranges": "true"},
    )
    assert invalid.status_code == 400
    assert_bbc_error(invalid.json())


def test_flow_put_replaces_tags_when_omitted(client: TestClient) -> None:
    flow_id = uuid4()
    source_id = uuid4()
    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(
            flow_id,
            source_id,
            tags={"editorial": "programme"},
        ),
    )
    assert created.status_code == 201
    assert created.json()["tags"] == {"editorial": "programme"}

    replaced = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(flow_id, source_id, label="replacement"),
    )

    assert replaced.status_code == 204
    assert client.get(f"/flows/{flow_id}").json()["tags"] == {}


def test_flow_and_source_property_endpoints_round_trip(client: TestClient) -> None:
    flow_id = uuid4()
    source_id = uuid4()
    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(flow_id, source_id, label="initial"),
    )
    assert created.status_code == 201

    flow_label = client.put(f"/flows/{flow_id}/label", json="updated")
    assert flow_label.status_code == 204
    flow_label_get = client.get(f"/flows/{flow_id}/label")
    assert flow_label_get.status_code == 200
    assert flow_label_get.json() == "updated"
    flow_label_delete = client.delete(f"/flows/{flow_id}/label")
    assert flow_label_delete.status_code == 204
    missing_flow_label = client.get(f"/flows/{flow_id}/label")
    assert missing_flow_label.status_code == 404

    flow_description = client.put(f"/flows/{flow_id}/description", json="described")
    assert flow_description.status_code == 204
    flow_description_get = client.get(f"/flows/{flow_id}/description")
    assert flow_description_get.status_code == 200
    assert flow_description_get.json() == "described"

    avg_rate = client.put(f"/flows/{flow_id}/avg_bit_rate", json=1000)
    assert avg_rate.status_code == 204
    assert client.get(f"/flows/{flow_id}/avg_bit_rate").json() == 1000
    max_rate = client.put(f"/flows/{flow_id}/max_bit_rate", json=2000)
    assert max_rate.status_code == 204
    assert client.get(f"/flows/{flow_id}/max_bit_rate").json() == 2000

    read_only = client.put(f"/flows/{flow_id}/read_only", json=False)
    assert read_only.status_code == 204
    assert client.get(f"/flows/{flow_id}/read_only").json() is False

    tag_put = client.put(f"/flows/{flow_id}/tags/editorial", json=["clean", "tx"])
    assert tag_put.status_code == 204
    flow_tag = client.get(f"/flows/{flow_id}/tags/editorial")
    assert flow_tag.status_code == 200
    assert flow_tag.json() == ["clean", "tx"]
    tag_delete = client.delete(f"/flows/{flow_id}/tags/editorial")
    assert tag_delete.status_code == 204

    source_label = client.put(f"/sources/{source_id}/label", json="source label")
    assert source_label.status_code == 204
    assert client.get(f"/sources/{source_id}/label").json() == "source label"
    source_description = client.put(
        f"/sources/{source_id}/description", json="source description"
    )
    assert source_description.status_code == 204
    assert client.get(f"/sources/{source_id}/description").json() == (
        "source description"
    )
    source_tag = client.put(f"/sources/{source_id}/tags/channel", json="bbc-one")
    assert source_tag.status_code == 204
    assert client.get(f"/sources/{source_id}/tags/channel").json() == "bbc-one"
    source_tag_delete = client.delete(f"/sources/{source_id}/tags/channel")
    assert source_tag_delete.status_code == 204


def test_flow_collection_projects_to_source_collection_and_collected_by(
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
    read_only_child = client.put(f"/flows/{child_flow_id}/read_only", json=True)
    assert read_only_child.status_code == 204
    parent = client.put(
        f"/flows/{parent_flow_id}",
        json=multi_flow_payload(parent_flow_id, parent_source_id),
    )
    assert parent.status_code == 201

    updated_collection = client.put(
        f"/flows/{parent_flow_id}/flow_collection",
        json=[{"id": str(child_flow_id), "role": "video"}],
    )
    assert updated_collection.status_code == 204

    collection = client.get(f"/flows/{parent_flow_id}/flow_collection")
    assert collection.status_code == 200
    assert collection.json() == [{"id": str(child_flow_id), "role": "video"}]

    parent_source = client.get(f"/sources/{parent_source_id}")
    assert parent_source.status_code == 200
    assert parent_source.json()["source_collection"] == [
        {"id": str(child_source_id), "role": "video"}
    ]

    child_source = client.get(f"/sources/{child_source_id}")
    assert child_source.status_code == 200
    assert child_source.json()["collected_by"] == [str(parent_source_id)]
    child_flow = client.get(f"/flows/{child_flow_id}")
    assert child_flow.status_code == 200
    assert child_flow.json()["collected_by"] == [str(parent_flow_id)]

    collection_head = client.head(f"/flows/{parent_flow_id}/flow_collection")
    assert collection_head.status_code == 200

    deleted_collection = client.delete(f"/flows/{parent_flow_id}/flow_collection")
    assert deleted_collection.status_code == 204
    assert client.get(f"/flows/{parent_flow_id}/flow_collection").json() == []
    assert "collected_by" not in client.get(f"/flows/{child_flow_id}").json()


def test_flow_validation_follows_bbc_concrete_content_shapes(
    client: TestClient,
) -> None:
    image_flow_id = uuid4()
    image_source_id = uuid4()
    image = client.put(
        f"/flows/{image_flow_id}",
        json=image_flow_payload(image_flow_id, image_source_id),
    )
    assert image.status_code == 201
    assert image.json()["format"] == IMAGE_FORMAT

    missing_essence_flow_id = uuid4()
    missing_essence = client.put(
        f"/flows/{missing_essence_flow_id}",
        json={
            "id": str(missing_essence_flow_id),
            "source_id": str(uuid4()),
            "format": VIDEO_FORMAT,
            "codec": "video/h264",
            "container": "video/mp2t",
        },
    )
    assert missing_essence.status_code == 400
    assert_bbc_error(missing_essence.json())

    mismatch_flow_id = uuid4()
    mismatch = client.put(
        f"/flows/{mismatch_flow_id}",
        json={
            "id": str(mismatch_flow_id),
            "source_id": str(image_source_id),
            "format": VIDEO_FORMAT,
            "codec": "video/h264",
            "container": "video/mp2t",
            "essence_parameters": {
                "frame_width": 1920,
                "frame_height": 1080,
                "frame_rate": {"numerator": 25, "denominator": 1},
            },
        },
    )
    assert mismatch.status_code == 400


def test_segments_accept_bbc_bodies_and_emit_paging_headers(
    client: TestClient,
) -> None:
    flow_id = uuid4()
    source_id = uuid4()
    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(flow_id, source_id),
    )
    assert created.status_code == 201
    object_one = f"bbc/{uuid4()}.ts"
    object_two = f"bbc/{uuid4()}.ts"

    allocated = client.post(
        f"/flows/{flow_id}/storage",
        json={"object_ids": [object_one, object_two]},
    )
    assert allocated.status_code == 201
    assert [item["object_id"] for item in allocated.json()["media_objects"]] == [
        object_one,
        object_two,
    ]

    segments = client.post(
        f"/flows/{flow_id}/segments",
        json=[
            {"object_id": object_one, "timerange": "[0:0_10:0)"},
            {
                "object_id": object_two,
                "timerange": "[10:0_20:0)",
                "object_timerange": "[100:0_110:0)",
                "sample_offset": 10,
                "sample_count": 250,
                "key_frame_count": 5,
            },
        ],
    )
    assert segments.status_code == 201

    listed = client.get(
        f"/flows/{flow_id}/segments",
        params={
            "limit": "1",
            "reverse_order": "true",
            "include_object_timerange": "true",
            "accept_get_urls": PRIMARY_BACKEND_LABEL,
            "accept_storage_ids": str(PRIMARY_BACKEND_ID),
            "presigned": "true",
            "verbose_storage": "true",
        },
    )
    assert listed.status_code == 200
    assert listed.headers["x-paging-limit"] == "1"
    assert listed.headers["x-paging-count"] == "1"
    assert listed.headers["x-paging-reverse-order"] == "true"
    assert listed.headers["x-paging-timerange"] == "[0:0_20:0)"
    assert "x-paging-nextkey" in listed.headers
    assert "link" in listed.headers
    payload = listed.json()
    assert payload[0]["object_id"] == object_two
    assert payload[0]["object_timerange"] == "[100:0_110:0)"
    assert payload[0]["get_urls"][0]["storage_id"] == str(PRIMARY_BACKEND_ID)
    assert payload[0]["get_urls"][0]["controlled"] is True

    next_page = client.get(
        f"/flows/{flow_id}/segments",
        params={
            "page": listed.headers["x-paging-nextkey"],
            "limit": "1",
            "reverse_order": "true",
        },
    )
    assert next_page.status_code == 200
    assert [item["object_id"] for item in next_page.json()] == [object_one]

    missing_flow_segments = client.get(f"/flows/{uuid4()}/segments")
    assert missing_flow_segments.status_code == 200
    assert missing_flow_segments.json() == []
    assert missing_flow_segments.headers["x-paging-timerange"] == "()"

    invalid_wrapper = client.post(
        f"/flows/{flow_id}/segments",
        json={"segments": [{"object_id": object_one, "timerange": "[20:0_30:0)"}]},
    )
    assert invalid_wrapper.status_code == 400


def test_segment_batch_reports_per_segment_failures(client: TestClient) -> None:
    flow_id = uuid4()
    source_id = uuid4()
    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(flow_id, source_id),
    )
    assert created.status_code == 201
    object_one = f"bbc/{uuid4()}.ts"
    object_two = f"bbc/{uuid4()}.ts"
    allocate_objects(client, flow_id, [object_one, object_two])

    initial = client.post(
        f"/flows/{flow_id}/segments",
        json={"object_id": object_one, "timerange": "[0:0_10:0)"},
    )
    assert initial.status_code == 201

    response = client.post(
        f"/flows/{flow_id}/segments",
        json=[
            {"object_id": object_two, "timerange": "[5:0_15:0)"},
            {"object_id": object_two, "timerange": "[10:0_20:0)"},
        ],
    )
    assert response.status_code == 200
    assert response.json()["failed_segments"][0]["object_id"] == object_two
    assert response.json()["failed_segments"][0]["error"]["type"] == "TAMSError"
