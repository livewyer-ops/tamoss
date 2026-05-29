from __future__ import annotations

# mypy: disable-error-code=attr-defined
# Focused PostgreSQL stores are invoked through the repository facade and share
# one connection and transaction boundary.
from collections.abc import Callable, Iterator
from contextlib import contextmanager
from contextvars import ContextVar
from typing import Any

import psycopg
from psycopg_pool import ConnectionPool

from tamoss.db.migrations.runner import MultipleAlembicHeads
from tamoss.domain.model import StorageBackend


class PostgresConnectionMixin:
    def __init__(
        self,
        *,
        storage_backend: StorageBackend,
        database_url: str | None = None,
        database_url_provider: Callable[[], str | None] | None = None,
        connection: Any | None = None,
        pool: ConnectionPool | None = None,
        pool_min_size: int = 1,
        pool_max_size: int = 10,
    ) -> None:
        if database_url is None and database_url_provider is not None:
            database_url = database_url_provider()
        if database_url is None and connection is None and pool is None:
            raise ValueError("database_url, connection, or pool is required")
        self._database_url = database_url
        self._database_url_provider = database_url_provider
        self._connection = connection
        self._pool = pool
        self._owns_pool = False
        self._pool_min_size = pool_min_size
        self._pool_max_size = pool_max_size
        self._configured_storage_backend = storage_backend
        self._transaction_connection: ContextVar[Any | None] = ContextVar(
            "tamoss_postgres_transaction_connection",
            default=None,
        )
        if self._pool is None and self._connection is None:
            self._open_pool()
            self._owns_pool = True

    @contextmanager
    def unit_of_work(self) -> Iterator[Any]:
        active_connection = self._transaction_connection.get()
        if active_connection is not None:
            yield self
            return

        if self._connection is not None:
            with self._connection.transaction():
                token = self._transaction_connection.set(self._connection)
                try:
                    yield self
                finally:
                    self._transaction_connection.reset(token)
            return

        if self._pool is not None:
            self._ensure_pool_current()
            with self._pool.connection() as conn, conn.transaction():
                token = self._transaction_connection.set(conn)
                try:
                    yield self
                finally:
                    self._transaction_connection.reset(token)
            return

        assert self._database_url is not None
        conn = psycopg.connect(self._database_url)
        try:
            with conn.transaction():
                token = self._transaction_connection.set(conn)
                try:
                    yield self
                finally:
                    self._transaction_connection.reset(token)
        finally:
            conn.close()

    @contextmanager
    def _connect(self) -> Iterator[Any]:
        active_connection = self._transaction_connection.get()
        if active_connection is not None:
            yield active_connection
            return
        if self._connection is not None:
            yield self._connection
            return
        if self._pool is not None:
            self._ensure_pool_current()
            with self._pool.connection() as conn:
                yield conn
            return
        assert self._database_url is not None
        conn = psycopg.connect(self._database_url)
        try:
            yield conn
            conn.commit()
        except Exception:
            conn.rollback()
            raise
        finally:
            conn.close()

    def close(self) -> None:
        if self._owns_pool and self._pool is not None:
            self._pool.close()

    def _open_pool(self) -> None:
        assert self._database_url is not None
        self._pool = ConnectionPool(
            conninfo=self._database_url,
            min_size=self._pool_min_size,
            max_size=max(self._pool_min_size, self._pool_max_size),
            open=True,
            name="tamoss-postgres",
        )

    def _ensure_pool_current(self) -> None:
        if not self._owns_pool or self._database_url_provider is None:
            return
        current = self._database_url_provider()
        if current is None or current == self._database_url:
            return
        if self._pool is not None:
            self._pool.close()
        self._database_url = current
        self._open_pool()

    def check_connection(self) -> None:
        with self._connect() as conn:
            conn.execute("SELECT 1").fetchone()

    def current_schema_revision(self) -> str | None:
        with self._connect() as conn:
            exists = conn.execute(
                "SELECT to_regclass('public.alembic_version') IS NOT NULL"
            ).fetchone()
            if not exists or not exists[0]:
                return None
            rows = conn.execute("SELECT version_num FROM alembic_version").fetchall()
            if not rows:
                return None
            if len(rows) > 1:
                raise MultipleAlembicHeads("multiple Alembic heads are not supported")
            return str(rows[0][0])
