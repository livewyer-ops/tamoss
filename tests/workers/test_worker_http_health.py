from __future__ import annotations

import urllib.error
import urllib.request

import pytest
from tamoss.settings import Settings
from tamoss.worker_health import WorkerHealthState, start_worker_http_server

pytestmark = pytest.mark.worker


def test_worker_http_server_tracks_readiness_without_backend_calls() -> None:
    state = WorkerHealthState(600)
    server = start_worker_http_server(_settings(), state)
    assert server is not None
    try:
        assert _status(server.port, "/healthz") == 200
        assert _status(server.port, "/readyz") == 503

        state.mark_poll_succeeded()
        assert _status(server.port, "/readyz") == 200

        state.mark_poll_failed()
        assert _status(server.port, "/healthz") == 200
        assert _status(server.port, "/readyz") == 503

        status, body = _response(server.port, "/metrics")
        assert status == 200
        assert b"python_info" in body
    finally:
        server.close()


def test_worker_health_becomes_unhealthy_when_progress_is_stale() -> None:
    now = [100.0]
    state = WorkerHealthState(30, clock=lambda: now[0])
    state.mark_poll_succeeded()

    assert state.is_live() is True
    assert state.is_ready() is True

    now[0] += 31

    assert state.is_live() is False
    assert state.is_ready() is False


def test_worker_health_stops_readiness_during_shutdown() -> None:
    state = WorkerHealthState(600)
    state.mark_poll_succeeded()

    state.mark_stopping()

    assert state.is_live() is False
    assert state.is_ready() is False


def _settings() -> Settings:
    return Settings(
        auth_required=False,
        storage_backend=None,
        metrics_bind_address="127.0.0.1",
        metrics_port=0,
    )


def _status(port: int, path: str) -> int:
    return _response(port, path)[0]


def _response(port: int, path: str) -> tuple[int, bytes]:
    try:
        with urllib.request.urlopen(
            f"http://127.0.0.1:{port}{path}",
            timeout=5,
        ) as response:
            return response.status, response.read()
    except urllib.error.HTTPError as exc:
        return exc.code, exc.read()
