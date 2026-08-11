from __future__ import annotations

from dataclasses import replace
from datetime import UTC, datetime, timedelta
from uuid import UUID, uuid4

import psycopg
import pytest
from tamoss.adapters.postgres import PostgresRepository
from tamoss.domain.exceptions import SegmentOverlapError
from tamoss.domain.listings import DeleteRequestSortBy, FlowSortBy, SourceSortBy
from tamoss.domain.model import (
    DeletionRequestRecord,
    FlowRecord,
    MediaObjectRecord,
    ObjectCleanupRecord,
    ObjectCopyRecord,
    ObjectInstance,
    ProfileRecord,
    SegmentRecord,
    SourceRecord,
    WebhookDeliveryRecord,
    WebhookRecord,
)
from tamoss.domain.segments import SegmentTimerangeBounds

from tests.adapters.postgres.support import (
    PRIMARY_BACKEND_ID,
    RecordingObjectStorage,
)
from tests.adapters.postgres.support import (
    identity as _identity,
)
from tests.adapters.postgres.support import (
    multi_flow_write as _multi_flow_write,
)
from tests.adapters.postgres.support import (
    use_cases as _use_cases,
)
from tests.adapters.postgres.support import (
    video_flow_write as _video_flow_write,
)

pytestmark = pytest.mark.needs_db


def test_repository_round_trips_profiles_flow_fields_and_init_links(
    postgres_repo: PostgresRepository,
) -> None:
    profile_id = uuid4()
    flow_id = uuid4()
    init_id = "bbc/adapter/init.mp4"
    media_id = "bbc/adapter/media.m4s"
    profile = ProfileRecord(
        id=profile_id,
        label="HD profile",
        flow_metadata={
            "format": "urn:x-nmos:format:video",
            "codec": "video/h264",
            "container": "video/mp4",
            "essence_parameters": {
                "frame_width": 1920,
                "frame_height": 1080,
                "init_segments": True,
            },
        },
        tags={"editorial_purpose": ["programme", "trailer"]},
    )
    assert postgres_repo.profile_repository.create_profile(profile) is True
    assert postgres_repo.profile_repository.create_profile(profile) is False
    profile_page = postgres_repo.profile_repository.list_profiles_page(
        format="urn:x-nmos:format:video",
        codec="video/h264",
        label="HD profile",
        page=None,
        limit=10,
    )
    assert profile_page.items == [profile]

    flow = FlowRecord(
        id=flow_id,
        source_id=uuid4(),
        format="urn:x-nmos:format:video",
        container="video/mp4",
        profile_id=profile_id,
        status="ingesting",
        init_segments=True,
        data={
            "label": "Profiled flow",
            "profile_id": str(profile_id),
            "status": "ingesting",
            **profile.flow_metadata,
        },
    )
    init_object = MediaObjectRecord(
        id=init_id,
        object_kind="init",
        content_type="video/mp4",
        referenced_by_flows={flow_id},
    )
    media_object = MediaObjectRecord(
        id=media_id,
        object_kind="media",
        content_type="video/iso.segment",
        timerange="[0:0_10:0)",
        init_object_id=init_id,
        referenced_by_flows={flow_id},
    )
    segment = SegmentRecord(
        flow_id=flow_id,
        object_id=media_id,
        init_object_id=init_id,
        timerange="[0:0_10:0)",
    )
    postgres_repo.segment_repository.save_registered_segments(
        flow=flow,
        media_objects=[media_object, init_object],
        segments=[segment],
    )

    flow_page = postgres_repo.flow_repository.list_flows_page(
        source_id=None,
        timerange_start=None,
        timerange_end=None,
        timerange_is_empty=False,
        timerange_is_point=False,
        format=None,
        profile_id=profile_id,
        status="ingesting",
        init_segments=True,
        collected_by_ids=None,
        top_level_only=False,
        sort_by=FlowSortBy.LABEL,
        reverse_order=False,
        codec=None,
        label=None,
        frame_width=None,
        frame_height=None,
        tag_values={},
        tag_exists={},
        page=None,
        limit=10,
    )
    assert [item.id for item in flow_page.items] == [flow_id]
    stored_objects = postgres_repo.object_repository.get_objects([media_id, init_id])
    assert stored_objects[media_id].init_object_id == init_id
    assert stored_objects[init_id].object_kind == "init"
    assert postgres_repo.segment_repository.list_segments_for_objects(
        flow_id=flow_id,
        object_ids={init_id},
    ) == [segment]


def test_repository_preserves_and_reads_operator_registered_storage_backends(
    postgres_repo: PostgresRepository,
    postgres_connection: psycopg.Connection,
) -> None:
    extra_backend_id = UUID("33333333-3333-4333-8333-333333333333")
    with postgres_connection.cursor() as cur:
        cur.execute(
            """
            INSERT INTO tamoss_storage_backends (
                id,
                label,
                provider,
                region,
                store_product,
                store_type,
                default_storage,
                bucket_name,
                endpoint_url,
                public_endpoint_url,
                record,
                updated_at
            )
            VALUES (
                %(id)s,
                'tamoss.us-east-1:s3:archive',
                'rustfs',
                'us-east-1',
                's3',
                'http_object_store',
                FALSE,
                'archive',
                'https://objects.internal.example.test',
                'https://objects.example.test',
                jsonb_build_object(
                    'id', %(id_text)s::text,
                    'label', 'tamoss.us-east-1:s3:archive',
                    'provider', 'rustfs',
                    'region', 'us-east-1',
                    'store_product', 's3',
                    'store_type', 'http_object_store',
                    'default_storage', FALSE,
                    'bucket_name', 'archive',
                    'endpoint_url', 'https://objects.internal.example.test',
                    'public_endpoint_url', 'https://objects.example.test'
                ),
                NOW()
            )
            """,
            {"id": extra_backend_id, "id_text": str(extra_backend_id)},
        )

    backends = postgres_repo.service_repository.list_storage_backends()
    backend_ids = {backend.id for backend in backends}

    assert PRIMARY_BACKEND_ID in backend_ids
    assert extra_backend_id in backend_ids
    assert (
        postgres_repo.storage_repository.default_storage_backend().id
        == PRIMARY_BACKEND_ID
    )
    extra = postgres_repo.storage_repository.get_storage_backend(extra_backend_id)
    assert extra is not None
    assert extra.provider == "rustfs"
    assert extra.bucket_name == "archive"


def test_repository_round_trips_bbc_resources_and_segment_timerange_bounds(
    postgres_repo: PostgresRepository,
    postgres_connection: psycopg.Connection,
) -> None:
    flow_id = uuid4()
    source_id = uuid4()
    primary = postgres_repo.storage_repository.default_storage_backend()
    assert primary is not None

    postgres_repo.source_repository.save_source(
        SourceRecord(
            id=source_id,
            format="urn:x-nmos:format:video",
            label="Adapter source",
            description="BBC shaped source",
            tags={"suite": "adapter", "roles": ["video", "primary"]},
        )
    )
    postgres_repo.flow_repository.save_flow(
        FlowRecord(
            id=flow_id,
            source_id=source_id,
            format="urn:x-nmos:format:video",
            container="video/mp2t",
            read_only=True,
            tags={"suite": "adapter"},
            data={
                "label": "Adapter flow",
                "description": "BBC shaped flow",
                "codec": "video/h264",
                "essence_parameters": {
                    "frame_width": 1920,
                    "frame_height": 1080,
                    "frame_rate": {"numerator": 25, "denominator": 1},
                },
                "avg_bit_rate": 5000,
                "max_bit_rate": 8000,
                "flow_collection": [{"id": str(uuid4()), "role": "video"}],
            },
        )
    )
    postgres_repo.object_repository.save_object(
        MediaObjectRecord(
            id="bbc/adapter/segment-001.ts",
            timerange="[0:0_20:0)",
            first_referenced_by_flow=flow_id,
            referenced_by_flows={flow_id},
            instances=[
                ObjectInstance(
                    storage_backend=primary,
                    url="https://objects.example.test/bbc/adapter/segment-001.ts",
                    label=primary.label,
                    controlled=True,
                )
            ],
            key_frame_count=2,
            bytes_written=12345,
        )
    )

    postgres_repo.segment_repository.append_segment(
        SegmentRecord(
            flow_id=flow_id,
            object_id="bbc/adapter/segment-020.ts",
            timerange="[10:0_20:0)",
        )
    )
    postgres_repo.segment_repository.append_segment(
        SegmentRecord(
            flow_id=flow_id,
            object_id="bbc/adapter/segment-010.ts",
            timerange="[0:0_10:0)",
            object_timerange="[0:0_10:0)",
            ts_offset="0:0",
            sample_offset=0,
            sample_count=250,
            key_frame_count=1,
        )
    )
    postgres_repo.segment_repository.append_segment(
        SegmentRecord(
            flow_id=flow_id,
            object_id="bbc/adapter/still-frame.ts",
            timerange="[5:0_5:0]",
        )
    )

    assert (
        postgres_repo.source_repository.get_source(source_id).description
        == "BBC shaped source"
    )
    flow = postgres_repo.flow_repository.get_flow(flow_id)
    assert flow is not None
    assert flow.read_only is True
    assert flow.data["essence_parameters"]["frame_rate"]["numerator"] == 25

    media_object = postgres_repo.object_repository.get_object(
        "bbc/adapter/segment-001.ts"
    )
    assert media_object is not None
    assert media_object.referenced_by_flows == {flow_id}
    assert media_object.instances[0].storage_backend == primary

    segments = postgres_repo.segment_repository.list_segments(flow_id)
    assert [segment.object_id for segment in segments] == [
        "bbc/adapter/still-frame.ts",
        "bbc/adapter/segment-010.ts",
        "bbc/adapter/segment-020.ts",
    ]
    assert segments[1].sample_count == 250

    with postgres_connection.cursor() as cur:
        cur.execute(
            """
            SELECT timerange, timerange_start, timerange_end
            FROM tamoss_segments
            WHERE flow_id = %s
            ORDER BY timerange_end, timerange_start, object_id
            """,
            (flow_id,),
        )
        persisted_bounds = cur.fetchall()

    assert persisted_bounds == [
        ("[5:0_5:0]", 5_000_000_000, 5_000_000_001),
        ("[0:0_10:0)", 0, 10_000_000_000),
        ("[10:0_20:0)", 10_000_000_000, 20_000_000_000),
    ]


def test_repository_lists_segments_with_database_paging_and_filters(
    postgres_repo: PostgresRepository,
) -> None:
    flow_id = uuid4()
    postgres_repo.flow_repository.save_flow(
        FlowRecord(
            id=flow_id,
            data={},
            source_id=uuid4(),
            format="urn:x-nmos:format:video",
            container="video/mp2t",
        )
    )
    for object_id, timerange in [
        ("bbc/adapter/segment-000.ts", "[0:0_10:0)"),
        ("bbc/adapter/segment-010.ts", "[10:0_20:0)"),
        ("bbc/adapter/marker-015.ts", "[15:0_15:0]"),
        ("bbc/adapter/segment-020.ts", "[20:0_30:0)"),
    ]:
        postgres_repo.segment_repository.append_segment(
            SegmentRecord(
                flow_id=flow_id,
                object_id=object_id,
                timerange=timerange,
            )
        )

    first_page = postgres_repo.segment_repository.list_segments_page(
        flow_id=flow_id,
        object_id=None,
        timerange_start=None,
        timerange_end=None,
        timerange_is_empty=False,
        timerange_is_point=False,
        reverse_order=False,
        page=None,
        limit=2,
    )
    assert [segment.object_id for segment in first_page.items] == [
        "bbc/adapter/segment-000.ts",
        "bbc/adapter/marker-015.ts",
    ]
    assert first_page.next_page == "2"
    assert first_page.timerange == "[0:0_30:0)"

    point_page = postgres_repo.segment_repository.list_segments_page(
        flow_id=flow_id,
        object_id=None,
        timerange_start=15_000_000_000,
        timerange_end=15_000_000_000,
        timerange_is_empty=False,
        timerange_is_point=True,
        reverse_order=False,
        page=None,
        limit=10,
    )
    assert [segment.object_id for segment in point_page.items] == [
        "bbc/adapter/marker-015.ts",
        "bbc/adapter/segment-010.ts",
    ]
    assert point_page.timerange == "[10:0_20:0)"

    overlapping = postgres_repo.segment_repository.list_segments_overlapping(
        flow_id=flow_id,
        timeranges=[
            SegmentTimerangeBounds(
                start=15_000_000_000,
                end=15_000_000_000,
                is_point=True,
            )
        ],
    )
    assert [segment.object_id for segment in overlapping] == [
        "bbc/adapter/marker-015.ts",
        "bbc/adapter/segment-010.ts",
    ]

    filtered_page = postgres_repo.segment_repository.list_segments_page(
        flow_id=flow_id,
        object_id="bbc/adapter/segment-020.ts",
        timerange_start=15_000_000_000,
        timerange_end=25_000_000_000,
        timerange_is_empty=False,
        timerange_is_point=False,
        reverse_order=True,
        page=None,
        limit=10,
    )
    assert [segment.object_id for segment in filtered_page.items] == [
        "bbc/adapter/segment-020.ts"
    ]
    assert filtered_page.next_page is None
    assert filtered_page.timerange == "[20:0_30:0)"


def test_repository_lists_flows_page_with_sql_filters_and_relationships(
    postgres_repo: PostgresRepository,
) -> None:
    child_flow_id = UUID("11111111-1111-4111-8111-111111111101")
    parent_flow_id = UUID("11111111-1111-4111-8111-111111111102")
    idle_flow_id = UUID("11111111-1111-4111-8111-111111111103")
    child_source_id = UUID("11111111-1111-4111-8111-111111111201")
    parent_source_id = UUID("11111111-1111-4111-8111-111111111202")
    idle_source_id = UUID("11111111-1111-4111-8111-111111111203")

    postgres_repo.flow_repository.save_flow(
        FlowRecord(
            id=child_flow_id,
            source_id=child_source_id,
            format="urn:x-nmos:format:video",
            container="video/mp2t",
            tags={"editorial": ["news", "main"], "suite": "pushdown"},
            data={
                "label": "programme",
                "codec": "video/h264",
                "essence_parameters": {
                    "frame_width": 1920,
                    "frame_height": 1080,
                },
            },
        )
    )
    postgres_repo.flow_repository.save_flow(
        FlowRecord(
            id=parent_flow_id,
            source_id=parent_source_id,
            format="urn:x-nmos:format:multi",
            container="video/mp2t",
            data={
                "flow_collection": [
                    {"id": str(child_flow_id), "role": "video"},
                ],
            },
        )
    )
    postgres_repo.flow_repository.save_flow(
        FlowRecord(
            id=idle_flow_id,
            source_id=idle_source_id,
            format="urn:x-nmos:format:video",
            container="video/mp2t",
            tags={"editorial": "sport"},
            data={
                "label": "archive",
                "codec": "video/h264",
                "essence_parameters": {
                    "frame_width": 1920,
                    "frame_height": 1080,
                },
            },
        )
    )
    postgres_repo.segment_repository.append_segment(
        SegmentRecord(
            flow_id=child_flow_id,
            object_id="bbc/adapter/programme.ts",
            timerange="[0:0_10:0)",
        )
    )

    flow_page = postgres_repo.flow_repository.list_flows_page(
        source_id=child_source_id,
        timerange_start=5_000_000_000,
        timerange_end=6_000_000_000,
        timerange_is_empty=False,
        timerange_is_point=False,
        format="urn:x-nmos:format:video",
        profile_id=None,
        status=None,
        init_segments=None,
        collected_by_ids=None,
        top_level_only=False,
        sort_by=FlowSortBy.CREATED,
        reverse_order=False,
        codec="video/h264",
        label="programme",
        frame_width=1920,
        frame_height=1080,
        tag_values={"editorial": {"main"}},
        tag_exists={"suite": True, "missing": False},
        page=None,
        limit=10,
    )

    assert [flow.id for flow in flow_page.items] == [child_flow_id]
    assert flow_page.items[0].data["collected_by"] == [str(parent_flow_id)]
    assert flow_page.next_page is None

    empty_timerange_page = postgres_repo.flow_repository.list_flows_page(
        source_id=idle_source_id,
        timerange_start=None,
        timerange_end=None,
        timerange_is_empty=True,
        timerange_is_point=False,
        format=None,
        profile_id=None,
        status=None,
        init_segments=None,
        collected_by_ids=None,
        top_level_only=False,
        sort_by=FlowSortBy.CREATED,
        reverse_order=False,
        codec=None,
        label=None,
        frame_width=None,
        frame_height=None,
        tag_values={},
        tag_exists={},
        page=None,
        limit=10,
    )
    assert [flow.id for flow in empty_timerange_page.items] == [idle_flow_id]

    timeranges = postgres_repo.flow_repository.flow_timeranges(
        [child_flow_id, idle_flow_id]
    )
    assert timeranges == {
        child_flow_id: "[0:0_10:0)",
        idle_flow_id: "()",
    }

    relationships = postgres_repo.source_repository.source_relationships_for(
        [child_source_id]
    )
    assert relationships[child_source_id].collected_by == [parent_source_id]
    assert relationships[child_source_id].source_collection == []


def test_repository_pushes_source_webhook_and_object_flow_tag_filters(
    postgres_repo: PostgresRepository,
) -> None:
    source_id = UUID("11111111-1111-4111-8111-111111111301")
    other_source_id = UUID("11111111-1111-4111-8111-111111111302")
    flow_id = UUID("11111111-1111-4111-8111-111111111401")
    other_flow_id = UUID("11111111-1111-4111-8111-111111111402")
    webhook_id = UUID("11111111-1111-4111-8111-111111111501")
    other_webhook_id = UUID("11111111-1111-4111-8111-111111111502")

    postgres_repo.source_repository.save_source(
        SourceRecord(
            id=source_id,
            format="urn:x-nmos:format:video",
            label="programme",
            tags={"role": ["primary", "studio"]},
        )
    )
    postgres_repo.source_repository.save_source(
        SourceRecord(
            id=other_source_id,
            format="urn:x-nmos:format:video",
            label="programme",
            tags={"role": "backup"},
        )
    )
    postgres_repo.flow_repository.save_flow(
        FlowRecord(
            id=flow_id,
            source_id=source_id,
            format="urn:x-nmos:format:video",
            container="video/mp2t",
            tags={"role": ["primary", "main"]},
            data={},
        )
    )
    postgres_repo.flow_repository.save_flow(
        FlowRecord(
            id=other_flow_id,
            source_id=other_source_id,
            format="urn:x-nmos:format:video",
            container="video/mp2t",
            tags={"role": "backup"},
            data={},
        )
    )
    postgres_repo.webhook_repository.save_webhook(
        WebhookRecord(
            id=webhook_id,
            data={"url": "https://webhook.example.test/tamoss"},
            status="created",
            tags={"env": ["prod", "blue"]},
        )
    )
    postgres_repo.webhook_repository.save_webhook(
        WebhookRecord(
            id=other_webhook_id,
            data={"url": "https://webhook.example.test/other"},
            status="created",
            tags={"env": "dev"},
        )
    )

    source_page = postgres_repo.source_repository.list_sources_page(
        label="programme",
        format="urn:x-nmos:format:video",
        collected_by_ids=None,
        top_level_only=False,
        sort_by=SourceSortBy.CREATED,
        reverse_order=False,
        tag_values={"role": {"studio"}},
        tag_exists={"missing": False},
        page=None,
        limit=10,
    )
    assert [source.id for source in source_page.items] == [source_id]

    webhook_page = postgres_repo.webhook_repository.list_webhooks_page(
        tag_values={"env": {"blue"}},
        tag_exists={},
        reverse_order=False,
        page=None,
        limit=10,
    )
    assert [webhook.id for webhook in webhook_page.items] == [webhook_id]

    flow_id_page = postgres_repo.flow_repository.list_flow_ids_matching_tags_page(
        flow_ids=[other_flow_id, flow_id],
        tag_values={"role": {"main"}},
        tag_exists={"missing": False},
        page=None,
        limit=10,
    )
    assert flow_id_page.items == [flow_id]


def test_repository_saves_registered_segment_batch(
    postgres_repo: PostgresRepository,
) -> None:
    flow_id = uuid4()
    source_id = uuid4()
    now = datetime.now(UTC)
    flow = FlowRecord(
        id=flow_id,
        data={"label": "Registered batch"},
        source_id=source_id,
        format="urn:x-nmos:format:video",
        container="video/mp2t",
        segments_updated=now,
    )
    media_object = MediaObjectRecord(
        id="bbc/adapter/batch-000.ts",
        timerange="[0:0_10:0)",
        first_referenced_by_flow=flow_id,
        referenced_by_flows={flow_id},
    )
    segment = SegmentRecord(
        flow_id=flow_id,
        object_id=media_object.id,
        timerange="[0:0_10:0)",
    )

    postgres_repo.segment_repository.save_registered_segments(
        flow=flow,
        media_objects=[media_object],
        segments=[segment],
    )

    saved_flow = postgres_repo.flow_repository.get_flow(flow_id)
    assert saved_flow is not None
    assert saved_flow.segments_updated == now
    assert postgres_repo.object_repository.get_object(media_object.id) == media_object
    assert (
        postgres_repo.segment_repository.list_segments(flow_id)[0].object_id
        == media_object.id
    )


def test_repository_create_object_is_conflict_safe(
    postgres_repo: PostgresRepository,
) -> None:
    original = MediaObjectRecord(id="bbc/adapter/allocated.ts")
    replacement = MediaObjectRecord(
        id=original.id,
        timerange="[0:0_10:0)",
    )

    assert postgres_repo.storage_repository.create_object(original) is True
    assert postgres_repo.storage_repository.create_object(replacement) is False

    saved = postgres_repo.object_repository.get_object(original.id)
    assert saved is not None
    assert saved.timerange is None


def test_repository_unit_of_work_rolls_back_on_error(
    postgres_repo: PostgresRepository,
    postgres_connection: psycopg.Connection,
) -> None:
    flow_id = uuid4()
    object_id = "bbc/adapter/rollback.ts"

    with pytest.raises(psycopg.errors.DivisionByZero), postgres_repo.unit_of_work():
        postgres_repo.flow_repository.save_flow(
            FlowRecord(
                id=flow_id,
                data={"label": "Rollback"},
                source_id=uuid4(),
                format="urn:x-nmos:format:video",
                container="video/mp2t",
            )
        )
        postgres_repo.object_repository.save_object(MediaObjectRecord(id=object_id))
        postgres_connection.execute("SELECT 1 / 0")

    assert postgres_repo.flow_repository.get_flow(flow_id) is None
    assert postgres_repo.object_repository.get_object(object_id) is None


def test_repository_rejects_overlapping_registered_segments_atomically(
    postgres_repo: PostgresRepository,
) -> None:
    flow_id = uuid4()
    flow = FlowRecord(
        id=flow_id,
        data={"label": "Overlap"},
        source_id=uuid4(),
        format="urn:x-nmos:format:video",
        container="video/mp2t",
    )
    postgres_repo.segment_repository.save_registered_segments(
        flow=flow,
        media_objects=[MediaObjectRecord(id="bbc/adapter/first.ts")],
        segments=[
            SegmentRecord(
                flow_id=flow_id,
                object_id="bbc/adapter/first.ts",
                timerange="[0:0_10:0)",
            )
        ],
    )

    with pytest.raises(SegmentOverlapError):
        postgres_repo.segment_repository.save_registered_segments(
            flow=replace(flow, data={"label": "Should roll back"}),
            media_objects=[MediaObjectRecord(id="bbc/adapter/second.ts")],
            segments=[
                SegmentRecord(
                    flow_id=flow_id,
                    object_id="bbc/adapter/second.ts",
                    timerange="[5:0_15:0)",
                )
            ],
        )

    assert postgres_repo.object_repository.get_object("bbc/adapter/second.ts") is None
    assert postgres_repo.flow_repository.get_flow(flow_id).data["label"] == "Overlap"
    assert [
        segment.object_id
        for segment in postgres_repo.segment_repository.list_segments(flow_id)
    ] == ["bbc/adapter/first.ts"]


def test_repository_loads_media_objects_in_bulk(
    postgres_repo: PostgresRepository,
) -> None:
    flow_id = uuid4()
    postgres_repo.object_repository.save_object(
        MediaObjectRecord(
            id="bbc/adapter/object-a.ts",
            timerange="[0:0_10:0)",
            first_referenced_by_flow=flow_id,
            referenced_by_flows={flow_id},
        )
    )
    postgres_repo.object_repository.save_object(
        MediaObjectRecord(
            id="bbc/adapter/object-b.ts",
            timerange="[10:0_20:0)",
            first_referenced_by_flow=flow_id,
            referenced_by_flows={flow_id},
        )
    )

    objects_by_id = postgres_repo.object_repository.get_objects(
        [
            "bbc/adapter/object-b.ts",
            "bbc/adapter/missing.ts",
            "bbc/adapter/object-a.ts",
        ]
    )

    assert set(objects_by_id) == {
        "bbc/adapter/object-a.ts",
        "bbc/adapter/object-b.ts",
    }
    assert objects_by_id["bbc/adapter/object-a.ts"].referenced_by_flows == {flow_id}


def test_use_cases_project_flow_collection_relationships_with_postgres(
    postgres_repo: PostgresRepository,
) -> None:
    use_cases = _use_cases(postgres_repo)
    identity = _identity()
    parent_flow_id = UUID("00000000-0000-4000-8000-000000000100")
    parent_source_id = UUID("00000000-0000-4000-8000-000000000101")
    child_flow_id = UUID("00000000-0000-4000-8000-000000000109")
    child_source_id = UUID("00000000-0000-4000-8000-000000000109")
    audio_flow_id = UUID("00000000-0000-4000-8000-000000000102")
    audio_source_id = UUID("00000000-0000-4000-8000-000000000102")

    use_cases.flows.put_flow(
        flow_id=child_flow_id,
        flow=_video_flow_write(child_flow_id, child_source_id),
        identity=identity,
    )
    use_cases.flows.put_flow(
        flow_id=audio_flow_id,
        flow=_video_flow_write(audio_flow_id, audio_source_id),
        identity=identity,
    )
    use_cases.flows.put_flow(
        flow_id=parent_flow_id,
        flow=_multi_flow_write(parent_flow_id, parent_source_id),
        identity=identity,
    )

    use_cases.flows.set_flow_collection(
        flow_id=parent_flow_id,
        collection=[
            {"id": str(child_flow_id)},
            {"id": str(audio_flow_id), "role": "audio"},
        ],
        identity=identity,
    )

    assert use_cases.flows.get_flow_collection(parent_flow_id) == [
        {"id": str(child_flow_id)},
        {"id": str(audio_flow_id), "role": "audio"},
    ]
    relationships = use_cases.sources.source_relationships()
    assert relationships[parent_source_id].source_collection == [
        {"id": str(child_source_id)},
        {"id": str(audio_source_id), "role": "audio"},
    ]
    assert relationships[child_source_id].collected_by == [parent_source_id]
    assert relationships[audio_source_id].collected_by == [parent_source_id]
    scoped_relationships = use_cases.sources.source_relationships(
        [parent_source_id, child_source_id, audio_source_id]
    )
    assert scoped_relationships[parent_source_id].source_collection == [
        {"id": str(child_source_id)},
        {"id": str(audio_source_id), "role": "audio"},
    ]
    assert use_cases.flows.get_flow(child_flow_id, include_collected_by=True).data[
        "collected_by"
    ] == [str(parent_flow_id)]

    use_cases.flows.delete_flow_collection(flow_id=parent_flow_id, identity=identity)

    assert use_cases.flows.get_flow_collection(parent_flow_id) == []
    assert (
        "collected_by"
        not in use_cases.flows.get_flow(child_flow_id, include_collected_by=True).data
    )
    assert (
        "collected_by"
        not in use_cases.flows.get_flow(audio_flow_id, include_collected_by=True).data
    )


def test_use_cases_process_segment_delete_request_with_postgres(
    postgres_repo: PostgresRepository,
) -> None:
    object_storage = RecordingObjectStorage()
    use_cases = _use_cases(postgres_repo, object_storage=object_storage)
    identity = _identity()
    flow_id = uuid4()
    source_id = uuid4()
    object_id = "bbc/adapter/delete-me.ts"
    backend = postgres_repo.storage_repository.default_storage_backend()
    assert backend is not None

    use_cases.flows.put_flow(
        flow_id=flow_id,
        flow=_video_flow_write(flow_id, source_id),
        identity=identity,
    )
    postgres_repo.object_repository.save_object(
        MediaObjectRecord(
            id=object_id,
            timerange="[0:0_10:0)",
            first_referenced_by_flow=flow_id,
            referenced_by_flows={flow_id},
            instances=[
                ObjectInstance(
                    storage_backend=backend,
                    url="https://objects.example.test/bbc/adapter/delete-me.ts",
                    label=backend.label,
                    controlled=True,
                    presigned=True,
                )
            ],
        )
    )
    postgres_repo.segment_repository.append_segment(
        SegmentRecord(
            flow_id=flow_id,
            object_id=object_id,
            timerange="[0:0_10:0)",
        )
    )

    request = use_cases.deletion.delete_segments(
        flow_id=flow_id,
        timerange="[0:0_10:0)",
        object_id=None,
        identity=identity,
    )

    assert request is not None
    assert request.status == "created"
    assert postgres_repo.segment_repository.list_segments(flow_id)

    processed = use_cases.deletion.process_pending_delete_requests(
        max_requests=1,
        worker_id="postgres-delete-worker",
        lease_seconds=30,
    )

    saved_request = postgres_repo.deletion_repository.get_delete_request(request.id)
    cleanups = postgres_repo.deletion_repository.list_object_cleanups(
        delete_request_id=request.id
    )
    assert processed == 1
    assert saved_request is not None
    assert saved_request.status == "done"
    assert saved_request.timerange_remaining is None
    assert saved_request.claimed_by is None
    assert postgres_repo.segment_repository.list_segments(flow_id) == []
    assert postgres_repo.object_repository.get_object(object_id) is None
    assert len(cleanups) == 1
    assert cleanups[0].status == "done"
    assert cleanups[0].attempt_count == 1
    assert object_storage.deleted == [(backend.id, object_id)]


def test_delete_request_created_sort_uses_persisted_resource_timestamp(
    postgres_repo: PostgresRepository,
    postgres_connection: psycopg.Connection,
) -> None:
    base = datetime(2026, 8, 1, tzinfo=UTC)
    requests = [
        DeletionRequestRecord(
            id=UUID(f"00000000-0000-4000-8000-{index:012d}"),
            flow_id=uuid4(),
            timerange_to_delete="[0:0_10:0)",
            delete_flow=False,
            status="created",
            created=base + timedelta(hours=offset),
            updated=base + timedelta(hours=offset),
        )
        for index, offset in ((1, 1), (3, 3), (2, 2))
    ]
    for request in requests:
        postgres_repo.deletion_repository.save_delete_request(request)

    page = postgres_repo.deletion_repository.list_delete_requests_page(
        sort_by=DeleteRequestSortBy.CREATED,
        reverse_order=False,
        retention_seconds=0,
        page=None,
        limit=10,
    )
    with postgres_connection.cursor() as cur:
        cur.execute(
            """
            SELECT id, created_at, (record->>'created')::timestamptz
            FROM tamoss_delete_requests
            ORDER BY created_at DESC, id DESC
            """
        )
        stored_timestamps = cur.fetchall()

    assert [request.id for request in page.items] == [
        requests[1].id,
        requests[2].id,
        requests[0].id,
    ]
    assert all(
        created_at == record_created
        for _, created_at, record_created in stored_timestamps
    )


def test_repository_rejects_open_or_empty_segment_timeranges(
    postgres_repo: PostgresRepository,
) -> None:
    flow_id = uuid4()
    postgres_repo.flow_repository.save_flow(
        FlowRecord(
            id=flow_id,
            data={},
            source_id=uuid4(),
            format="urn:x-nmos:format:video",
            container="video/mp2t",
        )
    )

    with pytest.raises(ValueError, match="finite start and end"):
        postgres_repo.segment_repository.append_segment(
            SegmentRecord(
                flow_id=flow_id,
                object_id="bbc/adapter/open-ended.ts",
                timerange="[0:0_)",
            )
        )
    with pytest.raises(ValueError, match="must not be empty"):
        postgres_repo.segment_repository.append_segment(
            SegmentRecord(
                flow_id=flow_id,
                object_id="bbc/adapter/empty.ts",
                timerange="[5:0_5:0)",
            )
        )


def test_repository_claims_delete_requests_and_webhook_deliveries_with_leases(
    postgres_repo: PostgresRepository,
) -> None:
    now = datetime.now(UTC)
    flow_id = uuid4()
    webhook_id = uuid4()
    delivery_id = uuid4()
    delete_request_id = uuid4()
    cleanup_id = uuid4()
    copy_id = uuid4()

    postgres_repo.webhook_repository.save_webhook(
        WebhookRecord(
            id=webhook_id,
            data={"url": "https://webhook.example.test/tamoss"},
            status="started",
            tags={"suite": "adapter"},
        )
    )
    postgres_repo.webhook_repository.save_webhook_delivery(
        WebhookDeliveryRecord(
            id=delivery_id,
            webhook_id=webhook_id,
            webhook_snapshot={"id": str(webhook_id), "status": "started"},
            event_type="flows/segments_added",
            event_timestamp=now,
            payload={"event": "flows/segments_added", "flow_id": str(flow_id)},
            status="pending",
        )
    )
    postgres_repo.deletion_repository.save_delete_request(
        DeletionRequestRecord(
            id=delete_request_id,
            flow_id=flow_id,
            timerange_to_delete="[0:0_10:0)",
            delete_flow=False,
            status="created",
            created_by="adapter-test",
        )
    )
    postgres_repo.deletion_repository.save_object_cleanup(
        ObjectCleanupRecord(
            id=cleanup_id,
            object_id="bbc/adapter/delete-me.ts",
            storage_backend_id=PRIMARY_BACKEND_ID,
            status="pending",
        )
    )
    postgres_repo.object_repository.save_object_copy(
        ObjectCopyRecord(
            id=copy_id,
            object_id="bbc/adapter/copy-me.ts",
            source_storage_backend_id=PRIMARY_BACKEND_ID,
            destination_storage_backend_id=uuid4(),
            status="pending",
        )
    )

    stored_webhook = postgres_repo.webhook_repository.get_webhook(webhook_id)
    assert stored_webhook is not None
    assert stored_webhook.data["url"] == "https://webhook.example.test/tamoss"
    assert stored_webhook.tags == {"suite": "adapter"}
    assert [
        webhook.id for webhook in postgres_repo.webhook_repository.list_webhooks()
    ] == [webhook_id]
    assert (
        postgres_repo.webhook_repository.get_webhook_delivery(delivery_id) is not None
    )
    assert postgres_repo.webhook_repository.list_webhook_deliveries()[0].payload[
        "event"
    ] == ("flows/segments_added")
    assert (
        postgres_repo.deletion_repository.get_delete_request(delete_request_id)
        is not None
    )
    assert (
        postgres_repo.deletion_repository.list_delete_requests()[0].id
        == delete_request_id
    )
    assert postgres_repo.deletion_repository.list_object_cleanups()[0].id == cleanup_id
    assert postgres_repo.object_repository.list_object_copies()[0].id == copy_id

    claimed_delivery = postgres_repo.webhook_repository.claim_webhook_deliveries(
        worker_id="worker-a",
        limit=1,
        lease_seconds=30,
    )[0]
    claimed_delete = postgres_repo.deletion_repository.claim_delete_requests(
        worker_id="worker-a",
        limit=1,
        lease_seconds=30,
    )[0]
    claimed_cleanup = postgres_repo.deletion_repository.claim_object_cleanups(
        worker_id="worker-a",
        limit=1,
        lease_seconds=30,
    )[0]
    claimed_copy = postgres_repo.object_repository.claim_object_copies(
        worker_id="worker-a",
        limit=1,
        lease_seconds=30,
    )[0]

    assert claimed_delivery.status == "started"
    assert claimed_delivery.claimed_by == "worker-a"
    assert claimed_delivery.claim_expires_at is not None
    assert claimed_delete.status == "started"
    assert claimed_delete.claimed_by == "worker-a"
    assert claimed_delete.claim_expires_at is not None
    assert claimed_cleanup.status == "started"
    assert claimed_cleanup.claimed_by == "worker-a"
    assert claimed_cleanup.claim_expires_at is not None
    assert claimed_copy.status == "started"
    assert claimed_copy.claimed_by == "worker-a"
    assert claimed_copy.claim_expires_at is not None

    assert (
        postgres_repo.webhook_repository.claim_webhook_deliveries(
            worker_id="worker-b",
            limit=1,
            lease_seconds=30,
        )
        == []
    )
    assert (
        postgres_repo.deletion_repository.claim_delete_requests(
            worker_id="worker-b",
            limit=1,
            lease_seconds=30,
        )
        == []
    )
    assert (
        postgres_repo.deletion_repository.claim_object_cleanups(
            worker_id="worker-b",
            limit=1,
            lease_seconds=30,
        )
        == []
    )
    assert (
        postgres_repo.object_repository.claim_object_copies(
            worker_id="worker-b",
            limit=1,
            lease_seconds=30,
        )
        == []
    )

    expired_at = datetime.now(UTC) - timedelta(seconds=1)
    postgres_repo.webhook_repository.save_webhook_delivery(
        replace(
            claimed_delivery,
            status="started",
            claimed_by="worker-a",
            claim_expires_at=expired_at,
        )
    )
    postgres_repo.deletion_repository.save_delete_request(
        replace(
            claimed_delete,
            status="started",
            claimed_by="worker-a",
            claim_expires_at=expired_at,
        )
    )
    postgres_repo.deletion_repository.save_object_cleanup(
        replace(
            claimed_cleanup,
            status="started",
            claimed_by="worker-a",
            claim_expires_at=expired_at,
        )
    )
    postgres_repo.object_repository.save_object_copy(
        replace(
            claimed_copy,
            status="started",
            claimed_by="worker-a",
            claim_expires_at=expired_at,
        )
    )

    reclaimed_delivery = postgres_repo.webhook_repository.claim_webhook_deliveries(
        worker_id="worker-b",
        limit=1,
        lease_seconds=30,
    )[0]
    reclaimed_delete = postgres_repo.deletion_repository.claim_delete_requests(
        worker_id="worker-b",
        limit=1,
        lease_seconds=30,
    )[0]
    reclaimed_cleanup = postgres_repo.deletion_repository.claim_object_cleanups(
        worker_id="worker-b",
        limit=1,
        lease_seconds=30,
    )[0]
    reclaimed_copy = postgres_repo.object_repository.claim_object_copies(
        worker_id="worker-b",
        limit=1,
        lease_seconds=30,
    )[0]

    assert reclaimed_delivery.id == delivery_id
    assert reclaimed_delivery.claimed_by == "worker-b"
    assert reclaimed_delete.id == delete_request_id
    assert reclaimed_delete.claimed_by == "worker-b"
    assert reclaimed_cleanup.id == cleanup_id
    assert reclaimed_cleanup.claimed_by == "worker-b"
    assert reclaimed_copy.id == copy_id
    assert reclaimed_copy.claimed_by == "worker-b"

    postgres_repo.webhook_repository.save_webhook_delivery(
        replace(
            reclaimed_delivery,
            status="done",
            claimed_by=None,
            claim_expires_at=None,
        )
    )
    assert (
        postgres_repo.webhook_repository.get_webhook_delivery(delivery_id).status
        == "done"
    )

    postgres_repo.deletion_repository.save_object_cleanup(
        replace(
            reclaimed_cleanup,
            status="done",
            claimed_by=None,
            claim_expires_at=None,
        )
    )
    assert (
        postgres_repo.deletion_repository.list_object_cleanups(statuses={"done"})[0].id
        == cleanup_id
    )

    postgres_repo.object_repository.save_object_copy(
        replace(
            reclaimed_copy,
            status="done",
            claimed_by=None,
            claim_expires_at=None,
        )
    )
    assert (
        postgres_repo.object_repository.list_object_copies(statuses={"done"})[0].id
        == copy_id
    )

    postgres_repo.webhook_repository.delete_webhook(webhook_id)
    assert postgres_repo.webhook_repository.get_webhook(webhook_id) is None


def test_repository_claims_webhook_deliveries_in_queue_order(
    postgres_repo: PostgresRepository,
) -> None:
    webhook_id = uuid4()
    base_time = datetime.now(UTC) - timedelta(minutes=5)
    first_id = UUID("00000000-0000-4000-8000-000000000101")
    second_id = UUID("00000000-0000-4000-8000-000000000102")
    third_id = UUID("00000000-0000-4000-8000-000000000103")
    created_times = {
        first_id: base_time,
        second_id: base_time + timedelta(seconds=1),
        third_id: base_time + timedelta(seconds=2),
    }

    postgres_repo.webhook_repository.save_webhook(
        WebhookRecord(
            id=webhook_id,
            data={"url": "https://webhook.example.test/tamoss"},
            status="started",
        )
    )
    for delivery_id, sequence in (
        (third_id, "third"),
        (first_id, "first"),
        (second_id, "second"),
    ):
        created = created_times[delivery_id]
        postgres_repo.webhook_repository.save_webhook_delivery(
            WebhookDeliveryRecord(
                id=delivery_id,
                webhook_id=webhook_id,
                webhook_snapshot={"status": "started"},
                event_type="flows/updated",
                event_timestamp=created,
                payload={"sequence": sequence},
                status="pending",
                created=created,
                updated=created,
                next_attempt_at=created,
            )
        )

    claimed = postgres_repo.webhook_repository.claim_webhook_deliveries(
        worker_id="worker-order",
        limit=3,
        lease_seconds=30,
    )

    assert [delivery.payload["sequence"] for delivery in claimed] == [
        "first",
        "second",
        "third",
    ]
