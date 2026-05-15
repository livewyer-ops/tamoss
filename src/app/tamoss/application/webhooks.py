from __future__ import annotations

import os
import re
from typing import Any
from urllib.parse import urlparse

import requests

VALID_WEBHOOK_EVENTS = {
    "flows/created",
    "flows/updated",
    "flows/deleted",
    "flows/segments_added",
    "flows/segments_deleted",
    "sources/created",
    "sources/updated",
    "sources/deleted",
}
RETRIABLE_STATUS_CODES = {408, 429, 500, 502, 503, 504}

_HTTP_FIELD_NAME = re.compile(r"^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$")
_RESERVED_WEBHOOK_HEADERS = {
    "connection",
    "content-length",
    "content-type",
    "host",
    "keep-alive",
    "proxy-authenticate",
    "proxy-authorization",
    "te",
    "trailer",
    "transfer-encoding",
    "upgrade",
}
_DEFAULT_RETRY_BACKOFF_SECONDS = (30, 60, 120, 300)


def validate_webhook_configuration(data: dict[str, Any]) -> None:
    validate_webhook_url(data.get("url"))
    validate_api_key_header_name(data.get("api_key_name"))
    events = data.get("events") or []
    if not events or any(event not in VALID_WEBHOOK_EVENTS for event in events):
        raise ValueError("Unsupported webhook event")


def validate_webhook_url(value: Any) -> None:
    _parse_webhook_url(str(value) if value is not None else "")


def validate_api_key_header_name(value: Any) -> None:
    if value is None:
        return
    header_name = str(value)
    if (
        not header_name
        or header_name.strip() != header_name
        or not _HTTP_FIELD_NAME.fullmatch(header_name)
        or header_name.lower() in _RESERVED_WEBHOOK_HEADERS
    ):
        raise ValueError("Invalid webhook api_key_name")


def webhook_delivery_snapshot(webhook_data: dict[str, Any], *, status: str) -> dict:
    return {
        "url": webhook_data["url"],
        "api_key_name": webhook_data.get("api_key_name"),
        "api_key_value": webhook_data.get("api_key_value"),
        "accept_get_urls": webhook_data.get("accept_get_urls"),
        "accept_storage_ids": webhook_data.get("accept_storage_ids"),
        "presigned": webhook_data.get("presigned"),
        "verbose_storage": webhook_data.get("verbose_storage"),
        "status": status,
    }


def webhook_matches(
    webhook_data: dict[str, Any],
    *,
    event_type: str,
    flow_ids: list[str],
    source_ids: list[str],
    flow_collected_by_ids: list[str],
    source_collected_by_ids: list[str],
) -> bool:
    if event_type not in _list_of_strings(webhook_data.get("events")):
        return False
    return (
        _selector_matches(flow_ids, _list_of_strings(webhook_data.get("flow_ids")))
        and _selector_matches(
            source_ids, _list_of_strings(webhook_data.get("source_ids"))
        )
        and _selector_matches(
            flow_collected_by_ids,
            _list_of_strings(webhook_data.get("flow_collected_by_ids")),
        )
        and _selector_matches(
            source_collected_by_ids,
            _list_of_strings(webhook_data.get("source_collected_by_ids")),
        )
    )


def send_webhook_delivery(
    *,
    webhook: dict[str, Any],
    payload: dict[str, Any],
) -> requests.Response:
    return requests.post(  # nosec B113 - timeout is supplied from runtime settings.
        str(webhook["url"]),
        headers=webhook_headers(webhook),
        json=payload,
        timeout=delivery_timeout_seconds(),
    )


def webhook_headers(webhook: dict[str, Any]) -> dict[str, str]:
    headers = {"Content-Type": "application/json"}
    api_key_name = webhook.get("api_key_name")
    api_key_value = webhook.get("api_key_value")
    if api_key_name and api_key_value:
        validate_api_key_header_name(api_key_name)
        headers[str(api_key_name)] = str(api_key_value)
    return headers


def _parse_webhook_url(url: str) -> None:
    parsed = urlparse(url)
    if parsed.scheme not in {"https", "http"}:
        raise ValueError("Webhook URL must use http or https")
    if not parsed.hostname:
        raise ValueError("Webhook URL must include a hostname")
    if parsed.username or parsed.password:
        raise ValueError("Webhook URL must not include credentials")
    try:
        port = parsed.port
    except ValueError as exc:
        raise ValueError("Webhook URL port is invalid") from exc
    if port is not None and not 0 < port <= 65535:
        raise ValueError("Webhook URL port is invalid")


def delivery_timeout_seconds() -> float:
    value = os.getenv("TAMOSS_WEBHOOK_TIMEOUT_SECONDS") or "30"
    try:
        return max(1.0, float(value))
    except ValueError:
        return 30.0


def max_attempts() -> int:
    return _positive_int_env("TAMOSS_WORKER_MAX_ATTEMPTS", 5)


def retry_delay(attempt_count: int) -> int:
    index = max(0, min(attempt_count - 1, len(_DEFAULT_RETRY_BACKOFF_SECONDS) - 1))
    return _DEFAULT_RETRY_BACKOFF_SECONDS[index]


def _positive_int_env(name: str, default: int) -> int:
    value = os.getenv(name)
    if value is None:
        return default
    try:
        parsed = int(value)
    except ValueError:
        return default
    return parsed if parsed > 0 else default


def _list_of_strings(value: Any) -> list[str]:
    if value is None:
        return []
    if isinstance(value, list):
        return [str(item) for item in value]
    return []


def _selector_matches(candidates: list[str], required: list[str]) -> bool:
    if not required:
        return True
    if not candidates:
        return False
    return any(candidate in set(candidates) for candidate in required)
