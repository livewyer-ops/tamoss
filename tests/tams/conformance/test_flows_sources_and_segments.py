from __future__ import annotations

from uuid import uuid4

import pytest
from fastapi.testclient import TestClient
from prometheus_client import REGISTRY
from tamoss.contract.generated import contract_models
from tamoss.domain.exceptions import SEGMENT_OVERLAP_MESSAGE

from tests.tams.support import (
    IMAGE_FORMAT,
    PRIMARY_BACKEND_ID,
    PRIMARY_BACKEND_LABEL,
    VIDEO_FORMAT,
    allocate_objects,
    assert_bbc_error,
    create_video_flow,
    flow_collection_item,
    image_flow_payload,
    multi_flow_payload,
    register_segment,
    segment_payload,
    segment_wrapper_payload,
    storage_allocation_payload,
    upload_allocated_object,
    video_flow_payload,
)

pytestmark = pytest.mark.tams_conformance


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

    invalid_tag_exists = client.get(
        "/sources",
        params={"tag_exists.editorial": "maybe"},
    )
    assert invalid_tag_exists.status_code == 400


def test_core_api_responses_validate_against_contract_models(
    client: TestClient,
) -> None:
    flow_id, source_id, _ = create_video_flow(client)
    object_id = register_segment(client, flow_id, object_id=f"bbc/{uuid4()}.ts")

    contract_models.Service.model_validate(client.get("/service").json())
    contract_models.FlowGet.model_validate(client.get(f"/flows/{flow_id}").json())
    contract_models.Source.model_validate(client.get(f"/sources/{source_id}").json())
    contract_models.FlowSegment.model_validate(
        client.get(f"/flows/{flow_id}/segments").json()[0]
    )
    contract_models.Object.model_validate(client.get(f"/objects/{object_id}").json())


def test_flow_metadata_version_is_preserved_on_create_and_bumped_on_update(
    client: TestClient,
) -> None:
    flow_id = uuid4()
    source_id = uuid4()
    created = client.put(
        f"/flows/{flow_id}",
        json=video_flow_payload(
            flow_id,
            source_id,
            metadata_version="upstream-version",
        ),
    )
    assert created.status_code == 201
    assert created.json()["metadata_version"] == "upstream-version"

    updated = client.put(f"/flows/{flow_id}/label", json="new label")
    fetched = client.get(f"/flows/{flow_id}")

    assert updated.status_code == 204
    assert fetched.status_code == 200
    assert fetched.json()["metadata_version"] != "upstream-version"
    assert fetched.json()["updated_by"]


@pytest.mark.tamoss_extension
def test_list_flows_can_include_timerange_compatibility_extension(
    client: TestClient,
) -> None:
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


@pytest.mark.tamoss_extension
def test_empty_timerange_query_discovers_empty_flows_for_retention_cleanup(
    client: TestClient,
) -> None:
    segmented_flow_id, _, _ = create_video_flow(client)
    empty_flow_id, _, _ = create_video_flow(client)
    register_segment(client, segmented_flow_id, timerange="[0:0_10:0)")

    listed = client.get(
        "/flows",
        params={"timerange": "()", "include_timerange": "true"},
    )

    assert listed.status_code == 200
    payload = {item["id"]: item for item in listed.json()}
    assert set(payload) == {str(empty_flow_id)}
    assert payload[str(empty_flow_id)]["timerange"] == "()"

    head = client.head("/flows", params={"timerange": "()"})
    assert head.status_code == 200


def test_retention_tags_are_discoverable_with_tag_exists_filters(
    client: TestClient,
) -> None:
    segment_retention_flow_id, _, _ = create_video_flow(
        client,
        tags={"segment_retention_offset": "3600:0"},
    )
    flow_retention_flow_id, _, _ = create_video_flow(
        client,
        tags={"flow_retention_offset": "86400:0"},
    )
    regular_flow_id, _, _ = create_video_flow(client)

    segment_retention = client.get(
        "/flows",
        params={"tag_exists.segment_retention_offset": "true"},
    )
    flow_retention = client.get(
        "/flows",
        params={"tag_exists.flow_retention_offset": "true"},
    )
    no_segment_retention = client.get(
        "/flows",
        params={"tag_exists.segment_retention_offset": "false"},
    )

    assert segment_retention.status_code == 200
    assert [item["id"] for item in segment_retention.json()] == [
        str(segment_retention_flow_id)
    ]
    assert flow_retention.status_code == 200
    assert [item["id"] for item in flow_retention.json()] == [
        str(flow_retention_flow_id)
    ]
    assert no_segment_retention.status_code == 200
    no_segment_retention_ids = {item["id"] for item in no_segment_retention.json()}
    assert str(segment_retention_flow_id) not in no_segment_retention_ids
    assert {
        str(flow_retention_flow_id),
        str(regular_flow_id),
    } <= no_segment_retention_ids


@pytest.mark.tamoss_extension
def test_collection_flows_include_child_timerange_compatibility_extension(
    client: TestClient,
) -> None:
    first_flow_id = uuid4()
    first_source_id = uuid4()
    second_flow_id = uuid4()
    second_source_id = uuid4()
    parent_flow_id = uuid4()
    parent_source_id = uuid4()

    assert (
        client.put(
            f"/flows/{first_flow_id}",
            json=video_flow_payload(first_flow_id, first_source_id),
        ).status_code
        == 201
    )
    assert (
        client.put(
            f"/flows/{second_flow_id}",
            json=video_flow_payload(second_flow_id, second_source_id),
        ).status_code
        == 201
    )
    assert (
        client.put(
            f"/flows/{parent_flow_id}",
            json=multi_flow_payload(parent_flow_id, parent_source_id),
        ).status_code
        == 201
    )
    register_segment(client, first_flow_id, timerange="[0:0_10:0)")
    register_segment(client, second_flow_id, timerange="[20:0_30:0)")

    collection = client.put(
        f"/flows/{parent_flow_id}/flow_collection",
        json=[
            flow_collection_item(first_flow_id, role="video"),
            flow_collection_item(second_flow_id, role="audio"),
        ],
    )
    assert collection.status_code == 204

    detail = client.get(
        f"/flows/{parent_flow_id}", params={"include_timerange": "true"}
    )
    assert detail.status_code == 200
    assert detail.json()["timerange"] == "[0:0_30:0)"

    listed = client.get(
        "/flows",
        params={"source_id": str(parent_source_id), "include_timerange": "true"},
    )
    assert listed.status_code == 200
    assert listed.json()[0]["timerange"] == "[0:0_30:0)"


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
    flow_label_head = client.head(f"/flows/{flow_id}/label")
    assert flow_label_head.status_code == 200
    assert flow_label_head.content == b""
    invalid_flow_label_query = client.get(
        f"/flows/{flow_id}/label", params={"include_timerange": "true"}
    )
    assert invalid_flow_label_query.status_code == 400
    flow_label_delete = client.delete(f"/flows/{flow_id}/label")
    assert flow_label_delete.status_code == 204
    missing_flow_label = client.get(f"/flows/{flow_id}/label")
    assert missing_flow_label.status_code == 200
    assert missing_flow_label.json() == ""

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

    tag_put = client.put(f"/flows/{flow_id}/tags/editorial/role", json=["clean", "tx"])
    assert tag_put.status_code == 204
    flow_tag = client.get(f"/flows/{flow_id}/tags/editorial/role")
    assert flow_tag.status_code == 200
    assert flow_tag.json() == ["clean", "tx"]
    tag_delete = client.delete(f"/flows/{flow_id}/tags/editorial/role")
    assert tag_delete.status_code == 204

    source_label = client.put(f"/sources/{source_id}/label", json="source label")
    assert source_label.status_code == 204
    assert client.get(f"/sources/{source_id}/label").json() == "source label"
    source_label_head = client.head(f"/sources/{source_id}/label")
    assert source_label_head.status_code == 200
    assert source_label_head.content == b""
    source_label_delete = client.delete(f"/sources/{source_id}/label")
    assert source_label_delete.status_code == 204
    missing_source_label = client.get(f"/sources/{source_id}/label")
    assert missing_source_label.status_code == 200
    assert missing_source_label.json() == ""
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
        json=[flow_collection_item(child_flow_id)],
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
    missing_essence_payload = video_flow_payload(missing_essence_flow_id, uuid4())
    missing_essence_payload.pop("essence_parameters")
    missing_essence = client.put(
        f"/flows/{missing_essence_flow_id}",
        json=missing_essence_payload,
    )
    assert missing_essence.status_code == 400
    assert_bbc_error(missing_essence.json())

    mismatch_flow_id = uuid4()
    mismatch = client.put(
        f"/flows/{mismatch_flow_id}",
        json=video_flow_payload(mismatch_flow_id, image_source_id),
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
        json=storage_allocation_payload([object_one, object_two]),
    )
    assert allocated.status_code == 201
    assert [item["object_id"] for item in allocated.json()["media_objects"]] == [
        object_one,
        object_two,
    ]
    upload_allocated_object(client, object_one)
    upload_allocated_object(client, object_two)

    segments = client.post(
        f"/flows/{flow_id}/segments",
        json=[
            segment_payload(object_one),
            segment_payload(
                object_two,
                "[10:0_20:0)",
                object_timerange="[100:0_110:0)",
                sample_offset=10,
                sample_count=250,
                key_frame_count=5,
            ),
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
    assert payload[0]["sample_offset"] == 10
    assert payload[0]["sample_count"] == 250
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
        json=segment_wrapper_payload([segment_payload(object_one, "[20:0_30:0)")]),
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
    upload_allocated_object(client, object_one)
    upload_allocated_object(client, object_two)

    initial = client.post(
        f"/flows/{flow_id}/segments",
        json=segment_payload(object_one),
    )
    assert initial.status_code == 201

    ingested_before = REGISTRY.get_sample_value("tamoss_segments_ingested_total") or 0.0
    failed_before = (
        REGISTRY.get_sample_value("tamoss_segment_ingest_failed_total") or 0.0
    )
    response = client.post(
        f"/flows/{flow_id}/segments",
        json=[
            segment_payload(object_two, "[5:0_15:0)"),
            segment_payload(object_two, "[10:0_20:0)"),
        ],
    )
    assert response.status_code == 200
    assert response.json()["failed_segments"][0]["object_id"] == object_two
    assert response.json()["failed_segments"][0]["error"]["type"] == "TAMSError"

    # One of the two bulk segments registers, the overlapping one fails — the
    # media-load counters count segments, not the single HTTP request.
    ingested_after = REGISTRY.get_sample_value("tamoss_segments_ingested_total") or 0.0
    failed_after = (
        REGISTRY.get_sample_value("tamoss_segment_ingest_failed_total") or 0.0
    )
    assert ingested_after - ingested_before == 1
    assert failed_after - failed_before == 1


def test_segment_timerange_validation_follows_bbc_boundary_rules(
    client: TestClient,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    object_id = f"bbc/{uuid4()}.ts"
    allocate_objects(client, flow_id, [object_id])
    upload_allocated_object(client, object_id)

    for timerange in ["(0:0_10:0]", "[5:0_5:0)"]:
        response = client.post(
            f"/flows/{flow_id}/segments",
            json=segment_payload(object_id, timerange),
        )
        assert response.status_code == 400

    inclusive_last_duration = client.post(
        f"/flows/{flow_id}/segments",
        json=segment_payload(
            object_id,
            "[0:0_10:0]",
            last_duration="0:40000000",
        ),
    )
    assert inclusive_last_duration.status_code == 400


def test_segment_registration_derives_object_timerange_from_ts_offset(
    client: TestClient,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    object_id = f"bbc/{uuid4()}.ts"
    allocate_objects(client, flow_id, [object_id])
    upload_allocated_object(client, object_id)

    registered = client.post(
        f"/flows/{flow_id}/segments",
        json=segment_payload(
            object_id,
            "[10:0_20:0)",
            ts_offset="10:0",
        ),
    )
    listed = client.get(
        f"/flows/{flow_id}/segments",
        params={"include_object_timerange": "true"},
    )
    media_object = client.get(f"/objects/{object_id}")

    assert registered.status_code == 201
    assert listed.status_code == 200
    assert listed.json()[0]["object_timerange"] == "[0:0_10:0)"
    assert media_object.status_code == 200
    assert media_object.json()["timerange"] == "[0:0_10:0)"


def test_segment_overlap_and_queries_respect_timerange_boundaries(
    client: TestClient,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    inclusive_object = register_segment(
        client,
        flow_id,
        object_id=f"bbc/{uuid4()}.ts",
        timerange="[0:0_10:0]",
    )
    adjacent_object = f"bbc/{uuid4()}.ts"
    allocate_objects(client, flow_id, [adjacent_object])
    upload_allocated_object(client, adjacent_object)

    overlapping = client.post(
        f"/flows/{flow_id}/segments",
        json=segment_payload(adjacent_object, "[10:0_20:0)"),
    )
    assert overlapping.status_code == 400
    assert overlapping.json()["summary"] == SEGMENT_OVERLAP_MESSAGE

    includes_endpoint = client.get(
        f"/flows/{flow_id}/segments", params={"timerange": "[10:0]"}
    )
    assert includes_endpoint.status_code == 200
    assert [item["object_id"] for item in includes_endpoint.json()] == [
        inclusive_object
    ]

    excludes_endpoint = client.get(
        f"/flows/{flow_id}/segments", params={"timerange": "(10:0_20:0)"}
    )
    assert excludes_endpoint.status_code == 200
    assert excludes_endpoint.json() == []


@pytest.mark.tamoss_extension
def test_segment_coverage_gap_header_is_separate_from_flow_extent(
    client: TestClient,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    register_segment(client, flow_id, timerange="[0:0_10:0)")
    register_segment(client, flow_id, timerange="[20:0_30:0)")

    listed = client.get(f"/flows/{flow_id}/segments")

    assert listed.status_code == 200
    assert listed.headers["x-paging-timerange"] == "[0:0_30:0)"
    assert listed.headers["x-tamoss-coverage-gaps"] == "[10:0_20:0)"


def test_read_only_flow_rejects_writes_with_403(client: TestClient) -> None:
    """bbc-id: semantic.flow.read_only_write_rejection"""
    flow_id, _source_id, payload = create_video_flow(client)

    assert client.put(f"/flows/{flow_id}/read_only", json=True).status_code == 204
    assert client.get(f"/flows/{flow_id}/read_only").json() is True

    rejected = {
        "put_flow": client.put(f"/flows/{flow_id}", json=payload),
        "put_description": client.put(f"/flows/{flow_id}/description", json="mutated"),
        "put_tag": client.put(f"/flows/{flow_id}/tags/probe", json="x"),
        "post_storage": client.post(
            f"/flows/{flow_id}/storage",
            json=storage_allocation_payload([f"bbc/{uuid4()}.ts"]),
        ),
        "delete_segments": client.delete(f"/flows/{flow_id}/segments"),
        "delete_flow": client.delete(f"/flows/{flow_id}"),
    }
    for name, response in rejected.items():
        assert response.status_code == 403, name
        assert_bbc_error(response.json(), "forbidden")

    assert client.put(f"/flows/{flow_id}/read_only", json=False).status_code == 204
    assert client.delete(f"/flows/{flow_id}").status_code in {202, 204}


def test_unset_label_and_description_read_as_empty_strings(
    client: TestClient,
) -> None:
    """bbc-id: semantic.flow.unset_string_properties_are_readable"""
    flow_id = uuid4()
    source_id = uuid4()
    payload = video_flow_payload(flow_id, source_id)
    payload.pop("label", None)
    payload.pop("description", None)
    assert client.put(f"/flows/{flow_id}", json=payload).status_code == 201

    for path in (
        f"/flows/{flow_id}/label",
        f"/flows/{flow_id}/description",
        f"/sources/{source_id}/label",
        f"/sources/{source_id}/description",
    ):
        response = client.get(path)
        assert response.status_code == 200, path
        assert response.json() == "", path


def test_flow_put_with_mismatched_body_id_returns_404(client: TestClient) -> None:
    """bbc-id: semantic.flow.path_id_authoritative"""
    payload = video_flow_payload(uuid4(), uuid4())
    response = client.put(f"/flows/{uuid4()}", json=payload)
    assert response.status_code == 404
    assert_bbc_error(response.json(), "not_found")
    assert response.json()["summary"] == "The requested Flow ID in the path is invalid."


def test_string_property_put_rejects_non_json_bodies(client: TestClient) -> None:
    """bbc-id: semantic.flow.property_bodies_must_be_json"""
    flow_id, source_id, _ = create_video_flow(client)
    for path in (
        f"/flows/{flow_id}/label",
        f"/flows/{flow_id}/description",
        f"/flows/{flow_id}/tags/probe",
        f"/sources/{source_id}/label",
        f"/sources/{source_id}/description",
        f"/sources/{source_id}/tags/probe",
    ):
        for content_type in ("text/plain", "application/x-www-form-urlencoded"):
            response = client.put(
                path,
                content=b"test this",
                headers={"Content-Type": content_type},
            )
            assert response.status_code == 400, (path, content_type)
            assert_bbc_error(response.json(), "bad_request")


def test_unset_numeric_flow_properties_read_as_null(client: TestClient) -> None:
    """bbc-id: semantic.flow.unset_numeric_properties_are_readable

    The spec reserves the 404 on these endpoints for a missing Flow and
    defines no unset form for the integer properties, so an existing Flow
    without a bit rate reads back as 200 null.
    """
    flow_id = uuid4()
    payload = video_flow_payload(flow_id, uuid4())
    payload.pop("avg_bit_rate", None)
    payload.pop("max_bit_rate", None)
    assert client.put(f"/flows/{flow_id}", json=payload).status_code == 201

    for name in ("avg_bit_rate", "max_bit_rate"):
        response = client.get(f"/flows/{flow_id}/{name}")
        assert response.status_code == 200, name
        assert response.json() is None, name


def test_malformed_flow_id_in_segments_path_returns_404(client: TestClient) -> None:
    """bbc-id: semantic.segments.malformed_flow_id_is_404

    The segments path documents only 404 for the Flow ID path parameter,
    so malformed IDs resolve to 404 rather than 400.
    """
    listed = client.get("/flows/not-a-uuid/segments")
    assert listed.status_code == 404
    assert_bbc_error(listed.json(), "not_found")
    assert listed.json()["summary"] == "The Flow ID in the path is invalid."

    posted = client.post(
        "/flows/not-a-uuid/segments",
        json=segment_payload("bbc/never-registered.ts"),
    )
    assert posted.status_code == 404

    deleted = client.delete("/flows/not-a-uuid/segments")
    assert deleted.status_code == 404
