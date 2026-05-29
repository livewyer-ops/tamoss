from __future__ import annotations

import psycopg
import pytest
from tamoss.adapters.postgres import PostgresRepository
from tamoss.domain.model import ServiceMetadata

from tests.adapters.postgres.support import (
    PRIMARY_BACKEND_ID,
    REPLACEMENT_BACKEND_ID,
    SCHEMA_ASSETS_DIR,
    execute_sql_file,
    primary_backend,
    replacement_backend,
)

pytestmark = pytest.mark.needs_db


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
