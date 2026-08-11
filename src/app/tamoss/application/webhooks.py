from __future__ import annotations

import ipaddress
import re
import socket
import threading
import time
from collections.abc import Callable, Iterable, Mapping
from dataclasses import dataclass
from functools import lru_cache
from typing import Protocol
from urllib.parse import ParseResult, urljoin, urlparse
from uuid import UUID, uuid4

import requests
from requests import Response
from requests.adapters import HTTPAdapter

from tamoss.application.contexts.object_get_urls import objects_get_urls
from tamoss.contract.payloads import (
    JsonPayload,
    flow_payload,
    source_payload,
    without_none,
)
from tamoss.domain.flow_collections import (
    collected_by_by_flow_id,
    collection_child_id,
    flow_collection,
    flow_with_collected_by,
)
from tamoss.domain.model import (
    FlowRecord,
    MediaObjectRecord,
    SegmentRecord,
    SourceRecord,
    WebhookDeliveryRecord,
    WebhookRecord,
    utc_now,
)
from tamoss.domain.segments import timerange_union
from tamoss.metrics import observe_webhook_delivery
from tamoss.ports.object_storage import ObjectStorage
from tamoss.ports.repositories import (
    WebhookEventRepository,
    WebhookResourceRepository,
)

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
_CLOUD_METADATA_IPS = {
    ipaddress.ip_address("169.254.169.254"),
    ipaddress.ip_address("100.100.100.200"),
}
_METADATA_HOSTNAMES = {
    "metadata",
    "metadata.google.internal",
}
_WEBHOOK_CREDENTIAL_REF = "webhook.api_key_value"

# Webhook receivers are contacted continuously (per-segment events), so
# deliveries share one keepalive-pooled session instead of paying a TCP and
# TLS handshake per callback. Pool sizes bound concurrent connections per
# worker process and must cover the delivery concurrency setting.
_HTTP_POOL_CONNECTIONS = 32
_HTTP_POOL_MAXSIZE = 128

# Egress validation resolves receiver hostnames repeatedly (twice per
# delivery attempt); a short TTL cache absorbs that without materially
# changing rebinding exposure, since the post-validation connect resolves
# independently either way.
_DNS_CACHE_TTL_SECONDS = 30.0
_DNS_CACHE_MAX_ENTRIES = 1024
_dns_cache_lock = threading.Lock()
_dns_cache: dict[
    tuple[str, int],
    tuple[float, list[ipaddress.IPv4Address | ipaddress.IPv6Address]],
] = {}


@lru_cache(maxsize=1)
def _http_session() -> requests.Session:
    session = requests.Session()
    adapter = HTTPAdapter(
        pool_connections=_HTTP_POOL_CONNECTIONS,
        pool_maxsize=_HTTP_POOL_MAXSIZE,
    )
    session.mount("https://", adapter)
    session.mount("http://", adapter)
    return session


@dataclass(frozen=True)
class WebhookEgressPolicy:
    allow_private_targets: bool = False
    allowed_hosts: tuple[str, ...] = ()


class WebhookEgressError(ValueError):
    pass


WebhookData = Mapping[str, object]
WebhookEventFactory = Callable[[WebhookRecord], JsonPayload]
FlowWebhookEventFactory = Callable[[WebhookRecord, list[str]], JsonPayload]


class FlowEventResourceRepository(Protocol):
    def list_flows_by_source(self, source_id: UUID) -> list[FlowRecord]: ...

    def list_flows_collecting(self, flow_ids: Iterable[UUID]) -> list[FlowRecord]: ...

    def get_source(self, source_id: UUID) -> SourceRecord | None: ...


@dataclass(frozen=True)
class FlowEventContext:
    source: SourceRecord | None
    flow_collected_by_ids: list[str]
    source_collected_by_ids: list[str]


def validate_webhook_configuration(
    data: WebhookData,
    *,
    egress_policy: WebhookEgressPolicy | None = None,
) -> None:
    validate_webhook_url(data.get("url"), egress_policy=egress_policy)
    validate_api_key_header_name(data.get("api_key_name"))
    events_value = data.get("events")
    if not isinstance(events_value, list):
        raise ValueError("Unsupported webhook event")
    events = _list_of_strings(events_value)
    if any(event not in VALID_WEBHOOK_EVENTS for event in events):
        raise ValueError("Unsupported webhook event")


def validate_webhook_url(
    value: object,
    *,
    egress_policy: WebhookEgressPolicy | None = None,
) -> None:
    parsed = _parse_webhook_url(str(value) if value is not None else "")
    validate_webhook_target(parsed, egress_policy=egress_policy)


def validate_api_key_header_name(value: object) -> None:
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


def webhook_delivery_snapshot(webhook_data: WebhookData, *, status: str) -> JsonPayload:
    api_key_value_ref = None
    if webhook_data.get("api_key_name") and webhook_data.get("api_key_value"):
        api_key_value_ref = _WEBHOOK_CREDENTIAL_REF
    return {
        "url": webhook_data["url"],
        "api_key_name": webhook_data.get("api_key_name"),
        "api_key_value_ref": api_key_value_ref,
        "accept_get_urls": webhook_data.get("accept_get_urls"),
        "accept_storage_ids": webhook_data.get("accept_storage_ids"),
        "presigned": webhook_data.get("presigned"),
        "verbose_storage": webhook_data.get("verbose_storage"),
        "status": status,
    }


def webhook_for_delivery(
    webhook_snapshot: WebhookData,
    live_webhook_data: WebhookData,
) -> JsonPayload:
    webhook = dict(webhook_snapshot)
    api_key_value_ref = webhook.pop("api_key_value_ref", None)
    if api_key_value_ref == _WEBHOOK_CREDENTIAL_REF:
        webhook["api_key_value"] = live_webhook_data.get("api_key_value")
    return webhook


def webhook_matches(
    webhook_data: WebhookData,
    *,
    event_type: str,
    flow_ids: list[str],
    source_ids: list[str],
    flow_collected_by_ids: list[str],
    source_collected_by_ids: list[str],
) -> bool:
    if event_type not in _list_of_strings(webhook_data.get("events")):
        return False
    is_flow_event = event_type.startswith("flows/")
    flow_filter_matches = True
    flow_collected_filter_matches = True
    if is_flow_event:
        flow_filter_matches = _selector_matches(
            flow_ids,
            _list_of_strings(webhook_data.get("flow_ids")),
        )
        flow_collected_filter_matches = _collection_selector_matches(
            flow_collected_by_ids,
            _optional_list_of_strings(webhook_data.get("flow_collected_by_ids")),
        )
    return (
        flow_filter_matches
        and _selector_matches(
            source_ids, _list_of_strings(webhook_data.get("source_ids"))
        )
        and flow_collected_filter_matches
        and _collection_selector_matches(
            source_collected_by_ids,
            _optional_list_of_strings(webhook_data.get("source_collected_by_ids")),
        )
    )


def active_webhooks_for_event(
    repository: WebhookEventRepository,
    event_type: str,
) -> list[WebhookRecord]:
    """Active webhooks subscribed to the event type.

    Publishing call sites use this to skip event-context queries and payload
    construction entirely when nothing would be delivered.
    """
    return [
        webhook
        for webhook in repository.list_webhooks()
        if webhook.status in {"created", "started"}
        and event_type in _list_of_strings(webhook.data.get("events"))
    ]


def publish_webhook_event(
    *,
    repository: WebhookEventRepository,
    event_type: str,
    event_factory: WebhookEventFactory,
    flow: FlowRecord | None,
    source: SourceRecord | None,
    flow_collected_by_ids: list[str],
    source_collected_by_ids: list[str],
    webhooks: list[WebhookRecord] | None = None,
) -> list[WebhookDeliveryRecord]:
    event_timestamp = utc_now()
    flow_ids = [str(flow.id)] if flow is not None else []
    source_ids = [str(source.id)] if source is not None else []
    deliveries: list[WebhookDeliveryRecord] = []

    candidates = (
        webhooks
        if webhooks is not None
        else active_webhooks_for_event(repository, event_type)
    )
    for webhook in candidates:
        if not webhook_matches(
            webhook.data,
            event_type=event_type,
            flow_ids=flow_ids,
            source_ids=source_ids,
            flow_collected_by_ids=flow_collected_by_ids,
            source_collected_by_ids=source_collected_by_ids,
        ):
            continue
        if webhook.status == "created":
            webhook.status = "started"
            repository.save_webhook(webhook)

        delivery = WebhookDeliveryRecord(
            id=uuid4(),
            webhook_id=webhook.id,
            webhook_snapshot=webhook_delivery_snapshot(
                webhook.data, status=webhook.status
            ),
            event_type=event_type,
            event_timestamp=event_timestamp,
            payload={
                "event_timestamp": event_timestamp.isoformat(),
                "event_type": event_type,
                "event": event_factory(webhook),
            },
            status="pending",
            created=event_timestamp,
            updated=event_timestamp,
            next_attempt_at=event_timestamp,
        )
        repository.save_webhook_delivery(delivery)
        deliveries.append(delivery)
    return deliveries


def _publish_flow_webhook_event(
    *,
    repository: WebhookEventRepository,
    resource_repository: FlowEventResourceRepository,
    event_type: str,
    flow: FlowRecord,
    event_factory: FlowWebhookEventFactory,
    event_context: FlowEventContext | None = None,
) -> list[WebhookDeliveryRecord]:
    webhooks = active_webhooks_for_event(repository, event_type)
    if not webhooks:
        return []
    context = event_context or flow_event_context(resource_repository, flow)
    return publish_webhook_event(
        repository=repository,
        event_type=event_type,
        event_factory=lambda webhook: event_factory(
            webhook, context.flow_collected_by_ids
        ),
        flow=flow,
        source=context.source,
        flow_collected_by_ids=context.flow_collected_by_ids,
        source_collected_by_ids=context.source_collected_by_ids,
        webhooks=webhooks,
    )


def publish_flow_event(
    *,
    repository: WebhookEventRepository,
    resource_repository: FlowEventResourceRepository,
    event_type: str,
    flow: FlowRecord,
) -> list[WebhookDeliveryRecord]:
    return _publish_flow_webhook_event(
        repository=repository,
        resource_repository=resource_repository,
        event_type=event_type,
        flow=flow,
        event_factory=lambda _webhook, collected_by_ids: {
            "flow": flow_payload(flow_with_collected_by(flow, collected_by_ids))
        },
    )


def publish_flow_deleted(
    *,
    repository: WebhookEventRepository,
    resource_repository: FlowEventResourceRepository,
    flow: FlowRecord,
    event_context: FlowEventContext,
) -> list[WebhookDeliveryRecord]:
    return _publish_flow_webhook_event(
        repository=repository,
        resource_repository=resource_repository,
        event_type="flows/deleted",
        flow=flow,
        event_factory=lambda _webhook, _collected_by_ids: {"flow_id": str(flow.id)},
        event_context=event_context,
    )


def publish_segments_added(
    *,
    repository: WebhookEventRepository,
    resource_repository: FlowEventResourceRepository,
    object_storage: ObjectStorage,
    flow: FlowRecord,
    segments: list[SegmentRecord],
    objects_by_id: dict[str, MediaObjectRecord],
) -> list[WebhookDeliveryRecord]:
    return _publish_flow_webhook_event(
        repository=repository,
        resource_repository=resource_repository,
        event_type="flows/segments_added",
        flow=flow,
        event_factory=lambda webhook, _collected_by_ids: segments_added_event(
            flow=flow,
            segments=segments,
            objects_by_id=objects_by_id,
            webhook_data=webhook.data,
            object_storage=object_storage,
        ),
    )


def publish_segments_deleted(
    *,
    repository: WebhookEventRepository,
    resource_repository: FlowEventResourceRepository,
    flow: FlowRecord,
    segments: list[SegmentRecord],
) -> list[WebhookDeliveryRecord]:
    return _publish_flow_webhook_event(
        repository=repository,
        resource_repository=resource_repository,
        event_type="flows/segments_deleted",
        flow=flow,
        event_factory=lambda _webhook, _collected_by_ids: {
            "flow_id": str(flow.id),
            "timerange": timerange_union(segments),
        },
    )


def publish_source_event(
    *,
    repository: WebhookEventRepository,
    resource_repository: WebhookResourceRepository,
    event_type: str,
    source: SourceRecord,
) -> list[WebhookDeliveryRecord]:
    webhooks = active_webhooks_for_event(repository, event_type)
    if not webhooks:
        return []
    relationship = resource_repository.source_relationships_for([source.id]).get(
        source.id
    )
    return publish_webhook_event(
        repository=repository,
        event_type=event_type,
        event_factory=lambda _webhook: {
            "source": source_payload(
                source,
                source_collection=(
                    relationship.source_collection if relationship else None
                ),
                collected_by=relationship.collected_by if relationship else None,
            )
        },
        flow=None,
        source=source,
        flow_collected_by_ids=[],
        source_collected_by_ids=source_collected_by_ids(resource_repository, source),
        webhooks=webhooks,
    )


def publish_source_deleted(
    *,
    repository: WebhookEventRepository,
    source: SourceRecord,
    source_collected_by_ids: list[str],
) -> list[WebhookDeliveryRecord]:
    webhooks = active_webhooks_for_event(repository, "sources/deleted")
    if not webhooks:
        return []
    return publish_webhook_event(
        repository=repository,
        event_type="sources/deleted",
        event_factory=lambda _webhook: {"source_id": str(source.id)},
        flow=None,
        source=source,
        flow_collected_by_ids=[],
        source_collected_by_ids=source_collected_by_ids,
        webhooks=webhooks,
    )


def segment_payload(
    segment: SegmentRecord,
    get_urls: list[JsonPayload],
) -> JsonPayload:
    payload = {
        "object_id": segment.object_id,
        "timerange": segment.timerange,
        "ts_offset": segment.ts_offset,
        "last_duration": segment.last_duration,
        "sample_offset": segment.sample_offset,
        "sample_count": segment.sample_count,
        "get_urls": get_urls,
        "key_frame_count": segment.key_frame_count,
    }
    return without_none(payload)


def segments_added_event(
    *,
    flow: FlowRecord,
    segments: list[SegmentRecord],
    objects_by_id: dict[str, MediaObjectRecord],
    webhook_data: WebhookData,
    object_storage: ObjectStorage,
) -> JsonPayload:
    media_objects = []
    for object_id in dict.fromkeys(segment.object_id for segment in segments):
        media_object = objects_by_id.get(object_id)
        if media_object is not None:
            media_objects.append(media_object)
    get_urls_by_object = (
        objects_get_urls(
            media_objects,
            object_storage=object_storage,
            accept_get_urls=_optional_string_set(webhook_data.get("accept_get_urls")),
            accept_storage_ids=_optional_string_set(
                webhook_data.get("accept_storage_ids")
            ),
            presigned=webhook_data.get("presigned"),
            verbose_storage=bool(webhook_data.get("verbose_storage")),
        )
        if media_objects
        else {}
    )
    return {
        "flow_id": str(flow.id),
        "segments": [
            segment_payload(segment, get_urls_by_object.get(segment.object_id, []))
            for segment in segments
        ],
    }


def flow_event_context(
    repository: FlowEventResourceRepository,
    flow: FlowRecord,
) -> FlowEventContext:
    source = (
        repository.get_source(flow.source_id) if flow.source_id is not None else None
    )
    parents = repository.list_flows_collecting([flow.id])
    collected_by_ids = collected_by_by_flow_id(parents).get(flow.id, [])
    return FlowEventContext(
        source=source,
        flow_collected_by_ids=collected_by_ids,
        source_collected_by_ids=source_collected_by_ids(repository, source),
    )


def source_collected_by_ids(
    repository: FlowEventResourceRepository,
    source: SourceRecord | None,
) -> list[str]:
    if source is None:
        return []
    child_ids = {flow.id for flow in repository.list_flows_by_source(source.id)}
    if not child_ids:
        return []
    collected_by: list[str] = []
    for parent_flow in repository.list_flows_collecting(child_ids):
        if parent_flow.source_id is None:
            continue
        for item in flow_collection(parent_flow):
            child_flow_id = collection_child_id(item)
            if child_flow_id is None:
                continue
            if child_flow_id not in child_ids:
                continue
            source_id = str(parent_flow.source_id)
            if source_id not in collected_by:
                collected_by.append(source_id)
    return collected_by


def _optional_string_set(value: object) -> set[str] | None:
    if value is None:
        return None
    if isinstance(value, list):
        return {str(item) for item in value}
    return set()


def send_webhook_delivery(
    *,
    webhook: WebhookData,
    payload: JsonPayload,
    timeout_seconds: float,
    egress_policy: WebhookEgressPolicy | None = None,
) -> requests.Response:
    url = str(webhook["url"])
    validate_webhook_url(url, egress_policy=egress_policy)
    start = time.perf_counter()
    try:
        response = _http_session().post(
            url,
            headers=webhook_headers(webhook),
            json=payload,
            allow_redirects=False,
            timeout=timeout_seconds,
        )
        _validate_redirect_response(url, response, egress_policy=egress_policy)
    except Exception:
        observe_webhook_delivery("failure", time.perf_counter() - start)
        raise
    observe_webhook_delivery(
        _delivery_outcome(response.status_code),
        time.perf_counter() - start,
    )
    return response


def _delivery_outcome(status_code: int) -> str:
    if 200 <= status_code < 300:
        return "success"
    if status_code in RETRIABLE_STATUS_CODES:
        return "retry"
    return "failure"


def webhook_headers(webhook: WebhookData) -> dict[str, str]:
    headers = {"Content-Type": "application/json"}
    api_key_name = webhook.get("api_key_name")
    api_key_value = webhook.get("api_key_value")
    if api_key_name and api_key_value:
        validate_api_key_header_name(api_key_name)
        headers[str(api_key_name)] = str(api_key_value)
    return headers


def _parse_webhook_url(url: str) -> ParseResult:
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
    return parsed


def validate_webhook_target(
    parsed: ParseResult,
    *,
    egress_policy: WebhookEgressPolicy | None = None,
) -> None:
    policy = egress_policy or WebhookEgressPolicy()
    hostname = _normalized_hostname(parsed.hostname or "")
    if _host_allowed(hostname, policy.allowed_hosts):
        return

    if _is_kubernetes_service_name(hostname) or hostname in _METADATA_HOSTNAMES:
        raise WebhookEgressError("Webhook URL targets a restricted network destination")

    host_ip = _ip_address(hostname)
    if host_ip is not None and _blocked_ip(host_ip):
        raise WebhookEgressError("Webhook URL targets a restricted network destination")

    if policy.allow_private_targets:
        return

    port = parsed.port or (443 if parsed.scheme == "https" else 80)
    for address in _resolve_host_addresses(hostname, port):
        if _blocked_ip(address):
            raise WebhookEgressError(
                "Webhook URL resolves to a restricted network destination"
            )


def retry_delay(attempt_count: int) -> int:
    index = max(0, min(attempt_count - 1, len(_DEFAULT_RETRY_BACKOFF_SECONDS) - 1))
    return _DEFAULT_RETRY_BACKOFF_SECONDS[index]


def _validate_redirect_response(
    source_url: str,
    response: Response,
    *,
    egress_policy: WebhookEgressPolicy | None,
) -> None:
    if response.status_code < 300 or response.status_code >= 400:
        return
    location = response.headers.get("Location")
    if location:
        validate_webhook_url(urljoin(source_url, location), egress_policy=egress_policy)
    raise WebhookEgressError("Webhook redirects are not allowed")


def _list_of_strings(value: object) -> list[str]:
    if value is None:
        return []
    if isinstance(value, list):
        return [str(item) for item in value]
    return []


def _optional_list_of_strings(value: object) -> list[str] | None:
    if value is None:
        return None
    if isinstance(value, list):
        return [str(item) for item in value]
    return None


def _selector_matches(candidates: list[str], required: list[str]) -> bool:
    if not required:
        return True
    if not candidates:
        return False
    candidate_set = set(candidates)
    return any(item in candidate_set for item in required)


def _collection_selector_matches(
    candidates: list[str], required: list[str] | None
) -> bool:
    if required is None:
        return True
    if not required:
        return not candidates
    if not candidates:
        return False
    candidate_set = set(candidates)
    return any(item in candidate_set for item in required)


def _normalized_hostname(hostname: str) -> str:
    return hostname.strip().lower().rstrip(".")


def _is_kubernetes_service_name(hostname: str) -> bool:
    return (
        hostname == "localhost"
        or hostname.endswith(".svc")
        or hostname.endswith(".svc.cluster.local")
        or hostname.endswith(".cluster.local")
    )


def _ip_address(value: str) -> ipaddress.IPv4Address | ipaddress.IPv6Address | None:
    try:
        return ipaddress.ip_address(value)
    except ValueError:
        return None


def _blocked_ip(address: ipaddress.IPv4Address | ipaddress.IPv6Address) -> bool:
    return (
        address in _CLOUD_METADATA_IPS
        or address.is_loopback
        or address.is_link_local
        or address.is_private
        or address.is_multicast
        or address.is_reserved
        or address.is_unspecified
    )


def _host_allowed(hostname: str, allowed_hosts: tuple[str, ...]) -> bool:
    host_ip = _ip_address(hostname)
    for raw_item in allowed_hosts:
        item = _normalized_hostname(raw_item)
        if not item:
            continue
        if item == "*" or item == hostname:
            return True
        if item.startswith(".") and hostname.endswith(item):
            return True
        if host_ip is None:
            continue
        try:
            if "/" in item and host_ip in ipaddress.ip_network(item, strict=False):
                return True
            if "/" not in item and host_ip == ipaddress.ip_address(item):
                return True
        except ValueError:
            continue
    return False


def _resolve_host_addresses(
    hostname: str,
    port: int,
) -> list[ipaddress.IPv4Address | ipaddress.IPv6Address]:
    cache_key = (hostname, port)
    now = time.monotonic()
    with _dns_cache_lock:
        cached = _dns_cache.get(cache_key)
        if cached is not None and now - cached[0] < _DNS_CACHE_TTL_SECONDS:
            return list(cached[1])
    try:
        address_info = socket.getaddrinfo(hostname, port, type=socket.SOCK_STREAM)
    except OSError:
        return []

    addresses: list[ipaddress.IPv4Address | ipaddress.IPv6Address] = []
    for item in address_info:
        sockaddr = item[4]
        if not sockaddr:
            continue
        address = _ip_address(str(sockaddr[0]))
        if address is not None and address not in addresses:
            addresses.append(address)
    with _dns_cache_lock:
        if len(_dns_cache) >= _DNS_CACHE_MAX_ENTRIES:
            _dns_cache.clear()
        _dns_cache[cache_key] = (now, list(addresses))
    return addresses
