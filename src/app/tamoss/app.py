from __future__ import annotations

import logging
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from types import TracebackType
from typing import Any

import anyio
from fastapi import FastAPI, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse
from jwt import PyJWKClientError
from starlette.responses import Response

from tamoss.api.routes import (
    delete_requests,
    flows,
    health,
    objects,
    segments,
    service,
    sources,
    storage,
    webhooks,
)
from tamoss.application.use_cases import TamossUseCases
from tamoss.auth import (
    authenticate_request,
    authorize_request,
    unauthorized_headers,
    warm_oauth2_jwks,
)
from tamoss.bootstrap import StartupConfigurationError, create_use_cases
from tamoss.contract.openapi import load_public_openapi
from tamoss.errors import (
    TamossError,
    Unauthorized,
    error_payload,
    register_error_handlers,
)
from tamoss.metrics import install_http_metrics, record_api_info, start_metrics_server
from tamoss.settings import Settings

logger = logging.getLogger(__name__)

CORS_ALLOW_METHODS = ["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"]
CORS_ALLOW_HEADERS = ["Authorization", "Content-Type", "Accept", "Origin"]
CORS_EXPOSE_HEADERS = [
    "Link",
    "X-Paging-Count",
    "X-Paging-Limit",
    "X-Paging-NextKey",
]


def create_app(
    settings: Settings | None = None,
    *,
    use_cases: TamossUseCases | None = None,
) -> FastAPI:
    use_cases = use_cases or create_use_cases(settings)
    resolved_settings = use_cases.settings

    @asynccontextmanager
    async def lifespan(application: FastAPI) -> AsyncIterator[None]:
        # Sync handlers and threaded auth both consume this limiter; the
        # anyio default of 40 tokens otherwise caps request concurrency
        # below the configured database pool headroom.
        anyio.to_thread.current_default_thread_limiter().total_tokens = (
            resolved_settings.api_thread_pool_tokens
        )
        try:
            await _warm_runtime_auth(resolved_settings)
            application.state.tamoss_metrics_server = start_metrics_server(
                resolved_settings
            )
            yield
        finally:
            _close_metrics_server(application)
            _close_repository(application)

    application = FastAPI(
        title="Time Addressable Media Open Source Store",
        description="TAMOSS API for time-addressable media workflows.",
        version=resolved_settings.tamoss_version,
        lifespan=lifespan,
    )
    application.state.tamoss_use_cases = use_cases
    application.state.tamoss_settings = resolved_settings

    register_error_handlers(application)
    _install_runtime_auth(application, resolved_settings)
    install_http_metrics(application)
    _install_cors(application, resolved_settings)
    record_api_info(resolved_settings)
    application.include_router(health.router)
    application.include_router(service.router)
    application.include_router(webhooks.router)
    application.include_router(delete_requests.router)
    application.include_router(sources.router)
    application.include_router(flows.router)
    application.include_router(storage.router)
    application.include_router(segments.router)
    application.include_router(objects.router)
    _install_openapi_schema(application, resolved_settings)
    return application


def _install_cors(application: FastAPI, settings: Settings) -> None:
    if not settings.cors_allowed_origins and not settings.cors_allowed_origin_regexes:
        return
    application.add_middleware(
        CORSMiddleware,
        allow_origins=settings.cors_allowed_origins,
        allow_origin_regex=_combined_cors_origin_regex(
            settings.cors_allowed_origin_regexes
        ),
        allow_methods=CORS_ALLOW_METHODS,
        allow_headers=CORS_ALLOW_HEADERS,
        expose_headers=CORS_EXPOSE_HEADERS,
    )


def _combined_cors_origin_regex(regexes: list[str]) -> str | None:
    if not regexes:
        return None
    if len(regexes) == 1:
        return regexes[0]
    return "|".join(f"(?:{regex})" for regex in regexes)


def _close_repository(application: FastAPI) -> None:
    use_cases = getattr(application.state, "tamoss_use_cases", None)
    repository = getattr(use_cases, "repository", None)
    close = getattr(repository, "close", None)
    if callable(close):
        close()


def _close_metrics_server(application: FastAPI) -> None:
    metrics_server = getattr(application.state, "tamoss_metrics_server", None)
    close = getattr(metrics_server, "close", None)
    if callable(close):
        close()
    application.state.tamoss_metrics_server = None


def _install_runtime_auth(application: FastAPI, settings: Settings) -> None:
    @application.middleware("http")
    async def tamoss_auth_middleware(
        request: Request,
        call_next: Callable[[Request], Awaitable[Response]],
    ) -> Response:
        if _auth_is_skipped(request):
            return await call_next(request)
        try:
            if _request_carries_credentials(request, settings):
                identity = await anyio.to_thread.run_sync(
                    authenticate_request,
                    request,
                    settings,
                )
            else:
                # Without credentials authentication is a pure in-memory
                # decision (anonymous or 401), so the thread-pool dispatch
                # would only burn a limiter token.
                identity = authenticate_request(request, settings)
            authorize_request(request, identity, settings)
        except TamossError as exc:
            headers = (
                unauthorized_headers(settings)
                if isinstance(exc, Unauthorized)
                else None
            )
            return JSONResponse(
                status_code=exc.status_code,
                content=error_payload(exc.error_type, exc.detail),
                headers=headers,
            )
        return await call_next(request)


async def _warm_runtime_auth(settings: Settings) -> None:
    try:
        await anyio.to_thread.run_sync(warm_oauth2_jwks, settings)
    except (OSError, PyJWKClientError, TimeoutError, ValueError):
        logger.warning("OAuth2 JWKS warmup failed", exc_info=True)


def _auth_is_skipped(request: Request) -> bool:
    return request.method == "OPTIONS" or request.url.path in {
        "/healthz",
        "/readyz",
    }


def _request_carries_credentials(request: Request, settings: Settings) -> bool:
    if settings.trust_forward_auth_headers:
        return True
    return bool(
        request.headers.get("authorization") or request.query_params.get("access_token")
    )


def _install_openapi_schema(application: FastAPI, settings: Settings) -> None:
    def custom_openapi() -> dict[str, Any]:
        if application.openapi_schema:
            return application.openapi_schema
        application.openapi_schema = load_public_openapi(settings)
        return application.openapi_schema

    setattr(application, "openapi", custom_openapi)  # noqa: B010


class _FailedStartupLifespan:
    def __init__(self, startup_error: StartupConfigurationError) -> None:
        self._startup_error = startup_error

    async def __aenter__(self) -> None:
        logger.error("TAMOSS startup configuration is invalid: %s", self._startup_error)
        raise self._startup_error

    async def __aexit__(
        self,
        _exc_type: type[BaseException] | None,
        _exc: BaseException | None,
        _traceback: TracebackType | None,
    ) -> bool:
        return False


def _default_app() -> FastAPI:
    try:
        return create_app()
    except StartupConfigurationError as exc:
        startup_error = exc

        def failed_lifespan(_application: FastAPI) -> _FailedStartupLifespan:
            return _FailedStartupLifespan(startup_error)

        return FastAPI(lifespan=failed_lifespan)


app = _default_app()
