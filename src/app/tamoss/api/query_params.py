from __future__ import annotations

from uuid import UUID

from fastapi import Query, Request

from tamoss.domain.tags import parse_bool_filter, parse_tag_value_list
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


def parse_flow_tag_filters(
    request: Request,
) -> tuple[dict[str, set[str]], dict[str, bool]]:
    values: dict[str, set[str]] = {}
    exists: dict[str, bool] = {}
    try:
        for key, value in request.query_params.items():
            if key.startswith("flow_tag."):
                values[key.removeprefix("flow_tag.")] = parse_tag_value_list(value)
            elif key.startswith("flow_tag_exists."):
                exists[key.removeprefix("flow_tag_exists.")] = parse_bool_filter(value)
    except ValueError as exc:
        raise BadRequest("Bad request. Invalid query options.") from exc
    return values, exists


def parse_storage_backend_tag_filters(
    request: Request,
) -> tuple[dict[str, set[str]], dict[str, bool]]:
    values: dict[str, set[str]] = {}
    exists: dict[str, bool] = {}
    try:
        for key, value in request.query_params.items():
            if key.startswith("storage_backend_tag."):
                values[key.removeprefix("storage_backend_tag.")] = parse_tag_value_list(
                    value
                )
            elif key.startswith("storage_backend_tag_exists."):
                exists[key.removeprefix("storage_backend_tag_exists.")] = (
                    parse_bool_filter(value)
                )
    except ValueError as exc:
        raise BadRequest("Bad request. Invalid query options.") from exc
    return values, exists


def tag_filter_parameters(
    _tag_value: str | None = Query(
        default=None,
        alias="tag.{name}",
    ),
    _tag_exists: bool | None = Query(
        default=None,
        alias="tag_exists.{name}",
    ),
) -> None:
    return None


def flow_tag_filter_parameters(
    _flow_tag_value: str | None = Query(
        default=None,
        alias="flow_tag.{name}",
    ),
    _flow_tag_exists: bool | None = Query(
        default=None,
        alias="flow_tag_exists.{name}",
    ),
) -> None:
    return None


def storage_backend_tag_filter_parameters(
    _storage_tag_value: str | None = Query(
        default=None,
        alias="storage_backend_tag.{name}",
    ),
    _storage_tag_exists: bool | None = Query(
        default=None,
        alias="storage_backend_tag_exists.{name}",
    ),
) -> None:
    return None
