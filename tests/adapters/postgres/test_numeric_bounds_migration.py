from __future__ import annotations

from collections.abc import Iterator
from pathlib import Path
from urllib.parse import urlencode
from uuid import uuid4

import psycopg
import pytest
from alembic import command
from alembic.config import Config
from psycopg import sql
from sqlalchemy import event
from sqlalchemy.engine import Engine
from tamoss.adapters.postgres import PostgresRepository
from tamoss.db.migrations import CURRENT_SCHEMA_REVISION
from tamoss.db.migrations.runner import _sqlalchemy_url, alembic_config
from tamoss.domain.model import FlowRecord, MediaObjectRecord, SegmentRecord

from tests.adapters.postgres.support import (
    database_url,
    execute_sql_file,
    primary_backend,
)

pytestmark = pytest.mark.needs_db
PREVIOUS_SCHEMA = Path(__file__).with_name("fixtures") / "tams_8_2_schema_0007.sql"


@pytest.fixture()
def migration_database(
    postgres_connection: psycopg.Connection,
) -> Iterator[tuple[psycopg.Connection, Config]]:
    schema = f"tamoss_numeric_upgrade_{uuid4().hex}"
    postgres_connection.execute(
        sql.SQL("CREATE SCHEMA {}").format(sql.Identifier(schema))
    )
    options = f"-csearch_path={schema}"
    base_url = _sqlalchemy_url(database_url())
    separator = "&" if "?" in base_url else "?"
    config = alembic_config(f"{base_url}{separator}{urlencode({'options': options})}")
    try:
        with psycopg.connect(
            database_url(), options=options, autocommit=True
        ) as connection:
            yield connection, config
    finally:
        postgres_connection.execute(
            sql.SQL("DROP SCHEMA {} CASCADE").format(sql.Identifier(schema))
        )


def _bounds_types(connection: psycopg.Connection) -> list[tuple[str, str]]:
    return connection.execute(
        """
        SELECT data_type, is_nullable FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'tamoss_segments'
          AND column_name IN ('timerange_start', 'timerange_end')
        ORDER BY column_name
        """
    ).fetchall()


def _seed_previous_schema(
    connection: psycopg.Connection, config: Config
) -> PostgresRepository:
    execute_sql_file(connection, PREVIOUS_SCHEMA)
    command.stamp(config, "20260810_0007")
    repo = PostgresRepository(connection=connection, storage_backend=primary_backend())
    flow = FlowRecord(
        id=uuid4(),
        source_id=uuid4(),
        data={"label": "Existing media"},
        format="urn:x-nmos:format:video",
        container="video/mp2t",
    )
    segments = [
        SegmentRecord(flow_id=flow.id, object_id=str(uuid4()), timerange=timerange)
        for timerange in ("[-2:3_-1:4)", "[0:1_1:2)", "[3:4]")
    ]
    repo.segment_repository.save_registered_segments(
        flow=flow,
        media_objects=[MediaObjectRecord(id=s.object_id) for s in segments],
        segments=segments,
    )
    assert _bounds_types(connection) == [("bigint", "NO"), ("bigint", "NO")]
    return repo


def test_populated_rc2_upgrade_preserves_metadata_and_indexes(
    migration_database,
) -> None:
    connection, config = migration_database
    repo = _seed_previous_schema(connection, config)
    records = {
        table: connection.execute(
            sql.SQL("SELECT to_jsonb(t) FROM {} t ORDER BY to_jsonb(t)::text").format(
                sql.Identifier(table)
            )
        ).fetchall()
        for table in ("tamoss_flows", "tamoss_media_objects", "tamoss_segments")
    }
    indexes = connection.execute(
        "SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() "
        "AND tablename = 'tamoss_segments' ORDER BY indexname"
    ).fetchall()
    command.upgrade(config, "head")
    assert _bounds_types(connection) == [("numeric", "NO"), ("numeric", "NO")]
    assert (
        connection.execute("SELECT version_num FROM alembic_version").fetchone()[0]
        == CURRENT_SCHEMA_REVISION
    )
    for table, expected in records.items():
        assert (
            connection.execute(
                sql.SQL(
                    "SELECT to_jsonb(t) FROM {} t ORDER BY to_jsonb(t)::text"
                ).format(sql.Identifier(table))
            ).fetchall()
            == expected
        )
    assert (
        connection.execute(
            "SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() "
            "AND tablename = 'tamoss_segments' ORDER BY indexname"
        ).fetchall()
        == indexes
    )
    segment = SegmentRecord(
        flow_id=connection.execute("SELECT id FROM tamoss_flows").fetchone()[0],
        object_id=str(uuid4()),
        timerange="[9999999998:1_10000000002:2)",
    )
    repo.segment_repository.append_segment(segment)
    assert segment in repo.segment_repository.list_segments(segment.flow_id)
    file_node = connection.execute(
        "SELECT pg_relation_filenode('tamoss_segments')"
    ).fetchone()[0]
    command.upgrade(config, "head")
    assert (
        connection.execute("SELECT pg_relation_filenode('tamoss_segments')").fetchone()[
            0
        ]
        == file_node
    )


def test_numeric_bounds_migration_is_atomic_and_retryable(migration_database) -> None:
    connection, config = migration_database
    _seed_previous_schema(connection, config)

    def fail_after_conversion(
        conn, cursor, statement, parameters, context, executemany
    ):
        if statement == "ANALYZE tamoss_segments":
            raise RuntimeError("Interrupted after type conversion")

    event.listen(Engine, "before_cursor_execute", fail_after_conversion)
    try:
        with pytest.raises(RuntimeError, match="Interrupted after type conversion"):
            command.upgrade(config, "head")
    finally:
        event.remove(Engine, "before_cursor_execute", fail_after_conversion)
    assert _bounds_types(connection) == [("bigint", "NO"), ("bigint", "NO")]
    assert (
        connection.execute("SELECT version_num FROM alembic_version").fetchone()[0]
        == "20260810_0007"
    )
    assert connection.execute("SELECT count(*) FROM tamoss_segments").fetchone()[0] == 3
    command.upgrade(config, "head")
    assert _bounds_types(connection) == [("numeric", "NO"), ("numeric", "NO")]
    assert (
        connection.execute("SELECT version_num FROM alembic_version").fetchone()[0]
        == CURRENT_SCHEMA_REVISION
    )


def test_fresh_install_has_numeric_bounds(migration_database) -> None:
    connection, config = migration_database
    command.upgrade(config, "head")
    assert _bounds_types(connection) == [("numeric", "NO"), ("numeric", "NO")]
    assert (
        connection.execute("SELECT version_num FROM alembic_version").fetchone()[0]
        == CURRENT_SCHEMA_REVISION
    )
