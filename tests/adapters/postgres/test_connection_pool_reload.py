from __future__ import annotations

from uuid import UUID

from tamoss.adapters.postgres import PostgresRepository
from tamoss.domain.model import StorageBackend


def test_postgres_pool_reopens_when_database_url_provider_changes(monkeypatch) -> None:
    opened: list[_FakePool] = []

    def fake_pool(**kwargs) -> _FakePool:
        pool = _FakePool(kwargs["conninfo"])
        opened.append(pool)
        return pool

    monkeypatch.setattr(
        "tamoss.adapters.postgres_repository.connection.ConnectionPool",
        fake_pool,
    )
    urls = ["postgresql://user:first@db/tams"]
    repository = PostgresRepository(
        database_url_provider=lambda: urls[-1],
        storage_backend=_backend(),
    )

    assert opened[0].conninfo == "postgresql://user:first@db/tams"

    urls.append("postgresql://user:second@db/tams")
    repository._ensure_pool_current()

    assert opened[0].closed is True
    assert opened[1].conninfo == "postgresql://user:second@db/tams"


class _FakePool:
    def __init__(self, conninfo: str) -> None:
        self.conninfo = conninfo
        self.closed = False

    def close(self) -> None:
        self.closed = True


def _backend() -> StorageBackend:
    return StorageBackend(
        id=UUID("11111111-1111-4111-8111-111111111111"),
        label="tamoss.storage.primary",
        provider="tamoss",
        region="us-east-1",
        store_product="s3",
        default_storage=True,
        bucket_name="tamoss",
        endpoint_url="https://objects.internal.example.test",
        public_endpoint_url="https://objects.example.test",
        access_key="access",
        secret_key="secret",
    )
