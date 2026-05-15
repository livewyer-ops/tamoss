from __future__ import annotations

from uuid import UUID

from fastapi import Request

from tamoss.errors import BadRequest


def validate_query_params(
    request: Request,
    allowed: set[str],
    *,
    allowed_prefixes: tuple[str, ...] = (),
) -> None:
    for key, _value in request.query_params.multi_items():
        if key == "access_token" or key in allowed:
            continue
        if any(key.startswith(prefix) for prefix in allowed_prefixes):
            continue
        raise BadRequest("Bad request. Invalid query options.")


def parse_get_url_labels(value: str | None) -> set[str] | None:
    if value is None:
        return None
    if value == "":
        return set()
    parts = value.split(",")
    if any(part == "" for part in parts):
        raise BadRequest("Bad request. Invalid query options.")
    return set(parts)


def parse_storage_ids(value: str | None) -> set[str] | None:
    if value is None or value == "":
        return None
    try:
        return {str(UUID(part)) for part in value.split(",")}
    except ValueError as exc:
        raise BadRequest("Bad request. Invalid query options.") from exc
