from __future__ import annotations

import os
from dataclasses import replace
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Iterator
from uuid import UUID, uuid4

import psycopg
import pytest
from psycopg import sql
from tamoss.adapters.postgres import PostgresRepository
from tamoss.api.schemas import FlowCollectionItem, FlowWrite
from tamoss.application.use_cases import TamossUseCases
from tamoss.auth import Identity
from tamoss.domain.model import (
    DeletionRequestRecord,
    FlowRecord,
    MediaObjectRecord,
    ObjectInstance,
    SegmentRecord,
    ServiceMetadata,
    SourceRecord,
    StorageBackend,
    WebhookDeliveryRecord,
    WebhookRecord,
)
from tamoss.ports.repositories import SegmentTimerangeBounds
from tamoss.settings import Settings, StorageBackendSettings

pytestmark = pytest.mark.needs_db

REPO_ROOT = Path(__file__).resolve().parents[3]
PRIMARY_BACKEND_ID = UUID("11111111-1111-4111-8111-111111111111")
REPLACEMENT_BACKEND_ID = UUID("22222222-2222-4222-8222-222222222222")


@pytest.fixture()
def postgres_connection() -> Iterator[psycopg.Connection]:
    database_url = _database_url()
    schema = f"tamoss_test_{uuid4().hex}"
    try:
        admin = psycopg.connect(database_url, connect_timeout=2)
    except psycopg.OperationalError as exc:
        pytest.skip(f"Postgres test database is unavailable: {exc}")
    admin.autocommit = True
    with admin.cursor() as cur:
        cur.execute(sql.SQL("CREATE SCHEMA {}").format(sql.Identifier(schema)))

    conn = psycopg.connect(database_url, connect_timeout=2)
    conn.autocommit = True
    with conn.cursor() as cur:
        cur.execute(sql.SQL("SET search_path TO {}").format(sql.Identifier(schema)))

    try:
        yield conn
    finally:
        conn.close()
        with admin.cursor() as cur:
            cur.execute(
                sql.SQL("DROP SCHEMA IF EXISTS {} CASCADE").format(
                    sql.Identifier(schema)
                )
            )
        admin.close()


@pytest.fixture()
def postgres_repo(postgres_connection: psycopg.Connection) -> PostgresRepository:
    return PostgresRepository(
        connection=postgres_connection,
        storage_backend=_primary_backend(),
    )


def test_schema_and_bootstrap_sql_load_bbc_storage_backend_shape(
    postgres_connection: psycopg.Connection,
) -> None:
    _execute_sql_file(postgres_connection, REPO_ROOT / "db/schema.sql")
    _execute_sql_file(postgres_connection, REPO_ROOT / "db/bootstrap.sql")

    with postgres_connection.cursor() as cur:
        cur.execute(
            """
            SELECT label, store_type, default_storage, record
            FROM tamoss_storage_backends
            ORDER BY label
            """
        )
        rows = cur.fetchall()
        cur.execute(
            """
            SELECT table_name
            FROM information_schema.tables
            WHERE table_schema = current_schema()
              AND table_name LIKE 'tamoss_%'
            ORDER BY table_name
            """
        )
        tables = {row[0] for row in cur.fetchall()}

    assert {
        "tamoss_delete_requests",
        "tamoss_flows",
        "tamoss_media_objects",
        "tamoss_segments",
        "tamoss_service_metadata",
        "tamoss_sources",
        "tamoss_storage_backends",
        "tamoss_webhook_deliveries",
        "tamoss_webhooks",
    } <= tables
    assert len(rows) == 1
    label, store_type, default_storage, record = rows[0]
    assert label
    assert store_type == "http_object_store"
    assert default_storage is True
    assert record["id"]
    assert record["default_storage"] is True


def test_repository_persists_service_metadata_and_storage_backend_default(
    postgres_connection: psycopg.Connection,
) -> None:
    repo = PostgresRepository(
        connection=postgres_connection,
        storage_backend=_primary_backend(),
    )
    repo.save_service_metadata(
        ServiceMetadata(name="TAMOSS adapter test", description="Postgres")
    )

    replacement = _replacement_backend()
    repo = PostgresRepository(
        connection=postgres_connection,
        storage_backend=replacement,
    )

    assert repo.get_service_metadata() == ServiceMetadata(
        name="TAMOSS adapter test",
        description="Postgres",
    )
    assert repo.default_storage_backend() == replacement

    with postgres_connection.cursor() as cur:
        cur.execute(
            """
            SELECT id, default_storage, record->>'default_storage'
            FROM tamoss_storage_backends
            ORDER BY id
            """
        )
        default_rows = cur.fetchall()

    assert default_rows == [(REPLACEMENT_BACKEND_ID, True, "true")]


def test_repository_round_trips_bbc_resources_and_segment_timerange_bounds(
    postgres_repo: PostgresRepository,
    postgres_connection: psycopg.Connection,
) -> None:
    flow_id = uuid4()
    source_id = uuid4()
    primary = postgres_repo.default_storage_backend()
    assert primary is not None

    postgres_repo.save_source(
        SourceRecord(
            id=source_id,
            format="urn:x-nmos:format:video",
            label="Adapter source",
            description="BBC shaped source",
            tags={"suite": "adapter", "roles": ["video", "primary"]},
        )
    )
    postgres_repo.save_flow(
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
    postgres_repo.save_object(
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

    postgres_repo.append_segment(
        SegmentRecord(
            flow_id=flow_id,
            object_id="bbc/adapter/segment-020.ts",
            timerange="[10:0_20:0)",
        )
    )
    postgres_repo.append_segment(
        SegmentRecord(
            flow_id=flow_id,
            object_id="bbc/adapter/segment-010.ts",
            timerange="[0:0_10:0)",
            object_timerange="[0:0_10:0)",
            ts_offset="0:0",
            sample_offset=0,
            sample_count=250,
            get_urls=[
                {
                    "url": "https://objects.example.test/bbc/adapter/segment-010.ts",
                    "label": primary.label,
                    "storage_id": str(primary.id),
                    "presigned": False,
                }
            ],
            key_frame_count=1,
        )
    )
    postgres_repo.append_segment(
        SegmentRecord(
            flow_id=flow_id,
            object_id="bbc/adapter/still-frame.ts",
            timerange="[5:0_5:0]",
        )
    )

    assert postgres_repo.get_source(source_id).description == "BBC shaped source"
    flow = postgres_repo.get_flow(flow_id)
    assert flow is not None
    assert flow.read_only is True
    assert flow.data["essence_parameters"]["frame_rate"]["numerator"] == 25

    media_object = postgres_repo.get_object("bbc/adapter/segment-001.ts")
    assert media_object is not None
    assert media_object.referenced_by_flows == {flow_id}
    assert media_object.instances[0].storage_backend == primary

    segments = postgres_repo.list_segments(flow_id)
    assert [segment.object_id for segment in segments] == [
        "bbc/adapter/still-frame.ts",
        "bbc/adapter/segment-010.ts",
        "bbc/adapter/segment-020.ts",
    ]
    assert segments[1].sample_count == 250
    assert segments[1].get_urls[0]["storage_id"] == str(primary.id)

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
        ("[5:0_5:0]", 5_000_000_000, 5_000_000_000),
        ("[0:0_10:0)", 0, 10_000_000_000),
        ("[10:0_20:0)", 10_000_000_000, 20_000_000_000),
    ]


def test_repository_lists_segments_with_database_paging_and_filters(
    postgres_repo: PostgresRepository,
) -> None:
    flow_id = uuid4()
    postgres_repo.save_flow(
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
        postgres_repo.append_segment(
            SegmentRecord(
                flow_id=flow_id,
                object_id=object_id,
                timerange=timerange,
            )
        )

    first_page = postgres_repo.list_segments_page(
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

    point_page = postgres_repo.list_segments_page(
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

    overlapping = postgres_repo.list_segments_overlapping(
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

    filtered_page = postgres_repo.list_segments_page(
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

    postgres_repo.save_flow(
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
    postgres_repo.save_flow(
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
    postgres_repo.save_flow(
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
    postgres_repo.append_segment(
        SegmentRecord(
            flow_id=child_flow_id,
            object_id="bbc/adapter/programme.ts",
            timerange="[0:0_10:0)",
        )
    )

    flow_page = postgres_repo.list_flows_page(
        source_id=child_source_id,
        timerange_start=5_000_000_000,
        timerange_end=6_000_000_000,
        timerange_is_empty=False,
        timerange_is_point=False,
        format="urn:x-nmos:format:video",
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

    empty_timerange_page = postgres_repo.list_flows_page(
        source_id=idle_source_id,
        timerange_start=None,
        timerange_end=None,
        timerange_is_empty=True,
        timerange_is_point=False,
        format=None,
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

    timeranges = postgres_repo.flow_timeranges([child_flow_id, idle_flow_id])
    assert timeranges == {
        child_flow_id: "[0:0_10:0)",
        idle_flow_id: "()",
    }

    relationships = postgres_repo.source_relationships_for([child_source_id])
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

    postgres_repo.save_source(
        SourceRecord(
            id=source_id,
            format="urn:x-nmos:format:video",
            label="programme",
            tags={"role": ["primary", "studio"]},
        )
    )
    postgres_repo.save_source(
        SourceRecord(
            id=other_source_id,
            format="urn:x-nmos:format:video",
            label="programme",
            tags={"role": "backup"},
        )
    )
    postgres_repo.save_flow(
        FlowRecord(
            id=flow_id,
            source_id=source_id,
            format="urn:x-nmos:format:video",
            container="video/mp2t",
            tags={"role": ["primary", "main"]},
            data={},
        )
    )
    postgres_repo.save_flow(
        FlowRecord(
            id=other_flow_id,
            source_id=other_source_id,
            format="urn:x-nmos:format:video",
            container="video/mp2t",
            tags={"role": "backup"},
            data={},
        )
    )
    postgres_repo.save_webhook(
        WebhookRecord(
            id=webhook_id,
            data={"url": "https://webhook.example.test/tamoss"},
            status="created",
            tags={"env": ["prod", "blue"]},
        )
    )
    postgres_repo.save_webhook(
        WebhookRecord(
            id=other_webhook_id,
            data={"url": "https://webhook.example.test/other"},
            status="created",
            tags={"env": "dev"},
        )
    )

    source_page = postgres_repo.list_sources_page(
        label="programme",
        format="urn:x-nmos:format:video",
        tag_values={"role": {"studio"}},
        tag_exists={"missing": False},
        page=None,
        limit=10,
    )
    assert [source.id for source in source_page.items] == [source_id]

    webhook_page = postgres_repo.list_webhooks_page(
        tag_values={"env": {"blue"}},
        tag_exists={},
        page=None,
        limit=10,
    )
    assert [webhook.id for webhook in webhook_page.items] == [webhook_id]

    flow_id_page = postgres_repo.list_flow_ids_matching_tags_page(
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

    postgres_repo.save_registered_segments(
        flow=flow,
        media_objects=[media_object],
        segments=[segment],
    )

    saved_flow = postgres_repo.get_flow(flow_id)
    assert saved_flow is not None
    assert saved_flow.segments_updated == now
    assert postgres_repo.get_object(media_object.id) == media_object
    assert postgres_repo.list_segments(flow_id)[0].object_id == media_object.id


def test_repository_create_object_is_conflict_safe(
    postgres_repo: PostgresRepository,
) -> None:
    original = MediaObjectRecord(id="bbc/adapter/allocated.ts")
    replacement = MediaObjectRecord(
        id=original.id,
        timerange="[0:0_10:0)",
    )

    assert postgres_repo.create_object(original) is True
    assert postgres_repo.create_object(replacement) is False

    saved = postgres_repo.get_object(original.id)
    assert saved is not None
    assert saved.timerange is None


def test_repository_unit_of_work_rolls_back_on_error(
    postgres_repo: PostgresRepository,
    postgres_connection: psycopg.Connection,
) -> None:
    flow_id = uuid4()
    object_id = "bbc/adapter/rollback.ts"

    with pytest.raises(psycopg.errors.DivisionByZero):
        with postgres_repo.unit_of_work():
            postgres_repo.save_flow(
                FlowRecord(
                    id=flow_id,
                    data={"label": "Rollback"},
                    source_id=uuid4(),
                    format="urn:x-nmos:format:video",
                    container="video/mp2t",
                )
            )
            postgres_repo.save_object(MediaObjectRecord(id=object_id))
            postgres_connection.execute("SELECT 1 / 0")

    assert postgres_repo.get_flow(flow_id) is None
    assert postgres_repo.get_object(object_id) is None


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
    postgres_repo.save_registered_segments(
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

    with pytest.raises(ValueError, match="overlaps"):
        postgres_repo.save_registered_segments(
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

    assert postgres_repo.get_object("bbc/adapter/second.ts") is None
    assert postgres_repo.get_flow(flow_id).data["label"] == "Overlap"
    assert [segment.object_id for segment in postgres_repo.list_segments(flow_id)] == [
        "bbc/adapter/first.ts"
    ]


def test_repository_loads_media_objects_in_bulk(
    postgres_repo: PostgresRepository,
) -> None:
    flow_id = uuid4()
    postgres_repo.save_object(
        MediaObjectRecord(
            id="bbc/adapter/object-a.ts",
            timerange="[0:0_10:0)",
            first_referenced_by_flow=flow_id,
            referenced_by_flows={flow_id},
        )
    )
    postgres_repo.save_object(
        MediaObjectRecord(
            id="bbc/adapter/object-b.ts",
            timerange="[10:0_20:0)",
            first_referenced_by_flow=flow_id,
            referenced_by_flows={flow_id},
        )
    )

    objects_by_id = postgres_repo.get_objects(
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
    parent_flow_id = uuid4()
    parent_source_id = uuid4()
    child_flow_id = uuid4()
    child_source_id = uuid4()

    use_cases.put_flow(
        flow_id=child_flow_id,
        flow_write=_video_flow_write(child_flow_id, child_source_id),
        identity=identity,
    )
    use_cases.put_flow(
        flow_id=parent_flow_id,
        flow_write=_multi_flow_write(parent_flow_id, parent_source_id),
        identity=identity,
    )

    use_cases.set_flow_collection(
        flow_id=parent_flow_id,
        collection=[FlowCollectionItem(id=child_flow_id, role="video")],
        identity=identity,
    )

    assert use_cases.get_flow_collection(parent_flow_id) == [
        {"id": str(child_flow_id), "role": "video"}
    ]
    relationships = use_cases.source_relationships()
    assert relationships[parent_source_id].source_collection == [
        {"id": str(child_source_id), "role": "video"}
    ]
    assert relationships[child_source_id].collected_by == [parent_source_id]
    assert use_cases.get_flow(child_flow_id, include_collected_by=True).data[
        "collected_by"
    ] == [str(parent_flow_id)]

    use_cases.delete_flow_collection(flow_id=parent_flow_id, identity=identity)

    assert use_cases.get_flow_collection(parent_flow_id) == []
    assert (
        "collected_by"
        not in use_cases.get_flow(child_flow_id, include_collected_by=True).data
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
    backend = postgres_repo.default_storage_backend()
    assert backend is not None

    use_cases.put_flow(
        flow_id=flow_id,
        flow_write=_video_flow_write(flow_id, source_id),
        identity=identity,
    )
    postgres_repo.save_object(
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
    postgres_repo.append_segment(
        SegmentRecord(
            flow_id=flow_id,
            object_id=object_id,
            timerange="[0:0_10:0)",
        )
    )

    request = use_cases.delete_segments(
        flow_id=flow_id,
        timerange="[0:0_10:0)",
        object_id=None,
        identity=identity,
    )

    assert request is not None
    assert request.status == "created"
    assert postgres_repo.list_segments(flow_id)

    processed = use_cases.process_pending_delete_requests(
        max_requests=1,
        worker_id="postgres-delete-worker",
        lease_seconds=30,
    )

    saved_request = postgres_repo.get_delete_request(request.id)
    assert processed == 1
    assert saved_request is not None
    assert saved_request.status == "done"
    assert saved_request.timerange_remaining is None
    assert saved_request.claimed_by is None
    assert postgres_repo.list_segments(flow_id) == []
    assert postgres_repo.get_object(object_id) is None
    assert object_storage.deleted == [(backend.id, object_id)]


def test_repository_rejects_open_or_empty_segment_timeranges(
    postgres_repo: PostgresRepository,
) -> None:
    flow_id = uuid4()
    postgres_repo.save_flow(
        FlowRecord(
            id=flow_id,
            data={},
            source_id=uuid4(),
            format="urn:x-nmos:format:video",
            container="video/mp2t",
        )
    )

    with pytest.raises(ValueError, match="finite start and end"):
        postgres_repo.append_segment(
            SegmentRecord(
                flow_id=flow_id,
                object_id="bbc/adapter/open-ended.ts",
                timerange="[0:0_)",
            )
        )
    with pytest.raises(ValueError, match="must not be empty"):
        postgres_repo.append_segment(
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

    postgres_repo.save_webhook(
        WebhookRecord(
            id=webhook_id,
            data={"url": "https://webhook.example.test/tamoss"},
            status="started",
            tags={"suite": "adapter"},
        )
    )
    postgres_repo.save_webhook_delivery(
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
    postgres_repo.save_delete_request(
        DeletionRequestRecord(
            id=delete_request_id,
            flow_id=flow_id,
            timerange_to_delete="[0:0_10:0)",
            delete_flow=False,
            status="created",
            created_by="adapter-test",
        )
    )

    stored_webhook = postgres_repo.get_webhook(webhook_id)
    assert stored_webhook is not None
    assert stored_webhook.data["url"] == "https://webhook.example.test/tamoss"
    assert stored_webhook.tags == {"suite": "adapter"}
    assert [webhook.id for webhook in postgres_repo.list_webhooks()] == [webhook_id]
    assert postgres_repo.get_webhook_delivery(delivery_id) is not None
    assert postgres_repo.list_webhook_deliveries()[0].payload["event"] == (
        "flows/segments_added"
    )
    assert postgres_repo.get_delete_request(delete_request_id) is not None
    assert postgres_repo.list_delete_requests()[0].id == delete_request_id

    claimed_delivery = postgres_repo.claim_webhook_deliveries(
        worker_id="worker-a",
        limit=1,
        lease_seconds=30,
    )[0]
    claimed_delete = postgres_repo.claim_delete_requests(
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

    assert (
        postgres_repo.claim_webhook_deliveries(
            worker_id="worker-b",
            limit=1,
            lease_seconds=30,
        )
        == []
    )
    assert (
        postgres_repo.claim_delete_requests(
            worker_id="worker-b",
            limit=1,
            lease_seconds=30,
        )
        == []
    )

    expired_at = datetime.now(UTC) - timedelta(seconds=1)
    postgres_repo.save_webhook_delivery(
        replace(
            claimed_delivery,
            status="started",
            claimed_by="worker-a",
            claim_expires_at=expired_at,
        )
    )
    postgres_repo.save_delete_request(
        replace(
            claimed_delete,
            status="started",
            claimed_by="worker-a",
            claim_expires_at=expired_at,
        )
    )

    reclaimed_delivery = postgres_repo.claim_webhook_deliveries(
        worker_id="worker-b",
        limit=1,
        lease_seconds=30,
    )[0]
    reclaimed_delete = postgres_repo.claim_delete_requests(
        worker_id="worker-b",
        limit=1,
        lease_seconds=30,
    )[0]

    assert reclaimed_delivery.id == delivery_id
    assert reclaimed_delivery.claimed_by == "worker-b"
    assert reclaimed_delete.id == delete_request_id
    assert reclaimed_delete.claimed_by == "worker-b"

    postgres_repo.save_webhook_delivery(
        replace(
            reclaimed_delivery,
            status="done",
            claimed_by=None,
            claim_expires_at=None,
        )
    )
    assert postgres_repo.get_webhook_delivery(delivery_id).status == "done"

    postgres_repo.delete_webhook(webhook_id)
    assert postgres_repo.get_webhook(webhook_id) is None


def _database_url() -> str:
    return (
        os.getenv("TAMOSS_TEST_DB_URL") or "postgresql://tams:tams@127.0.0.1:55432/tams"
    )


def _execute_sql_file(connection: psycopg.Connection, path: Path) -> None:
    with connection.cursor() as cur:
        cur.execute(path.read_text(encoding="utf-8"))


def _primary_backend() -> StorageBackend:
    return StorageBackend(
        id=PRIMARY_BACKEND_ID,
        label="tamoss.postgres.primary",
        provider="tamoss",
        region="us-east-1",
        store_product="s3",
        default_storage=True,
        bucket_name="primary",
        endpoint_url="https://objects.internal.example.test",
        public_endpoint_url="https://objects.example.test",
    )


def _replacement_backend() -> StorageBackend:
    return StorageBackend(
        id=REPLACEMENT_BACKEND_ID,
        label="tamoss.postgres.replacement",
        provider="tamoss",
        region="us-east-1",
        store_product="s3",
        default_storage=True,
        bucket_name="replacement",
        endpoint_url="https://objects.internal.example.test",
        public_endpoint_url="https://objects.example.test",
    )


def _use_cases(
    repository: PostgresRepository,
    *,
    object_storage: RecordingObjectStorage | None = None,
) -> TamossUseCases:
    return TamossUseCases(
        repository=repository,
        object_storage=object_storage or RecordingObjectStorage(),
        settings=Settings(
            auth_required=False,
            database_url=None,
            storage_backend=StorageBackendSettings(
                id=PRIMARY_BACKEND_ID,
                label="tamoss.postgres.primary",
                provider="tamoss",
                region="us-east-1",
                store_product="s3",
                default_storage=True,
                bucket_name="primary",
                endpoint_url="https://objects.internal.example.test",
                public_endpoint_url="https://objects.example.test",
                access_key="access",
                secret_key="secret",
            ),
        ),
    )


def _identity() -> Identity:
    return Identity(subject="postgres-test", method="test")


def _video_flow_write(flow_id: UUID, source_id: UUID) -> FlowWrite:
    return FlowWrite(
        id=flow_id,
        source_id=source_id,
        format="urn:x-nmos:format:video",
        codec="video/h264",
        container="video/mp2t",
        essence_parameters={
            "frame_width": 1920,
            "frame_height": 1080,
            "frame_rate": {"numerator": 25, "denominator": 1},
        },
    )


def _multi_flow_write(flow_id: UUID, source_id: UUID) -> FlowWrite:
    return FlowWrite(
        id=flow_id,
        source_id=source_id,
        format="urn:x-nmos:format:multi",
        container="video/mp2t",
    )


class RecordingObjectStorage:
    def __init__(self) -> None:
        self.deleted: list[tuple[UUID, str]] = []

    def build_put_request(
        self, *, object_id: str, flow_container: str, backend: StorageBackend
    ) -> dict:
        return {
            "url": f"https://objects.example.test/{object_id}",
            "content-type": flow_container,
            "headers": {"Content-Type": flow_container},
        }

    def build_get_url(self, *, object_id: str, backend: StorageBackend) -> str:
        return f"https://objects.example.test/{object_id}"

    def build_get_urls(self, *, object_id: str, backend: StorageBackend) -> list[dict]:
        return [
            {
                "url": f"https://objects.example.test/{object_id}",
                "label": backend.label,
                "presigned": True,
            }
        ]

    def write(
        self, object_id: str, data: bytes, *, backend: StorageBackend | None = None
    ) -> None:
        return None

    def read(
        self, object_id: str, *, backend: StorageBackend | None = None
    ) -> bytes | None:
        return None

    def delete(self, object_id: str, *, backend: StorageBackend | None = None) -> None:
        assert backend is not None
        self.deleted.append((backend.id, object_id))
