from __future__ import annotations

import time
from collections.abc import Awaitable, Callable
from dataclasses import dataclass
from threading import Thread
from wsgiref.simple_server import WSGIServer

from fastapi import FastAPI, Request
from prometheus_client import Counter, Gauge, Histogram, start_http_server
from starlette.responses import Response

from tamoss.db.migrations import CURRENT_SCHEMA_REVISION
from tamoss.settings import Settings

_UNMATCHED_ROUTE = "unmatched"
_UNKNOWN_EXCEPTION = "UnknownException"

HTTP_REQUESTS_TOTAL = Counter(
    "tamoss_api_http_requests_total",
    "TAMOSS API HTTP requests completed.",
    ("method", "route", "status"),
)
HTTP_REQUEST_DURATION_SECONDS = Histogram(
    "tamoss_api_http_request_duration_seconds",
    "TAMOSS API HTTP request duration.",
    ("method", "route", "status"),
    buckets=(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0),
)
HTTP_REQUESTS_IN_PROGRESS = Gauge(
    "tamoss_api_http_requests_in_progress",
    "TAMOSS API HTTP requests currently being handled.",
    ("method",),
)
HTTP_EXCEPTIONS_TOTAL = Counter(
    "tamoss_api_http_exceptions_total",
    "TAMOSS API HTTP requests that raised an exception before a response was produced.",
    ("method", "route", "exception"),
)
API_INFO = Gauge(
    "tamoss_api_info",
    "TAMOSS API build and compatibility information.",
    ("version", "tams_api_version", "schema_revision"),
)


@dataclass
class MetricsServer:
    server: WSGIServer
    thread: Thread

    @property
    def port(self) -> int:
        return self.server.server_port

    def close(self) -> None:
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)


def install_http_metrics(application: FastAPI) -> None:
    @application.middleware("http")
    async def tamoss_metrics_middleware(
        request: Request,
        call_next: Callable[[Request], Awaitable[Response]],
    ) -> Response:
        method = request.method
        start = time.perf_counter()
        HTTP_REQUESTS_IN_PROGRESS.labels(method=method).inc()
        try:
            response = await call_next(request)
        except Exception as exc:
            route = _route_template(request)
            duration = time.perf_counter() - start
            record_http_exception(request, exc)
            HTTP_REQUESTS_TOTAL.labels(
                method=method,
                route=route,
                status="500",
            ).inc()
            HTTP_REQUEST_DURATION_SECONDS.labels(
                method=method,
                route=route,
                status="500",
            ).observe(duration)
            raise
        finally:
            HTTP_REQUESTS_IN_PROGRESS.labels(method=method).dec()

        route = _route_template(request)
        status = str(response.status_code)
        duration = time.perf_counter() - start
        HTTP_REQUESTS_TOTAL.labels(method=method, route=route, status=status).inc()
        HTTP_REQUEST_DURATION_SECONDS.labels(
            method=method,
            route=route,
            status=status,
        ).observe(duration)
        return response


def start_metrics_server(settings: Settings) -> MetricsServer | None:
    if settings.metrics_port is None:
        return None
    server, thread = start_http_server(
        settings.metrics_port,
        addr=settings.metrics_bind_address,
    )
    return MetricsServer(server=server, thread=thread)


def record_http_exception(request: Request, exc: Exception) -> None:
    HTTP_EXCEPTIONS_TOTAL.labels(
        method=request.method,
        route=_route_template(request),
        exception=type(exc).__name__ or _UNKNOWN_EXCEPTION,
    ).inc()


def record_api_info(settings: Settings) -> None:
    API_INFO.labels(
        version=settings.service_version,
        tams_api_version=settings.api_version,
        schema_revision=CURRENT_SCHEMA_REVISION,
    ).set(1)


def _route_template(request: Request) -> str:
    route = request.scope.get("route")
    path = getattr(route, "path", None)
    if isinstance(path, str) and path:
        return path
    return _UNMATCHED_ROUTE
