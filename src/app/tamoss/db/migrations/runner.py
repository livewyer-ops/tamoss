from __future__ import annotations

import os
from importlib import resources

from alembic import command
from alembic.config import Config
from sqlalchemy import create_engine, inspect, text
from sqlalchemy.engine import Connection
from tamoss.db.migrations import (
    CURRENT_SCHEMA_REVISION,
    PREVIOUS_SUPPORTED_SCHEMA_REVISION,
)
from tamoss.settings import Settings, get_settings


class UnsupportedSchemaRevision(RuntimeError):
    pass


class MultipleAlembicHeads(UnsupportedSchemaRevision):
    pass


def migrate(
    *,
    revision: str = "head",
    apply_fixtures: bool = False,
    apply_cnpg_ownership: bool = False,
    settings: Settings | None = None,
) -> None:
    resolved_url = (settings or get_settings()).database_url_value()
    if resolved_url is None:
        raise RuntimeError("POSTGRES_HOST is required")
    sqlalchemy_url = _sqlalchemy_url(resolved_url)

    engine = create_engine(sqlalchemy_url)
    try:
        with engine.begin() as connection:
            validate_supported_revision(observed_revision(connection))

        config = alembic_config(sqlalchemy_url)
        command.upgrade(config, revision)

        with engine.begin() as connection:
            if apply_fixtures or _env_enabled("TAMOSS_SCHEMA_APPLY_FIXTURES"):
                _execute_asset(connection, "fixtures.sql")
            if apply_cnpg_ownership or _env_enabled(
                "TAMOSS_SCHEMA_APPLY_CNPG_OWNERSHIP"
            ):
                connection.execute(text(CNPG_SCHEMA_OWNERSHIP_SQL))
    finally:
        engine.dispose()


def alembic_config(database_url: str) -> Config:
    script_location = resources.files("tamoss.db.migrations")
    config = Config()
    config.set_main_option("script_location", str(script_location))
    config.set_main_option("sqlalchemy.url", database_url.replace("%", "%%"))
    return config


def observed_revision(connection: Connection) -> str | None:
    inspector = inspect(connection)
    if not inspector.has_table("alembic_version"):
        return None
    rows = connection.execute(text("SELECT version_num FROM alembic_version")).all()
    if not rows:
        return None
    if len(rows) > 1:
        raise MultipleAlembicHeads("multiple Alembic heads are not supported")
    return str(rows[0][0])


def validate_supported_revision(observed: str | None) -> None:
    if observed in {None, "", CURRENT_SCHEMA_REVISION}:
        return
    if (
        PREVIOUS_SUPPORTED_SCHEMA_REVISION
        and observed == PREVIOUS_SUPPORTED_SCHEMA_REVISION
    ):
        return
    supported = [CURRENT_SCHEMA_REVISION]
    if PREVIOUS_SUPPORTED_SCHEMA_REVISION:
        supported.append(PREVIOUS_SUPPORTED_SCHEMA_REVISION)
    supported_text = ", ".join(supported)
    raise UnsupportedSchemaRevision(
        f"database schema revision {observed!r} is unsupported; "
        f"supported revisions: {supported_text}"
    )


def _execute_asset(connection: Connection, name: str) -> None:
    asset = resources.files("tamoss.db.migrations").joinpath("assets", name)
    connection.execute(text(asset.read_text(encoding="utf-8")))


def _sqlalchemy_url(database_url: str) -> str:
    if database_url.startswith("postgresql://"):
        return "postgresql+psycopg://" + database_url.removeprefix("postgresql://")
    if database_url.startswith("postgres://"):
        return "postgresql+psycopg://" + database_url.removeprefix("postgres://")
    return database_url


def _env_enabled(name: str) -> bool:
    return os.getenv(name, "").strip().lower() in {"1", "true", "yes", "on"}


CNPG_SCHEMA_OWNERSHIP_SQL = """
DO $tamoss$
DECLARE
  item record;
BEGIN
  EXECUTE 'ALTER SCHEMA public OWNER TO tams';
  FOR item IN
    SELECT format('%I.%I', schemaname, tablename) AS name
    FROM pg_tables
    WHERE schemaname = 'public'
  LOOP
    EXECUTE 'ALTER TABLE ' || item.name || ' OWNER TO tams';
  END LOOP;
  FOR item IN
    SELECT format('%I.%I', schemaname, sequencename) AS name
    FROM pg_sequences
    WHERE schemaname = 'public'
  LOOP
    EXECUTE 'ALTER SEQUENCE ' || item.name || ' OWNER TO tams';
  END LOOP;
  FOR item IN
    SELECT format('%I.%I', schemaname, viewname) AS name
    FROM pg_views
    WHERE schemaname = 'public'
  LOOP
    EXECUTE 'ALTER VIEW ' || item.name || ' OWNER TO tams';
  END LOOP;
END
$tamoss$;
GRANT ALL PRIVILEGES ON SCHEMA public TO tams;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO tams;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO tams;
"""
