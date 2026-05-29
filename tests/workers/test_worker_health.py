from __future__ import annotations

import json
from uuid import uuid4

import pytest
from tamoss import worker
from tamoss.settings import Settings, StorageBackendSettings

pytestmark = pytest.mark.worker


class _Repository:
    def __init__(self, error: Exception | None = None) -> None:
        self.error = error
        self.checked = False
        self.closed = False

    def check_connection(self) -> None:
        self.checked = True
        if self.error is not None:
            raise self.error

    def close(self) -> None:
        self.closed = True


class _UseCases:
    def __init__(self, repository: _Repository) -> None:
        self.repository = repository


def test_worker_health_succeeds_without_processing_queues(
    tmp_path, monkeypatch: pytest.MonkeyPatch
) -> None:
    credentials_file = tmp_path / "credentials.json"
    credentials_file.write_text(
        json.dumps(
            {
                "credentials": [
                    {
                        "storageBackendId": str(uuid4()),
                        "accessKey": "access",
                        "secretKey": "secret",
                    }
                ]
            }
        ),
        encoding="utf-8",
    )
    repository = _Repository()
    monkeypatch.setattr(
        worker, "create_use_cases", lambda _settings: _UseCases(repository)
    )

    worker.check_health(_settings(str(credentials_file)))

    assert repository.checked is True
    assert repository.closed is True


def test_worker_health_reports_database_failure(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    repository = _Repository(RuntimeError("database unavailable"))
    monkeypatch.setattr(
        worker, "create_use_cases", lambda _settings: _UseCases(repository)
    )

    with pytest.raises(worker.WorkerHealthError, match="database connectivity"):
        worker.check_health(_settings())

    assert repository.checked is True
    assert repository.closed is True


def test_worker_health_reports_credentials_failure(tmp_path) -> None:
    credentials_file = tmp_path / "credentials.json"
    credentials_file.write_text("not-json", encoding="utf-8")

    with pytest.raises(
        worker.WorkerHealthError, match="storage backend credentials file"
    ):
        worker.check_health(_settings(str(credentials_file)))


def _settings(credentials_file: str | None = None) -> Settings:
    return Settings(
        auth_required=False,
        storage_backend_credentials_file=credentials_file,
        storage_backend=StorageBackendSettings(
            id=uuid4(),
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
        ),
    )
