from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path
from urllib.parse import urlencode
from uuid import UUID, uuid4

import psycopg
import pytest
from alembic import command
from psycopg import sql
from psycopg.types.json import Jsonb
from tamoss.adapters.postgres import PostgresRepository
from tamoss.api.presenters import flow_response
from tamoss.db.migrations.runner import _sqlalchemy_url, alembic_config
from tamoss.domain.model import ServiceMetadata

from tests.adapters.postgres.support import (
    PRIMARY_BACKEND_ID,
    REPLACEMENT_BACKEND_ID,
    SCHEMA_ASSETS_DIR,
    database_url,
    execute_sql_file,
    primary_backend,
    replacement_backend,
)

pytestmark = pytest.mark.needs_db

TAMS_8_1_SCHEMA_FIXTURE = (
    Path(__file__).with_name("fixtures") / "tams_8_1_schema_0006.sql"
)


def test_tams_8_2_large_listing_queries_have_indexed_plans(
    postgres_connection: psycopg.Connection,
) -> None:
    with postgres_connection.cursor() as cur:
        cur.execute(
            """
            INSERT INTO tamoss_profiles (
              id, format, codec, label, record, created
            )
            SELECT
              ('00000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
              CASE WHEN n % 100 = 0 THEN 'urn:x-nmos:format:video'
                   ELSE 'urn:x-nmos:format:audio' END,
              CASE WHEN n % 100 = 0 THEN 'video/h264' ELSE 'audio/pcm' END,
              'profile-' || lpad(n::text, 5, '0'),
              '{}'::jsonb,
              NOW()
            FROM generate_series(1, 10000) AS n;

            INSERT INTO tamoss_sources (
              id, format, label, record, metadata_updated, created
            )
            SELECT
              ('10000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
              'urn:x-nmos:format:video',
              'source-' || lpad(n::text, 5, '0'),
              '{}'::jsonb,
              NOW(),
              NOW()
            FROM generate_series(1, 10000) AS n;

            INSERT INTO tamoss_flows (
              id, source_id, format, status, init_segments, label,
              flow_collection_ids, record, metadata_updated, created
            )
            SELECT
              ('20000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
              ('10000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
              'urn:x-nmos:format:video',
              CASE WHEN n % 100 = 0 THEN 'ingesting' ELSE 'closed_complete' END,
              n % 3 = 0,
              'flow-' || lpad(n::text, 5, '0'),
              CASE WHEN n % 10 = 0 THEN ARRAY[
                ('20000000-0000-4000-8000-' ||
                 lpad((n - 1)::text, 12, '0'))::uuid
              ] ELSE ARRAY[]::uuid[] END,
              '{}'::jsonb,
              NOW(),
              NOW()
            FROM generate_series(1, 10000) AS n;

            INSERT INTO tamoss_storage_backends (
              id, label, provider, region, store_product, store_type,
              default_storage, tags, record
            )
            SELECT
              ('30000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
              'storage-' || lpad(n::text, 5, '0'),
              'tamoss', 'us-east-1', 's3', 'http_object_store',
              FALSE, '{}'::jsonb, '{}'::jsonb
            FROM generate_series(1, 10000) AS n;

            INSERT INTO tamoss_webhooks (id, status, tags, record)
            SELECT
              ('40000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
              'awaiting',
              '{}'::jsonb,
              jsonb_build_object(
                'data', jsonb_build_object(
                  'url', 'https://hooks.example/' || lpad(n::text, 5, '0')
                )
              )
            FROM generate_series(1, 10000) AS n;

            INSERT INTO tamoss_delete_requests (
              id, flow_id, status, record, updated, created_at
            )
            SELECT
              ('50000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
              ('20000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
              CASE WHEN n % 2 = 0 THEN 'done' ELSE 'created' END,
              '{}'::jsonb,
              NOW() - (n || ' seconds')::interval,
              NOW() - (n || ' seconds')::interval
            FROM generate_series(1, 10000) AS n;

            ANALYZE tamoss_profiles;
            ANALYZE tamoss_sources;
            ANALYZE tamoss_flows;
            ANALYZE tamoss_storage_backends;
            ANALYZE tamoss_webhooks;
            ANALYZE tamoss_delete_requests;
            """
        )
        cur.execute("SET enable_seqscan = off")

        plans = {
            "profiles": (
                "SELECT record FROM tamoss_profiles "
                "WHERE format = 'urn:x-nmos:format:video' "
                "ORDER BY id LIMIT 51",
                "idx_tamoss_profiles_format_id",
            ),
            "sources": (
                "SELECT record FROM tamoss_sources "
                "ORDER BY label ASC NULLS LAST, id ASC LIMIT 51",
                "idx_tamoss_sources_label",
            ),
            "flows": (
                "SELECT record FROM tamoss_flows "
                "WHERE status = 'ingesting' ORDER BY id LIMIT 51",
                "idx_tamoss_flows_status_id",
            ),
            "flow-label-filter": (
                "SELECT record FROM tamoss_flows "
                "WHERE label = 'flow-00100' ORDER BY id LIMIT 51",
                "idx_tamoss_flows_label",
            ),
            "storage": (
                "SELECT record FROM tamoss_storage_backends "
                "ORDER BY label ASC NULLS LAST, id ASC LIMIT 51",
                "idx_tamoss_storage_backends_label",
            ),
            "webhooks": (
                "SELECT record FROM tamoss_webhooks "
                "ORDER BY record->'data'->>'url' ASC, id ASC LIMIT 51",
                "idx_tamoss_webhooks_url",
            ),
            "delete-created": (
                "SELECT record FROM tamoss_delete_requests "
                "ORDER BY created_at DESC NULLS LAST, id DESC LIMIT 51",
                "idx_tamoss_delete_requests_created",
            ),
            "delete-expiry": (
                "SELECT record FROM tamoss_delete_requests ORDER BY "
                "(CASE WHEN status = 'done' THEN updated END) "
                "DESC NULLS LAST, id DESC LIMIT 51",
                "idx_tamoss_delete_requests_expiry",
            ),
            "flow-top-level": (
                "SELECT flow.id FROM tamoss_flows AS flow "
                "WHERE NOT EXISTS (SELECT 1 FROM tamoss_flows AS parent "
                "WHERE parent.flow_collection_ids @> ARRAY[flow.id]) "
                "ORDER BY flow.id LIMIT 51",
                "idx_tamoss_flows_flow_collection",
            ),
            "source-top-level": (
                "SELECT source.id FROM tamoss_sources AS source "
                "WHERE NOT EXISTS (SELECT 1 FROM tamoss_flows AS child "
                "JOIN tamoss_flows AS parent ON "
                "parent.flow_collection_ids @> ARRAY[child.id] "
                "WHERE child.source_id = source.id) "
                "ORDER BY source.id LIMIT 51",
                "idx_tamoss_flows_flow_collection",
            ),
        }

        for name, (query, expected_index) in plans.items():
            cur.execute(f"EXPLAIN (FORMAT JSON) {query}")
            plan = str(cur.fetchone()[0])
            assert expected_index in plan, (
                f"{name} did not use {expected_index}: {plan}"
            )
        cur.execute("RESET enable_seqscan")


def test_tams_8_2_migration_upgrades_populated_8_1_schema(
    postgres_connection: psycopg.Connection,
) -> None:
    schema = f"tamoss_upgrade_{uuid4().hex}"
    with postgres_connection.cursor() as cur:
        cur.execute(sql.SQL("CREATE SCHEMA {}").format(sql.Identifier(schema)))

    base_url = _sqlalchemy_url(database_url())
    separator = "&" if "?" in base_url else "?"
    migration_url = (
        f"{base_url}{separator}{urlencode({'options': f'-csearch_path={schema}'})}"
    )
    config = alembic_config(migration_url)
    try:
        with psycopg.connect(
            database_url(),
            options=f"-csearch_path={schema}",
        ) as legacy_connection:
            execute_sql_file(legacy_connection, TAMS_8_1_SCHEMA_FIXTURE)

        command.stamp(config, "20260610_0006")
        with psycopg.connect(
            database_url(),
            options=f"-csearch_path={schema}",
        ) as legacy_connection:
            with legacy_connection.cursor() as cur:
                cur.execute("SELECT version_num FROM alembic_version")
                assert cur.fetchone()[0] == "20260610_0006"
            _assert_tams_8_1_schema(legacy_connection)
            _seed_tams_8_1_upgrade_data(legacy_connection)

        command.upgrade(config, "20260810_0007")
        with (
            psycopg.connect(
                database_url(),
                options=f"-csearch_path={schema}",
            ) as upgraded_connection,
            upgraded_connection.cursor() as cur,
        ):
            cur.execute("SELECT version_num FROM alembic_version")
            version = cur.fetchone()[0]
            cur.execute(
                """
                SELECT id::text, created, record->>'created'
                FROM tamoss_sources
                ORDER BY id
                """
            )
            source_rows = cur.fetchall()
            cur.execute(
                """
                SELECT id::text, profile_id::text, status, init_segments,
                       label, created, flow_collection_ids::text[]
                FROM tamoss_flows
                ORDER BY id
                """
            )
            flow_rows = cur.fetchall()
            cur.execute(
                """
                SELECT id::text, jsonb_build_object(
                    'column_profile_id', profile_id::text,
                    'root_profile_id', record->'profile_id',
                    'data_profile_id', record->'data'->'profile_id',
                    'data_has_profile_id', record->'data' ? 'profile_id',
                    'column_status', status,
                    'root_status', record->'status',
                    'data_status', record->'data'->'status',
                    'data_has_status', record->'data' ? 'status',
                    'column_init_segments', init_segments,
                    'root_init_segments', record->'init_segments',
                    'data_init_segments',
                        record->'data'->'essence_parameters'->'init_segments',
                    'data_init_segments_type', jsonb_typeof(
                        record->'data'->'essence_parameters'->'init_segments'
                    )
                )
                FROM tamoss_flows
                ORDER BY id
                """
            )
            flow_projection_rows = dict(cur.fetchall())
            cur.execute(
                """
                SELECT id::text, created, record->>'created',
                       record->'data'->>'created'
                FROM tamoss_flows
                ORDER BY id
                """
            )
            flow_created_rows = {
                flow_id: (column_created, root_created, data_created)
                for (
                    flow_id,
                    column_created,
                    root_created,
                    data_created,
                ) in cur.fetchall()
            }
            cur.execute(
                """
                SELECT id::text, created_at, record->>'created'
                FROM tamoss_delete_requests
                ORDER BY id
                """
            )
            delete_rows = cur.fetchall()
            cur.execute(
                """
                SELECT id, object_kind, content_type,
                       record->>'object_kind', record->>'content_type'
                FROM tamoss_media_objects
                ORDER BY id
                """
            )
            object_rows = cur.fetchall()
            cur.execute(
                """
                SELECT tags
                FROM tamoss_storage_backends
                WHERE id = '40000000-0000-4000-8000-000000000001'
                """
            )
            storage_tags = cur.fetchone()[0]
            cur.execute("SELECT COUNT(*) FROM tamoss_profiles")
            profile_count = cur.fetchone()[0]
            cur.execute(
                """
                SELECT flow.init_segments,
                       flow.record->'init_segments',
                       flow.record->'data'->'essence_parameters'
                           ->'init_segments',
                       segment.init_object_id
                FROM tamoss_segments AS segment
                JOIN tamoss_flows AS flow ON flow.id = segment.flow_id
                WHERE segment.object_id = 'legacy/media.ts'
                """
            )
            segmented_flow_state = cur.fetchone()
            cur.execute(
                """
                SELECT indexdef
                FROM pg_indexes
                WHERE schemaname = current_schema()
                  AND indexname = 'idx_tamoss_delete_requests_created'
                """
            )
            delete_created_index = cur.fetchone()[0]

            repository = PostgresRepository(
                connection=upgraded_connection,
                storage_backend=primary_backend(),
            )
            runtime_source_created_rows: dict[str, datetime] = {}
            for source_id, *_ in source_rows:
                source = repository.source_repository.get_source(UUID(source_id))
                assert source is not None
                runtime_source_created_rows[source_id] = source.created
            runtime_delete_created_rows: dict[str, datetime] = {}
            for request_id, *_ in delete_rows:
                request = repository.deletion_repository.get_delete_request(
                    UUID(request_id)
                )
                assert request is not None
                runtime_delete_created_rows[request_id] = request.created
            runtime_flow_rows: dict[str, dict[str, object]] = {}
            runtime_created_rows: dict[str, tuple[datetime, datetime]] = {}
            for flow_id in flow_projection_rows:
                hydrated = repository.flow_repository.get_flow(UUID(flow_id))
                assert hydrated is not None
                presented = flow_response(hydrated)
                presented_created = datetime.fromisoformat(
                    str(presented["created"]).replace("Z", "+00:00")
                )
                runtime_created_rows[flow_id] = (
                    hydrated.created,
                    presented_created,
                )
                runtime_flow_rows[flow_id] = {
                    "profile_id": str(hydrated.profile_id)
                    if hydrated.profile_id is not None
                    else None,
                    "status": hydrated.status,
                    "init_segments": hydrated.init_segments,
                    "presented_profile_id": presented.get("profile_id"),
                    "presented_status": presented.get("status"),
                    "presented_init_segments": (
                        presented.get("essence_parameters") or {}
                    ).get("init_segments"),
                }
    finally:
        with postgres_connection.cursor() as cur:
            cur.execute(
                sql.SQL("DROP SCHEMA IF EXISTS {} CASCADE").format(
                    sql.Identifier(schema)
                )
            )

    assert version == "20260810_0007"
    assert source_rows == [
        (
            "10000000-0000-4000-8000-000000000001",
            datetime(2026, 5, 26, 9, tzinfo=UTC),
            "2026-05-26T09:00:00+00:00",
        ),
        (
            "10000000-0000-4000-8000-000000000002",
            datetime(2026, 6, 1, 10, tzinfo=UTC),
            "2026-06-01T10:00:00+00:00",
        ),
        (
            "10000000-0000-4000-8000-000000000003",
            datetime(2026, 6, 3, 11, tzinfo=UTC),
            "2026-06-03T11:00:00+00:00",
        ),
    ]
    assert runtime_source_created_rows == {
        "10000000-0000-4000-8000-000000000001": datetime(2026, 5, 26, 9, tzinfo=UTC),
        "10000000-0000-4000-8000-000000000002": datetime(2026, 6, 1, 10, tzinfo=UTC),
        "10000000-0000-4000-8000-000000000003": datetime(2026, 6, 3, 11, tzinfo=UTC),
    }
    assert flow_rows == [
        (
            "20000000-0000-4000-8000-000000000001",
            None,
            "replication_in_progress",
            False,
            "Migrated child flow",
            datetime(2026, 5, 27, 12, tzinfo=UTC),
            [],
        ),
        (
            "20000000-0000-4000-8000-000000000002",
            None,
            "closed_complete",
            False,
            "Migrated collection flow",
            datetime(2026, 6, 2, 12, tzinfo=UTC),
            ["20000000-0000-4000-8000-000000000001"],
        ),
        (
            "20000000-0000-4000-8000-000000000003",
            None,
            None,
            False,
            "Invalid extension values",
            datetime(2026, 5, 27, 12, tzinfo=UTC),
            [],
        ),
        (
            "20000000-0000-4000-8000-000000000004",
            None,
            "ingesting",
            False,
            "Noncanonical extension values",
            datetime(2026, 5, 27, 12, tzinfo=UTC),
            [],
        ),
        (
            "20000000-0000-4000-8000-000000000005",
            None,
            "awaiting_content",
            True,
            "Canonical textual boolean",
            datetime(2026, 5, 27, 12, tzinfo=UTC),
            [],
        ),
        (
            "20000000-0000-4000-8000-000000000006",
            None,
            "closed_complete",
            False,
            "Invalid UUID version and variant",
            datetime(2026, 5, 27, 12, tzinfo=UTC),
            [],
        ),
        (
            "20000000-0000-4000-8000-000000000007",
            None,
            "closed_complete",
            False,
            "Image with legacy init extension",
            datetime(2026, 5, 27, 12, tzinfo=UTC),
            [],
        ),
    ]
    child_created = datetime(2026, 5, 27, 12, tzinfo=UTC)
    collection_created = datetime(2026, 6, 2, 12, tzinfo=UTC)
    assert flow_created_rows == {
        "20000000-0000-4000-8000-000000000001": (
            child_created,
            child_created.isoformat(),
            child_created.isoformat(),
        ),
        "20000000-0000-4000-8000-000000000002": (
            collection_created,
            collection_created.isoformat(),
            collection_created.isoformat(),
        ),
        "20000000-0000-4000-8000-000000000003": (
            child_created,
            child_created.isoformat(),
            child_created.isoformat(),
        ),
        "20000000-0000-4000-8000-000000000004": (
            child_created,
            child_created.isoformat(),
            child_created.isoformat(),
        ),
        "20000000-0000-4000-8000-000000000005": (
            child_created,
            child_created.isoformat(),
            child_created.isoformat(),
        ),
        "20000000-0000-4000-8000-000000000006": (
            child_created,
            child_created.isoformat(),
            child_created.isoformat(),
        ),
        "20000000-0000-4000-8000-000000000007": (
            child_created,
            child_created.isoformat(),
            child_created.isoformat(),
        ),
    }
    assert flow_projection_rows == {
        "20000000-0000-4000-8000-000000000001": {
            "column_profile_id": None,
            "root_profile_id": None,
            "data_profile_id": None,
            "data_has_profile_id": False,
            "column_status": "replication_in_progress",
            "root_status": "replication_in_progress",
            "data_status": "replication_in_progress",
            "data_has_status": True,
            "column_init_segments": False,
            "root_init_segments": False,
            "data_init_segments": False,
            "data_init_segments_type": "boolean",
        },
        "20000000-0000-4000-8000-000000000002": {
            "column_profile_id": None,
            "root_profile_id": None,
            "data_profile_id": None,
            "data_has_profile_id": False,
            "column_status": "closed_complete",
            "root_status": "closed_complete",
            "data_status": "closed_complete",
            "data_has_status": True,
            "column_init_segments": False,
            "root_init_segments": False,
            "data_init_segments": None,
            "data_init_segments_type": None,
        },
        "20000000-0000-4000-8000-000000000003": {
            "column_profile_id": None,
            "root_profile_id": None,
            "data_profile_id": None,
            "data_has_profile_id": False,
            "column_status": None,
            "root_status": None,
            "data_status": None,
            "data_has_status": False,
            "column_init_segments": False,
            "root_init_segments": False,
            "data_init_segments": False,
            "data_init_segments_type": "boolean",
        },
        "20000000-0000-4000-8000-000000000004": {
            "column_profile_id": None,
            "root_profile_id": None,
            "data_profile_id": None,
            "data_has_profile_id": False,
            "column_status": "ingesting",
            "root_status": "ingesting",
            "data_status": "ingesting",
            "data_has_status": True,
            "column_init_segments": False,
            "root_init_segments": False,
            "data_init_segments": False,
            "data_init_segments_type": "boolean",
        },
        "20000000-0000-4000-8000-000000000005": {
            "column_profile_id": None,
            "root_profile_id": None,
            "data_profile_id": None,
            "data_has_profile_id": False,
            "column_status": "awaiting_content",
            "root_status": "awaiting_content",
            "data_status": "awaiting_content",
            "data_has_status": True,
            "column_init_segments": True,
            "root_init_segments": True,
            "data_init_segments": True,
            "data_init_segments_type": "boolean",
        },
        "20000000-0000-4000-8000-000000000006": {
            "column_profile_id": None,
            "root_profile_id": None,
            "data_profile_id": None,
            "data_has_profile_id": False,
            "column_status": "closed_complete",
            "root_status": "closed_complete",
            "data_status": "closed_complete",
            "data_has_status": True,
            "column_init_segments": False,
            "root_init_segments": False,
            "data_init_segments": False,
            "data_init_segments_type": "boolean",
        },
        "20000000-0000-4000-8000-000000000007": {
            "column_profile_id": None,
            "root_profile_id": None,
            "data_profile_id": None,
            "data_has_profile_id": False,
            "column_status": "closed_complete",
            "root_status": "closed_complete",
            "data_status": "closed_complete",
            "data_has_status": True,
            "column_init_segments": False,
            "root_init_segments": False,
            "data_init_segments": None,
            "data_init_segments_type": None,
        },
    }
    assert runtime_flow_rows == {
        "20000000-0000-4000-8000-000000000001": {
            "profile_id": None,
            "status": "replication_in_progress",
            "init_segments": False,
            "presented_profile_id": None,
            "presented_status": "replication_in_progress",
            "presented_init_segments": False,
        },
        "20000000-0000-4000-8000-000000000002": {
            "profile_id": None,
            "status": "closed_complete",
            "init_segments": False,
            "presented_profile_id": None,
            "presented_status": "closed_complete",
            "presented_init_segments": None,
        },
        "20000000-0000-4000-8000-000000000003": {
            "profile_id": None,
            "status": None,
            "init_segments": False,
            "presented_profile_id": None,
            "presented_status": None,
            "presented_init_segments": False,
        },
        "20000000-0000-4000-8000-000000000004": {
            "profile_id": None,
            "status": "ingesting",
            "init_segments": False,
            "presented_profile_id": None,
            "presented_status": "ingesting",
            "presented_init_segments": False,
        },
        "20000000-0000-4000-8000-000000000005": {
            "profile_id": None,
            "status": "awaiting_content",
            "init_segments": True,
            "presented_profile_id": None,
            "presented_status": "awaiting_content",
            "presented_init_segments": True,
        },
        "20000000-0000-4000-8000-000000000006": {
            "profile_id": None,
            "status": "closed_complete",
            "init_segments": False,
            "presented_profile_id": None,
            "presented_status": "closed_complete",
            "presented_init_segments": False,
        },
        "20000000-0000-4000-8000-000000000007": {
            "profile_id": None,
            "status": "closed_complete",
            "init_segments": False,
            "presented_profile_id": None,
            "presented_status": "closed_complete",
            "presented_init_segments": None,
        },
    }
    assert runtime_created_rows == {
        "20000000-0000-4000-8000-000000000001": (
            child_created,
            child_created,
        ),
        "20000000-0000-4000-8000-000000000002": (
            collection_created,
            collection_created,
        ),
        "20000000-0000-4000-8000-000000000003": (
            child_created,
            child_created,
        ),
        "20000000-0000-4000-8000-000000000004": (
            child_created,
            child_created,
        ),
        "20000000-0000-4000-8000-000000000005": (
            child_created,
            child_created,
        ),
        "20000000-0000-4000-8000-000000000006": (
            child_created,
            child_created,
        ),
        "20000000-0000-4000-8000-000000000007": (
            child_created,
            child_created,
        ),
    }
    assert delete_rows == [
        (
            "00000000-0000-4000-8000-000000000001",
            datetime(2026, 5, 1, 10, tzinfo=UTC),
            "2026-05-01T10:00:00+00:00",
        ),
        (
            "00000000-0000-4000-8000-000000000002",
            datetime(2026, 6, 2, 11, tzinfo=UTC),
            "2026-06-02T11:00:00+00:00",
        ),
    ]
    assert runtime_delete_created_rows == {
        "00000000-0000-4000-8000-000000000001": datetime(2026, 5, 1, 10, tzinfo=UTC),
        "00000000-0000-4000-8000-000000000002": datetime(2026, 6, 2, 11, tzinfo=UTC),
    }
    assert object_rows == [
        (
            "legacy/allocated.ts",
            "unassigned",
            "video/mp2t",
            None,
            "video/mp2t",
        ),
        (
            "legacy/media.ts",
            "media",
            "video/mp2t",
            "media",
            "video/mp2t",
        ),
    ]
    assert storage_tags == {}
    assert profile_count == 0
    assert segmented_flow_state == (False, False, False, None)
    assert "created_at DESC NULLS LAST" in delete_created_index


def _assert_tams_8_1_schema(connection: psycopg.Connection) -> None:
    with connection.cursor() as cur:
        cur.execute("SELECT to_regclass('tamoss_flows')::text")
        assert cur.fetchone()[0] == "tamoss_flows"
        cur.execute("SELECT to_regclass('tamoss_profiles')::text")
        assert cur.fetchone()[0] is None
        cur.execute(
            """
            SELECT table_name, column_name
            FROM information_schema.columns
            WHERE table_schema = current_schema()
              AND (
                (table_name = 'tamoss_storage_backends' AND column_name = 'tags')
                OR (table_name = 'tamoss_sources' AND column_name = 'created')
                OR (
                  table_name = 'tamoss_flows'
                  AND column_name IN (
                    'profile_id', 'status', 'init_segments', 'label', 'created',
                    'flow_collection_ids'
                  )
                )
                OR (
                  table_name = 'tamoss_media_objects'
                  AND column_name IN ('object_kind', 'content_type')
                )
                OR (
                  table_name = 'tamoss_segments'
                  AND column_name = 'init_object_id'
                )
              )
            ORDER BY table_name, column_name
            """
        )
        assert cur.fetchall() == []


def _seed_tams_8_1_upgrade_data(connection: psycopg.Connection) -> None:
    source_id = "10000000-0000-4000-8000-000000000001"
    fallback_source_id = "10000000-0000-4000-8000-000000000002"
    empty_source_id = "10000000-0000-4000-8000-000000000003"
    flow_id = "20000000-0000-4000-8000-000000000001"
    collection_flow_id = "20000000-0000-4000-8000-000000000002"
    invalid_extension_flow_id = "20000000-0000-4000-8000-000000000003"
    noncanonical_extension_flow_id = "20000000-0000-4000-8000-000000000004"
    textual_boolean_flow_id = "20000000-0000-4000-8000-000000000005"
    invalid_uuid_bits_flow_id = "20000000-0000-4000-8000-000000000006"
    image_extension_flow_id = "20000000-0000-4000-8000-000000000007"
    profile_id = "30000000-0000-4000-8000-000000000001"
    created = "2026-05-27T12:00:00+00:00"
    flow_data = {
        "id": flow_id,
        "source_id": source_id,
        "format": "urn:x-nmos:format:video",
        "codec": "video/h264",
        "container": "video/mp2t",
        "profile_id": profile_id,
        "status": "replication_in_progress",
        "label": "Migrated child flow",
        "created": created,
        "essence_parameters": {
            "frame_width": 1920,
            "frame_height": 1080,
            "frame_rate": {"numerator": 25, "denominator": 1},
            "init_segments": True,
        },
    }
    collection_created = "2026-06-02T12:00:00+00:00"
    collection_flow_data = {
        "id": collection_flow_id,
        "source_id": source_id,
        "format": "urn:x-nmos:format:video",
        "codec": "video/h264",
        "container": "video/mp2t",
        "status": "closed_complete",
        "label": "Migrated collection flow",
        "flow_collection": [{"id": flow_id, "role": "primary"}],
        "essence_parameters": {
            "frame_width": 1920,
            "frame_height": 1080,
            "frame_rate": {"numerator": 25, "denominator": 1},
        },
    }
    with connection.cursor() as cur:
        cur.execute(
            """
            INSERT INTO tamoss_storage_backends (
              id, label, provider, region, store_product, store_type,
              default_storage, record
            ) VALUES (%s, %s, %s, %s, %s, %s, FALSE, %s)
            """,
            (
                "40000000-0000-4000-8000-000000000001",
                "Legacy storage",
                "tamoss",
                "us-east-1",
                "s3",
                "http_object_store",
                Jsonb({"id": "40000000-0000-4000-8000-000000000001"}),
            ),
        )
        extension_flow_data = (
            (
                invalid_extension_flow_id,
                {
                    "profile_id": "not-a-uuid",
                    "status": "not-a-status",
                    "init_segments": "not-a-boolean",
                    "label": "Invalid extension values",
                },
            ),
            (
                noncanonical_extension_flow_id,
                {
                    # PostgreSQL accepts this compact UUID form, but the 8.2
                    # contract requires the canonical hyphenated representation.
                    "profile_id": "30000000000040008000000000000002",
                    "status": "ingesting",
                    "init_segments": "TRUE",
                    "label": "Noncanonical extension values",
                },
            ),
            (
                textual_boolean_flow_id,
                {
                    "profile_id": "30000000-0000-4000-8000-000000000003",
                    "status": "awaiting_content",
                    "init_segments": "true",
                    "label": "Canonical textual boolean",
                },
            ),
            (
                invalid_uuid_bits_flow_id,
                {
                    # PostgreSQL accepts the shape, but version 0 and variant 0
                    # are outside the UUID contract used by Flow responses.
                    "profile_id": "30000000-0000-0000-0000-000000000006",
                    "status": "closed_complete",
                    "init_segments": False,
                    "label": "Invalid UUID version and variant",
                },
            ),
        )
        cur.executemany(
            """
            INSERT INTO tamoss_flows (
              id, source_id, format, container, read_only, tags, record,
              metadata_updated, created_at
            ) VALUES (
              %s, %s, 'urn:x-nmos:format:video', 'video/mp2t', FALSE,
              '{}'::jsonb, %s, %s, %s
            )
            """,
            (
                (
                    extension_flow_id,
                    source_id,
                    Jsonb(
                        {
                            "id": extension_flow_id,
                            "data": {
                                "id": extension_flow_id,
                                "source_id": source_id,
                                "format": "urn:x-nmos:format:video",
                                "codec": "video/h264",
                                "container": "video/mp2t",
                                "profile_id": values["profile_id"],
                                "status": values["status"],
                                "label": values["label"],
                                "created": created,
                                "essence_parameters": {
                                    "frame_width": 1920,
                                    "frame_height": 1080,
                                    "frame_rate": {
                                        "numerator": 25,
                                        "denominator": 1,
                                    },
                                    "init_segments": values["init_segments"],
                                },
                            },
                            "source_id": source_id,
                            "format": "urn:x-nmos:format:video",
                            "container": "video/mp2t",
                            "read_only": False,
                            "tags": {},
                            "metadata_updated": created,
                        }
                    ),
                    created,
                    created,
                )
                for extension_flow_id, values in extension_flow_data
            ),
        )
        cur.execute(
            """
            INSERT INTO tamoss_flows (
              id, source_id, format, container, read_only, tags, record,
              metadata_updated, created_at
            ) VALUES (
              %s, %s, 'urn:x-tam:format:image', 'image/jpeg', FALSE,
              '{}'::jsonb, %s, %s, %s
            )
            """,
            (
                image_extension_flow_id,
                source_id,
                Jsonb(
                    {
                        "id": image_extension_flow_id,
                        "data": {
                            "id": image_extension_flow_id,
                            "source_id": source_id,
                            "format": "urn:x-tam:format:image",
                            "codec": "image/jpeg",
                            "container": "image/jpeg",
                            "status": "closed_complete",
                            "label": "Image with legacy init extension",
                            "created": created,
                            "essence_parameters": {
                                "frame_width": 1920,
                                "frame_height": 1080,
                                "init_segments": True,
                            },
                        },
                        "source_id": source_id,
                        "format": "urn:x-tam:format:image",
                        "container": "image/jpeg",
                        "read_only": False,
                        "tags": {},
                        "metadata_updated": created,
                    }
                ),
                created,
                created,
            ),
        )
        cur.execute(
            """
            INSERT INTO tamoss_sources (
              id, format, label, tags, record, metadata_updated, created_at
            ) VALUES
              (%s, %s, %s, '{}'::jsonb, %s, %s, %s),
              (%s, %s, %s, '{}'::jsonb, %s, %s, %s),
              (%s, %s, %s, '{}'::jsonb, %s, %s, %s)
            """,
            (
                source_id,
                "urn:x-nmos:format:video",
                "Migrated source",
                Jsonb(
                    {
                        "id": str(source_id),
                        "format": "urn:x-nmos:format:video",
                        "label": "Migrated source",
                        "created": "2026-05-26T09:00:00+00:00",
                        "metadata_updated": created,
                        "tags": {},
                    }
                ),
                created,
                "2026-08-01T09:00:00+00:00",
                fallback_source_id,
                "urn:x-nmos:format:audio",
                "Migrated fallback source",
                Jsonb(
                    {
                        "id": fallback_source_id,
                        "format": "urn:x-nmos:format:audio",
                        "label": "Migrated fallback source",
                        "metadata_updated": "2026-06-01T10:00:00+00:00",
                        "tags": {},
                    }
                ),
                "2026-06-01T10:00:00+00:00",
                "2026-06-01T10:00:00+00:00",
                empty_source_id,
                "urn:x-nmos:format:data",
                "Migrated empty-created source",
                Jsonb(
                    {
                        "id": empty_source_id,
                        "format": "urn:x-nmos:format:data",
                        "label": "Migrated empty-created source",
                        "created": "",
                        "metadata_updated": "2026-06-03T11:00:00+00:00",
                        "tags": {},
                    }
                ),
                "2026-06-03T11:00:00+00:00",
                "2026-06-03T11:00:00+00:00",
            ),
        )
        cur.execute(
            """
            INSERT INTO tamoss_flows (
              id, source_id, format, container, read_only, tags, record,
              metadata_updated, created_at
            ) VALUES
              (%s, %s, %s, %s, FALSE, '{}'::jsonb, %s, %s, %s),
              (%s, %s, %s, %s, FALSE, '{}'::jsonb, %s, %s, %s)
            """,
            (
                flow_id,
                source_id,
                "urn:x-nmos:format:video",
                "video/mp2t",
                Jsonb(
                    {
                        "id": str(flow_id),
                        "data": flow_data,
                        "source_id": str(source_id),
                        "format": "urn:x-nmos:format:video",
                        "container": "video/mp2t",
                        "read_only": False,
                        "tags": {},
                        "metadata_updated": created,
                    }
                ),
                created,
                "2026-08-01T12:00:00+00:00",
                collection_flow_id,
                source_id,
                "urn:x-nmos:format:video",
                "video/mp2t",
                Jsonb(
                    {
                        "id": collection_flow_id,
                        "data": collection_flow_data,
                        "source_id": source_id,
                        "format": "urn:x-nmos:format:video",
                        "container": "video/mp2t",
                        "read_only": False,
                        "tags": {},
                        "metadata_updated": collection_created,
                    }
                ),
                collection_created,
                collection_created,
            ),
        )
        cur.execute(
            """
            INSERT INTO tamoss_media_objects (
              id, first_referenced_by_flow, referenced_by_flows, record
            ) VALUES
              (%s, NULL, ARRAY[]::text[], %s),
              (%s, %s, %s, %s)
            """,
            (
                "legacy/allocated.ts",
                Jsonb(
                    {
                        "id": "legacy/allocated.ts",
                        "allocated_by_flow": flow_id,
                        "referenced_by_flows": [],
                        "instances": [],
                    }
                ),
                "legacy/media.ts",
                flow_id,
                [str(flow_id)],
                Jsonb(
                    {
                        "id": "legacy/media.ts",
                        "timerange": "[0:0_10:0)",
                        "first_referenced_by_flow": str(flow_id),
                        "referenced_by_flows": [str(flow_id)],
                        "instances": [],
                    }
                ),
            ),
        )
        cur.execute(
            """
            INSERT INTO tamoss_segments (
              flow_id, object_id, timerange, timerange_start, timerange_end,
              record, created
            ) VALUES (%s, %s, %s, 0, 10000000000, %s, %s)
            """,
            (
                flow_id,
                "legacy/media.ts",
                "[0:0_10:0)",
                Jsonb(
                    {
                        "flow_id": str(flow_id),
                        "object_id": "legacy/media.ts",
                        "timerange": "[0:0_10:0)",
                        "created": created,
                    }
                ),
                created,
            ),
        )
        cur.execute(
            """
            INSERT INTO tamoss_delete_requests (
              id, flow_id, status, record, updated, created_at
            ) VALUES
              (%s, %s, 'created', %s, %s, %s),
              (%s, %s, 'created', %s, %s, %s)
            """,
            (
                "00000000-0000-4000-8000-000000000001",
                flow_id,
                Jsonb(
                    {
                        "id": "00000000-0000-4000-8000-000000000001",
                        "flow_id": flow_id,
                        "timerange_to_delete": "[0:0_1:0)",
                        "delete_flow": False,
                        "created": "2026-05-01T10:00:00+00:00",
                        "updated": "2026-05-01T10:00:00+00:00",
                        "status": "created",
                    }
                ),
                "2026-05-01T10:00:00+00:00",
                "2026-08-01T10:00:00+00:00",
                "00000000-0000-4000-8000-000000000002",
                flow_id,
                Jsonb(
                    {
                        "id": "00000000-0000-4000-8000-000000000002",
                        "flow_id": flow_id,
                        "timerange_to_delete": "[1:0_2:0)",
                        "delete_flow": False,
                        "updated": "2026-06-02T11:00:00+00:00",
                        "status": "created",
                    }
                ),
                "2026-06-02T11:00:00+00:00",
                "2026-06-02T11:00:00+00:00",
            ),
        )


def test_schema_and_bootstrap_sql_create_tables_without_storage_backend_seed(
    postgres_connection: psycopg.Connection,
) -> None:
    execute_sql_file(postgres_connection, SCHEMA_ASSETS_DIR / "schema.sql")
    execute_sql_file(postgres_connection, SCHEMA_ASSETS_DIR / "bootstrap.sql")

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
        "tamoss_object_cleanups",
        "tamoss_object_copies",
        "tamoss_profiles",
        "tamoss_segments",
        "tamoss_service_metadata",
        "tamoss_sources",
        "tamoss_storage_backends",
        "tamoss_webhook_deliveries",
        "tamoss_webhooks",
    } <= tables
    assert rows == []


def test_repository_does_not_register_storage_backend_by_default(
    postgres_connection: psycopg.Connection,
) -> None:
    repo = PostgresRepository(
        connection=postgres_connection,
        storage_backend=primary_backend(),
    )

    assert repo.service_repository.list_storage_backends() == []
    assert repo.storage_repository.default_storage_backend() is None


def test_repository_persists_service_metadata_and_storage_backend_default(
    postgres_connection: psycopg.Connection,
) -> None:
    repo = PostgresRepository(
        connection=postgres_connection,
        storage_backend=primary_backend(),
        register_storage_backend=True,
    )
    repo.service_repository.save_service_metadata(
        ServiceMetadata(name="TAMOSS adapter test", description="Postgres")
    )

    replacement = replacement_backend()
    repo = PostgresRepository(
        connection=postgres_connection,
        storage_backend=replacement,
        register_storage_backend=True,
    )

    assert repo.service_repository.get_service_metadata() == ServiceMetadata(
        name="TAMOSS adapter test",
        description="Postgres",
    )
    assert repo.storage_repository.default_storage_backend() == replacement

    with postgres_connection.cursor() as cur:
        cur.execute(
            """
            SELECT id, default_storage, record->>'default_storage'
            FROM tamoss_storage_backends
            ORDER BY id
            """
        )
        default_rows = cur.fetchall()

    assert default_rows == [
        (PRIMARY_BACKEND_ID, False, "false"),
        (REPLACEMENT_BACKEND_ID, True, "true"),
    ]
