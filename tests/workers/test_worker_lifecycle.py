from __future__ import annotations

import os
import subprocess
import sys
from typing import Any

import pytest
from tamoss import metrics, worker

pytestmark = pytest.mark.worker


def _tasks_processed(queue: str) -> float:
    value = metrics.WORKER_TASKS_PROCESSED_TOTAL.labels(queue=queue)._value.get()
    return float(value)


def test_worker_import_does_not_create_application_with_unavailable_postgres() -> None:
    env = os.environ.copy()
    env["POSTGRES_HOST"] = "127.0.0.1"
    env["POSTGRES_PORT"] = "1"
    env["POSTGRES_DB"] = "tamoss"
    env["POSTGRES_USER"] = "tamoss"
    env["POSTGRES_PASSWORD"] = "tamoss"
    env["PYTHONPATH"] = os.pathsep.join(path for path in sys.path if path)

    result = subprocess.run(
        [sys.executable, "-c", "import tamoss.worker; print('imported')"],
        env=env,
        text=True,
        capture_output=True,
        check=False,
    )

    assert result.returncode == 0, result.stderr
    assert result.stdout.strip() == "imported"


def test_worker_main_retries_after_poll_failure(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    slept: list[int] = []
    closed = False

    def fail_once(*_: Any, **__: Any) -> tuple[int, int]:
        worker._shutdown = True
        raise RuntimeError("temporary backend startup failure")

    class Repository:
        def close(self) -> None:
            nonlocal closed
            closed = True

    class UseCases:
        repository = Repository()

    use_cases = UseCases()
    monkeypatch.setenv("TAMOSS_WORKER_POLL_INTERVAL_SECONDS", "1")
    monkeypatch.setenv("TAMOSS_WORKER_ENABLE_DELETE", "1")
    monkeypatch.setenv("TAMOSS_WORKER_ENABLE_WEBHOOK", "0")
    worker.get_settings.cache_clear()
    monkeypatch.setattr(worker, "create_use_cases", lambda _settings=None: use_cases)
    monkeypatch.setattr(worker, "drain_once", fail_once)
    monkeypatch.setattr(worker.time, "sleep", slept.append)
    monkeypatch.setattr(worker.signal, "signal", lambda *_: None)
    worker._shutdown = False

    try:
        worker.main()
    finally:
        worker._shutdown = False
        worker.get_settings.cache_clear()

    assert slept == [1]
    assert closed is True


def test_worker_main_records_tasks_processed_metric(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    def drain_then_stop(*_: Any, **__: Any) -> tuple[int, int]:
        worker._shutdown = True
        return 3, 2

    class Repository:
        def close(self) -> None:
            return None

    class UseCases:
        repository = Repository()

    use_cases = UseCases()
    monkeypatch.setenv("TAMOSS_WORKER_POLL_INTERVAL_SECONDS", "1")
    monkeypatch.setenv("TAMOSS_WORKER_ENABLE_DELETE", "1")
    monkeypatch.setenv("TAMOSS_WORKER_ENABLE_WEBHOOK", "1")
    worker.get_settings.cache_clear()
    monkeypatch.setattr(worker, "create_use_cases", lambda _settings=None: use_cases)
    monkeypatch.setattr(worker, "drain_once", drain_then_stop)
    monkeypatch.setattr(worker, "purge_finished_queue_records", lambda *_, **__: 0)
    monkeypatch.setattr(worker.time, "sleep", lambda _seconds: None)
    monkeypatch.setattr(worker.signal, "signal", lambda *_: None)
    worker._shutdown = False

    before_delete = _tasks_processed("delete")
    before_webhook = _tasks_processed("webhook")
    try:
        worker.main()
    finally:
        worker._shutdown = False
        worker.get_settings.cache_clear()

    assert _tasks_processed("delete") - before_delete == 3
    assert _tasks_processed("webhook") - before_webhook == 2
