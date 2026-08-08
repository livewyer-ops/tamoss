from __future__ import annotations

import logging
import time
from collections.abc import Callable
from dataclasses import dataclass
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from threading import Lock, Thread
from urllib.parse import urlsplit

from prometheus_client import CONTENT_TYPE_LATEST, generate_latest

from tamoss.settings import Settings

logger = logging.getLogger(__name__)


class WorkerHealthState:
    def __init__(
        self,
        stale_after_seconds: float,
        *,
        clock: Callable[[], float] = time.monotonic,
    ) -> None:
        if stale_after_seconds <= 0:
            raise ValueError("stale_after_seconds must be positive")
        self._stale_after_seconds = stale_after_seconds
        self._clock = clock
        self._lock = Lock()
        self._last_progress = clock()
        self._ready = False
        self._running = True

    def mark_poll_started(self) -> None:
        self._mark_progress()

    def mark_poll_succeeded(self) -> None:
        with self._lock:
            self._last_progress = self._clock()
            self._ready = True

    def mark_poll_failed(self) -> None:
        with self._lock:
            self._last_progress = self._clock()
            self._ready = False

    def mark_stopping(self) -> None:
        with self._lock:
            self._running = False
            self._ready = False

    def is_live(self) -> bool:
        running, _, age = self._snapshot()
        return running and age <= self._stale_after_seconds

    def is_ready(self) -> bool:
        running, ready, age = self._snapshot()
        return running and ready and age <= self._stale_after_seconds

    def _mark_progress(self) -> None:
        with self._lock:
            self._last_progress = self._clock()

    def _snapshot(self) -> tuple[bool, bool, float]:
        with self._lock:
            return (
                self._running,
                self._ready,
                max(0.0, self._clock() - self._last_progress),
            )


@dataclass
class WorkerHTTPServer:
    server: ThreadingHTTPServer
    thread: Thread

    @property
    def port(self) -> int:
        return self.server.server_port

    def close(self) -> None:
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)


def start_worker_http_server(
    settings: Settings,
    health: WorkerHealthState,
) -> WorkerHTTPServer | None:
    if settings.metrics_port is None:
        return None
    try:
        server = ThreadingHTTPServer(
            (settings.metrics_bind_address, settings.metrics_port),
            _worker_handler(health),
        )
    except OSError:
        logger.warning(
            "worker HTTP endpoint disabled: cannot bind %s:%s",
            settings.metrics_bind_address,
            settings.metrics_port,
            exc_info=True,
        )
        return None
    server.daemon_threads = True
    thread = Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return WorkerHTTPServer(server=server, thread=thread)


def _worker_handler(
    health: WorkerHealthState,
) -> type[BaseHTTPRequestHandler]:
    class WorkerRequestHandler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            self._serve(include_body=True)

        def do_HEAD(self) -> None:
            self._serve(include_body=False)

        def log_message(self, _format: str, *_args: object) -> None:
            return

        def _serve(self, *, include_body: bool) -> None:
            path = urlsplit(self.path).path
            if path == "/metrics":
                self._respond(
                    HTTPStatus.OK,
                    generate_latest(),
                    CONTENT_TYPE_LATEST,
                    include_body=include_body,
                )
                return
            if path == "/healthz":
                live = health.is_live()
                status = HTTPStatus.OK if live else HTTPStatus.SERVICE_UNAVAILABLE
                body = b"ok\n" if live else b"stale\n"
                self._respond(status, body, include_body=include_body)
                return
            if path == "/readyz":
                ready = health.is_ready()
                status = HTTPStatus.OK if ready else HTTPStatus.SERVICE_UNAVAILABLE
                body = b"ok\n" if ready else b"not ready\n"
                self._respond(status, body, include_body=include_body)
                return
            self._respond(
                HTTPStatus.NOT_FOUND, b"not found\n", include_body=include_body
            )

        def _respond(
            self,
            status: HTTPStatus,
            body: bytes,
            content_type: str = "text/plain; charset=utf-8",
            *,
            include_body: bool,
        ) -> None:
            self.send_response(status)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            if include_body:
                self.wfile.write(body)

    return WorkerRequestHandler
