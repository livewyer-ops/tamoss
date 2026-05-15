from __future__ import annotations

from collections.abc import Mapping
from datetime import datetime, timezone
from http import HTTPStatus
from typing import Any

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from starlette.exceptions import HTTPException as StarletteHTTPException


class TamossError(Exception):
    status_code = 500
    error_type = "internal_server_error"

    def __init__(self, detail: str):
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


class Conflict(TamossError):
    status_code = 409
    error_type = "conflict"


ErrorPayloadDict = dict[str, Any]


def error_payload(error_type: str, summary: str) -> ErrorPayloadDict:
    return {
        "type": error_type,
        "summary": summary,
        "time": datetime.now(timezone.utc).isoformat(),
    }


def normalize_error_payload(
    value: object, *, default_type: str = "TAMSError"
) -> ErrorPayloadDict | None:
    if value is None:
        return None
    if not isinstance(value, Mapping):
        return error_payload(default_type, str(value))

    summary = (
        value.get("summary")
        or value.get("message")
        or value.get("detail")
        or str(dict(value))
    )
    normalized: ErrorPayloadDict = {
        "type": str(value.get("type") or default_type),
        "summary": str(summary),
        "time": _normalize_error_time(value.get("time")),
    }
    traceback = value.get("traceback")
    if isinstance(traceback, list):
        normalized["traceback"] = [str(line) for line in traceback]
    elif traceback:
        normalized["traceback"] = [str(traceback)]
    return normalized


def _normalize_error_time(value: object) -> str:
    if isinstance(value, datetime):
        timestamp = value
    elif isinstance(value, str) and value:
        try:
            timestamp = datetime.fromisoformat(value.replace("Z", "+00:00"))
        except ValueError:
            timestamp = datetime.now(timezone.utc)
    else:
        timestamp = datetime.now(timezone.utc)
    if timestamp.tzinfo is None:
        timestamp = timestamp.replace(tzinfo=timezone.utc)
    return timestamp.isoformat()


def register_error_handlers(app: FastAPI) -> None:
    @app.exception_handler(TamossError)
    async def tamoss_error_handler(request: Request, exc: TamossError) -> JSONResponse:
        return JSONResponse(
            status_code=exc.status_code,
            content=error_payload(exc.error_type, exc.detail),
        )

    @app.exception_handler(RequestValidationError)
    async def validation_error_handler(
        request: Request, exc: RequestValidationError
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
        request: Request, exc: StarletteHTTPException
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
