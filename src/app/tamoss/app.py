from __future__ import annotations

import logging
from contextlib import asynccontextmanager

import anyio
from fastapi import FastAPI, Request
from fastapi.openapi.utils import get_openapi
from fastapi.responses import JSONResponse

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
from tamoss.auth import authenticate_request, unauthorized_headers, warm_oauth2_jwks
from tamoss.bootstrap import create_use_cases
from tamoss.errors import Unauthorized, error_payload, register_error_handlers
from tamoss.openapi_extensions import apply_tamoss_extensions
from tamoss.settings import Settings

logger = logging.getLogger(__name__)


def create_app(
    settings: Settings | None = None,
    *,
    use_cases: TamossUseCases | None = None,
) -> FastAPI:
    use_cases = use_cases or create_use_cases(settings)
    resolved_settings = use_cases.settings

    @asynccontextmanager
    async def lifespan(application: FastAPI):
        try:
            await _warm_runtime_auth(resolved_settings)
            yield
        finally:
            _close_repository(application)

    application = FastAPI(
        title="Time Addressable Media Open Source Store",
        description="TAMOSS API for time-addressable media workflows.",
        version=resolved_settings.api_version,
        lifespan=lifespan,
    )
    application.state.tamoss_use_cases = use_cases
    application.state.tamoss_settings = resolved_settings

    register_error_handlers(application)
    _install_runtime_auth(application, resolved_settings)
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


def _close_repository(application: FastAPI) -> None:
    use_cases = getattr(application.state, "tamoss_use_cases", None)
    repository = getattr(use_cases, "repository", None)
    close = getattr(repository, "close", None)
    if callable(close):
        close()


def _install_runtime_auth(application: FastAPI, settings: Settings) -> None:
    @application.middleware("http")
    async def tamoss_auth_middleware(request: Request, call_next):
        if _auth_is_skipped(request):
            return await call_next(request)
        try:
            await anyio.to_thread.run_sync(authenticate_request, request, settings)
        except Unauthorized as exc:
            return JSONResponse(
                status_code=exc.status_code,
                content=error_payload(exc.error_type, exc.detail),
                headers=unauthorized_headers(),
            )
        return await call_next(request)


async def _warm_runtime_auth(settings: Settings) -> None:
    try:
        await anyio.to_thread.run_sync(warm_oauth2_jwks, settings)
    except Exception:
        logger.warning("OAuth2 JWKS warmup failed", exc_info=True)


def _auth_is_skipped(request: Request) -> bool:
    return request.method == "OPTIONS" or request.url.path in {
        "/healthz",
        "/readyz",
    }


def _install_openapi_schema(application: FastAPI, settings: Settings) -> None:
    def custom_openapi() -> dict:
        if application.openapi_schema:
            return application.openapi_schema
        schema = get_openapi(
            title=application.title,
            version=settings.api_version,
            description=application.description,
            routes=application.routes,
        )
        _remove_generated_validation_responses(schema)
        _align_bbc_delete_request_path(schema)
        _remove_head_response_bodies(schema)
        apply_tamoss_extensions(schema)
        application.openapi_schema = schema
        return application.openapi_schema

    setattr(application, "openapi", custom_openapi)


def _remove_generated_validation_responses(schema: dict) -> None:
    for path_item in schema.get("paths", {}).values():
        for operation in path_item.values():
            if isinstance(operation, dict):
                operation.get("responses", {}).pop("422", None)


def _align_bbc_delete_request_path(schema: dict) -> None:
    runtime_path = "/flow-delete-requests/{request_id}"
    bbc_path = "/flow-delete-requests/{request-id}"
    paths = schema.get("paths", {})
    if runtime_path not in paths:
        return
    paths[bbc_path] = paths.pop(runtime_path)
    for operation in paths[bbc_path].values():
        if not isinstance(operation, dict):
            continue
        for parameter in operation.get("parameters", []):
            if parameter.get("in") == "path" and parameter.get("name") == "request_id":
                parameter["name"] = "request-id"


def _remove_head_response_bodies(schema: dict) -> None:
    for path_item in schema.get("paths", {}).values():
        if not isinstance(path_item, dict):
            continue
        head_operation = path_item.get("head")
        if _is_paged_head_operation(head_operation):
            continue
        if isinstance(head_operation, dict):
            for response in head_operation.get("responses", {}).values():
                if isinstance(response, dict):
                    response.pop("content", None)


def _is_paged_head_operation(operation: object) -> bool:
    if not isinstance(operation, dict):
        return False
    return any(
        parameter.get("in") == "query" and parameter.get("name") in {"page", "limit"}
        for parameter in operation.get("parameters", [])
        if isinstance(parameter, dict)
    )


def _default_app() -> FastAPI:
    try:
        return create_app()
    except RuntimeError as exc:
        startup_error = exc

        @asynccontextmanager
        async def failed_lifespan(_application: FastAPI):
            logger.error("TAMOSS startup configuration is invalid: %s", startup_error)
            raise startup_error
            yield

        return FastAPI(lifespan=failed_lifespan)


app = _default_app()
