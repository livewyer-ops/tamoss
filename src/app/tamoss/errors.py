from __future__ import annotations

import logging
from datetime import UTC, datetime
from http import HTTPStatus
from typing import Any
from uuid import uuid4

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from starlette.exceptions import HTTPException as StarletteHTTPException

logger = logging.getLogger(__name__)


class TamossError(Exception):
    status_code: int = 500
    error_type: str = "internal_server_error"

    def __init__(self, detail: str) -> None:
        self.detail = detail
        super().__init__(detail)


class BadRequest(TamossError):
    status_code = 400
    error_type = "bad_request"


class Unauthorized(TamossError):
    status_code = 401
    error_type = "unauthorized"


class Forbidden(TamossError):
    status_code = 403
    error_type = "forbidden"


class NotFound(TamossError):
    status_code = 404
    error_type = "not_found"


class ConfigurationError(TamossError):
    status_code = 503
    error_type = "configuration_error"


ErrorPayloadDict = dict[str, Any]


def error_payload(
    error_type: str, summary: str, *, incident_id: str | None = None
) -> ErrorPayloadDict:
    payload: ErrorPayloadDict = {
        "type": error_type,
        "summary": summary,
        "time": datetime.now(UTC).isoformat(),
    }
    if incident_id is not None:
        payload["incident_id"] = incident_id
    return payload


def normalize_error_payload(value: object) -> ErrorPayloadDict | None:
    # Local import avoids a circular dependency between errors.py and domain.model.
    from tamoss.domain.model import DomainErrorPayload  # noqa: PLC0415

    payload = DomainErrorPayload.from_json_dict(value)
    return payload.to_json_dict() if payload is not None else None


def register_error_handlers(app: FastAPI) -> None:
    @app.exception_handler(TamossError)
    async def tamoss_error_handler(_request: Request, exc: TamossError) -> JSONResponse:
        return JSONResponse(
            status_code=exc.status_code,
            content=error_payload(exc.error_type, exc.detail),
        )

    @app.exception_handler(RequestValidationError)
    async def validation_error_handler(
        _request: Request, exc: RequestValidationError
    ) -> JSONResponse:
        messages = "; ".join(
            error.get("msg", "Invalid request") for error in exc.errors()
        )
        return JSONResponse(
            status_code=400,
            content=error_payload("bad_request", f"Invalid request: {messages}"),
        )

    @app.exception_handler(StarletteHTTPException)
    async def http_error_handler(
        _request: Request, exc: StarletteHTTPException
    ) -> JSONResponse:
        try:
            status = HTTPStatus(exc.status_code)
            error_type = status.phrase.lower().replace(" ", "_")
        except ValueError:
            error_type = "http_error"
        return JSONResponse(
            status_code=exc.status_code,
            content=error_payload(error_type, str(exc.detail)),
            headers=getattr(exc, "headers", None),
        )

    @app.exception_handler(Exception)
    async def unexpected_error_handler(
        request: Request, _exc: Exception
    ) -> JSONResponse:
        incident_id = str(uuid4())
        logger.exception(
            "unhandled API exception",
            extra={
                "incident_id": incident_id,
                "method": request.method,
                "path": request.url.path,
            },
        )
        return JSONResponse(
            status_code=500,
            content=error_payload(
                "internal_server_error",
                "Internal server error",
                incident_id=incident_id,
            ),
        )
