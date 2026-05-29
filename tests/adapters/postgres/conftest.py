from __future__ import annotations

from collections.abc import Iterator
from uuid import uuid4

import psycopg
import pytest
from psycopg import sql
from tamoss.adapters.postgres import PostgresRepository

from tests.adapters.postgres.support import (
    SCHEMA_ASSETS_DIR,
    database_url,
    execute_sql_file,
    primary_backend,
)


@pytest.fixture()
def postgres_connection() -> Iterator[psycopg.Connection]:
    schema = f"tamoss_test_{uuid4().hex}"
    try:
        admin = psycopg.connect(database_url(), connect_timeout=2)
    except psycopg.OperationalError as exc:
        pytest.skip(f"Postgres test database is unavailable: {exc}")
    admin.autocommit = True
    with admin.cursor() as cur:
        cur.execute(sql.SQL("CREATE SCHEMA {}").format(sql.Identifier(schema)))

    conn = psycopg.connect(database_url(), connect_timeout=2)
    conn.autocommit = True
    with conn.cursor() as cur:
        cur.execute(sql.SQL("SET search_path TO {}").format(sql.Identifier(schema)))
    execute_sql_file(conn, SCHEMA_ASSETS_DIR / "schema.sql")

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
        storage_backend=primary_backend(),
        register_storage_backend=True,
    )
