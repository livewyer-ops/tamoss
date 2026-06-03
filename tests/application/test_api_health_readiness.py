from __future__ import annotations

import logging
from uuid import UUID

import psycopg
import pytest
from fastapi import FastAPI
from fastapi.testclient import TestClient
from tamoss.app import create_app
from tamoss.application.use_cases import TamossUseCases
from tamoss.bootstrap import StartupConfigurationError
from tamoss.db.migrations import CURRENT_SCHEMA_REVISION
from tamoss.db.migrations.runner import MultipleAlembicHeads, UnsupportedSchemaRevision
from tamoss.domain.model import StorageBackend
from tamoss.settings import Settings, StorageBackendSettings

from tests.support.object_storage import InMemoryObjectStorage


def test_readyz_reports_normal_ready_state() -> None:
    client = _client(_Repository([_storage_backend()]))

    response = client.get("/readyz")

    assert response.status_code == 200
    assert response.json()["status"] == "ready"
    assert response.json()["checks"]["repository"] == {
        "ok": True,
        "reason": "RepositoryReady",
        "schemaRevision": CURRENT_SCHEMA_REVISION,
    }
    assert response.json()["checks"]["storage_backends"] == {
        "ok": True,
        "reason": "StorageBackendMetadataReady",
        "count": 1,
        "backendIds": [str(_storage_backend().id)],
    }
    assert response.json()["checks"]["object_store"] == {
        "ok": True,
        "reason": "ObjectStoreReachable",
        "count": 1,
    }


def test_readyz_reports_database_unavailable_without_failing_health() -> None:
    client = _client(
        _Repository([_storage_backend()], error=psycopg.OperationalError())
    )

    health = client.get("/healthz")
    response = client.get("/readyz")

    assert health.status_code == 200
    assert response.status_code == 503
    assert response.json()["checks"]["repository"] == {
        "ok": False,
        "reason": "DatabaseUnavailable",
        "error": "OperationalError",
    }


def test_readyz_reports_missing_storage_backend_metadata() -> None:
    repository = _Repository([])
    client = _client(repository)

    response = client.get("/readyz")

    assert response.status_code == 503
    assert response.json()["checks"]["storage_backends"] == {
        "ok": False,
        "reason": "StorageBackendMetadataMissing",
        "count": 0,
    }
    assert response.json()["checks"]["object_store"] == {
        "ok": False,
        "reason": "ObjectStoreReachabilitySkipped",
        "count": 0,
    }
    assert repository.write_calls == 0


def test_readyz_reports_object_store_reachability_failure() -> None:
    backend = _storage_backend()
    client = _client(
        _Repository([backend]),
        object_storage=InMemoryObjectStorage(
            check_backend_error=RuntimeError("bucket unavailable")
        ),
    )

    response = client.get("/readyz")

    assert response.status_code == 503
    assert response.json()["checks"]["storage_backends"]["reason"] == (
        "StorageBackendMetadataReady"
    )
    assert response.json()["checks"]["object_store"] == {
        "ok": False,
        "reason": "ObjectStoreUnreachable",
        "error": "RuntimeError",
    }


def test_readyz_reports_schema_revision_mismatch() -> None:
    client = _client(_Repository([_storage_backend()], schema_revision="old"))

    response = client.get("/readyz")

    assert response.status_code == 503
    assert response.json()["checks"]["repository"] == {
        "ok": False,
        "reason": "SchemaRevisionMismatch",
        "observed": "old",
        "expected": CURRENT_SCHEMA_REVISION,
    }


def test_readyz_reports_multiple_schema_heads() -> None:
    client = _client(
        _Repository(
            [_storage_backend()],
            error=MultipleAlembicHeads("multiple Alembic heads"),
        )
    )

    response = client.get("/readyz")

    assert response.status_code == 503
    assert response.json()["checks"]["repository"] == {
        "ok": False,
        "reason": "SchemaRevisionMultipleHeads",
        "error": "MultipleAlembicHeads",
    }


def test_readyz_reports_unsupported_schema_revision() -> None:
    client = _client(
        _Repository(
            [_storage_backend()],
            error=UnsupportedSchemaRevision("unsupported schema revision"),
        )
    )

    response = client.get("/readyz")

    assert response.status_code == 503
    assert response.json()["checks"]["repository"] == {
        "ok": False,
        "reason": "SchemaRevisionUnsupported",
        "error": "UnsupportedSchemaRevision",
    }


def test_unexpected_api_error_returns_stable_payload_with_incident_id(
    caplog: pytest.LogCaptureFixture,
) -> None:
    app = _app(_Repository([_storage_backend()]))

    @app.get("/explode")
    def explode() -> None:
        raise RuntimeError("private backend detail")

    client = TestClient(app, raise_server_exceptions=False)

    with caplog.at_level(logging.ERROR, logger="tamoss.errors"):
        response = client.get("/explode")

    body = response.json()
    incident_id = body["incident_id"]
    assert response.status_code == 500
    assert body["type"] == "internal_server_error"
    assert body["summary"] == "Internal server error"
    assert incident_id
    assert "private backend detail" not in response.text
    matching_records = [
        record
        for record in caplog.records
        if getattr(record, "incident_id", None) == incident_id
    ]
    assert len(matching_records) == 1
    assert matching_records[0].exc_info is not None
    assert "private backend detail" in caplog.text


def test_create_app_fails_fast_for_invalid_configuration() -> None:
    with pytest.raises(StartupConfigurationError, match="S3 storage backend"):
        create_app(
            Settings(
                auth_required=False,
                storage_backend=None,
            )
        )


def _client(
    repository: _Repository, *, object_storage: InMemoryObjectStorage | None = None
) -> TestClient:
    return TestClient(_app(repository, object_storage=object_storage))


def _app(
    repository: _Repository, *, object_storage: InMemoryObjectStorage | None = None
) -> FastAPI:
    settings = _settings()
    return create_app(
        settings,
        use_cases=TamossUseCases(
            repository=repository,
            object_storage=object_storage or InMemoryObjectStorage(),
            settings=settings,
        ),
    )


def _settings() -> Settings:
    return Settings(
        auth_required=False,
        storage_backend=StorageBackendSettings(
            id="11111111-1111-4111-8111-111111111111",
            label="tamoss.storage.primary",
            provider="tamoss",
            region="us-east-1",
            store_product="s3",
            default_storage=True,
            bucket_name="tamoss-primary",
            endpoint_url="https://objects.internal.example.test",
            public_endpoint_url="https://objects.example.test",
            access_key="access",
            secret_key="secret",
        ),
    )


def _storage_backend() -> StorageBackend:
    return StorageBackend(
        id=UUID("11111111-1111-4111-8111-111111111111"),
        label="tamoss.storage.primary",
        provider="tamoss",
        region="us-east-1",
        store_product="s3",
        default_storage=True,
        bucket_name="tamoss-primary",
        endpoint_url="https://objects.internal.example.test",
        public_endpoint_url="https://objects.example.test",
        access_key="access",
        secret_key="secret",
    )


class _Repository:
    def __init__(
        self,
        storage_backends: list[StorageBackend],
        *,
        error: Exception | None = None,
        schema_revision: str = CURRENT_SCHEMA_REVISION,
    ) -> None:
        self._storage_backends = storage_backends
        self._error = error
        self._schema_revision = schema_revision
        self.write_calls = 0

    @property
    def service_repository(self) -> _Repository:
        return self

    @property
    def webhook_repository(self) -> _Repository:
        return self

    @property
    def deletion_repository(self) -> _Repository:
        return self

    @property
    def source_repository(self) -> _Repository:
        return self

    @property
    def flow_repository(self) -> _Repository:
        return self

    @property
    def storage_repository(self) -> _Repository:
        return self

    @property
    def segment_repository(self) -> _Repository:
        return self

    @property
    def object_repository(self) -> _Repository:
        return self

    def get_service_metadata(self) -> None:
        if self._error is not None:
            raise self._error

    def current_schema_revision(self) -> str:
        if self._error is not None:
            raise self._error
        return self._schema_revision

    def list_storage_backends(self) -> list[StorageBackend]:
        if self._error is not None:
            raise self._error
        return self._storage_backends

    def save_service_metadata(self, *_args: object) -> None:
        self.write_calls += 1
