from __future__ import annotations

import pytest
from sqlalchemy import create_engine, text
from tamoss.db.migrations import CURRENT_SCHEMA_REVISION, runner
from tamoss.db.migrations.runner import (
    MultipleAlembicHeads,
    UnsupportedSchemaRevision,
    _sqlalchemy_url,
    validate_supported_revision,
)


def test_current_schema_revision_is_supported() -> None:
    validate_supported_revision(CURRENT_SCHEMA_REVISION)


def test_empty_database_is_supported_for_fresh_install() -> None:
    validate_supported_revision(None)


def test_unknown_schema_revision_is_rejected() -> None:
    with pytest.raises(UnsupportedSchemaRevision):
        validate_supported_revision("unknown")


def test_observed_revision_rejects_multiple_alembic_heads() -> None:
    engine = create_engine("sqlite://")
    try:
        with engine.begin() as connection:
            connection.execute(text("CREATE TABLE alembic_version (version_num TEXT)"))
            connection.execute(
                text(
                    "INSERT INTO alembic_version (version_num) "
                    "VALUES ('revision-a'), ('revision-b')"
                )
            )

            with pytest.raises(MultipleAlembicHeads):
                runner.observed_revision(connection)
    finally:
        engine.dispose()


def test_postgres_urls_use_psycopg_driver() -> None:
    assert (
        _sqlalchemy_url("postgresql://user:pass@db/tams")
        == "postgresql+psycopg://user:pass@db/tams"
    )
    assert (
        _sqlalchemy_url("postgres://user:pass@db/tams")
        == "postgresql+psycopg://user:pass@db/tams"
    )


def test_migrate_uses_settings_database_url_value(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    captured: dict[str, str | bool] = {}

    class FakeSettings:
        def database_url_value(self) -> str:
            return "postgresql://user:p%40ss@db/tams"

    class BeginContext:
        def __enter__(self) -> object:
            return object()

        def __exit__(self, *args: object) -> bool:
            return False

    class FakeEngine:
        def begin(self) -> BeginContext:
            return BeginContext()

        def dispose(self) -> None:
            captured["disposed"] = True

    def fake_create_engine(url: str) -> FakeEngine:
        captured["url"] = url
        return FakeEngine()

    def fake_upgrade(config: object, revision: str) -> None:
        captured["revision"] = revision
        captured["config_url"] = config.get_main_option("sqlalchemy.url")

    monkeypatch.setattr(runner, "create_engine", fake_create_engine)
    monkeypatch.setattr(runner, "observed_revision", lambda connection: None)
    monkeypatch.setattr(runner.command, "upgrade", fake_upgrade)

    runner.migrate(settings=FakeSettings())

    assert captured == {
        "url": "postgresql+psycopg://user:p%40ss@db/tams",
        "revision": "head",
        "config_url": "postgresql+psycopg://user:p%40ss@db/tams",
        "disposed": True,
    }
