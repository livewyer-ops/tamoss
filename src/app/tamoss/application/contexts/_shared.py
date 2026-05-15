from __future__ import annotations

from dataclasses import dataclass, replace
from datetime import timedelta
from typing import Any, Iterable, cast
from urllib.parse import urlparse
from uuid import UUID, uuid4

from mediatimestamp import TimeRange

from tamoss.api.schemas import (
    FlowCollectionItem,
    FlowSegmentPost,
    FlowStoragePost,
    FlowWrite,
    MediaObjectRegistration,
    ServiceInfoUpdate,
    WebhookPost,
    WebhookPut,
)
from tamoss.application import validation
from tamoss.application import webhooks as webhooking
from tamoss.auth import Identity
from tamoss.domain.model import (
    DeletionRequestRecord,
    FlowRecord,
    MediaObjectRecord,
    ObjectInstance,
    SegmentRecord,
    ServiceMetadata,
    SourceRecord,
    SourceRelationships,
    StorageBackend,
    WebhookDeliveryRecord,
    WebhookRecord,
    utc_now,
)
from tamoss.domain.pagination import Page, page_sequence
from tamoss.domain.tags import TagValue, tags_match
from tamoss.errors import BadRequest, Forbidden, NotFound, error_payload
from tamoss.ports.object_storage import ObjectStorage
from tamoss.ports.repositories import SegmentTimerangeBounds, TamossRepository
from tamoss.settings import Settings

DEFAULT_WORKER_ID = "tamoss-worker"
DEFAULT_WORKER_LEASE_SECONDS = 300
_SERVER_MANAGED_FLOW_FIELDS = {
    "timerange",
    "collected_by",
    "created",
    "metadata_updated",
    "segments_updated",
    "metadata_version",
    "created_by",
    "updated_by",
}


@dataclass
class SegmentWriteResult:
    segment: SegmentRecord | None = None
    error: str | None = None


@dataclass(frozen=True)
class SegmentRegistrationCandidate:
    index: int
    segment_post: FlowSegmentPost
    timerange: SegmentTimerangeBounds


@dataclass(frozen=True)
class QueryTimerange:
    start: int | None = None
    end: int | None = None
    is_empty: bool = False
    is_point: bool = False


class UseCaseContext:
    repository: TamossRepository
    object_storage: ObjectStorage
    settings: Settings

    def __getattr__(self, name: str) -> Any:
        raise AttributeError(name)


__all__ = [
    "Any",
    "BadRequest",
    "DEFAULT_WORKER_ID",
    "DEFAULT_WORKER_LEASE_SECONDS",
    "DeletionRequestRecord",
    "FlowCollectionItem",
    "FlowRecord",
    "FlowSegmentPost",
    "FlowStoragePost",
    "FlowWrite",
    "Forbidden",
    "Identity",
    "Iterable",
    "MediaObjectRecord",
    "MediaObjectRegistration",
    "NotFound",
    "ObjectInstance",
    "ObjectStorage",
    "Page",
    "QueryTimerange",
    "SegmentRecord",
    "SegmentRegistrationCandidate",
    "SegmentTimerangeBounds",
    "SegmentWriteResult",
    "ServiceInfoUpdate",
    "ServiceMetadata",
    "Settings",
    "SourceRecord",
    "SourceRelationships",
    "StorageBackend",
    "TagValue",
    "TamossRepository",
    "TimeRange",
    "UUID",
    "UseCaseContext",
    "WebhookDeliveryRecord",
    "WebhookPost",
    "WebhookPut",
    "WebhookRecord",
    "_SERVER_MANAGED_FLOW_FIELDS",
    "_append_uncontrolled_instance",
    "_clear_worker_claim",
    "_collected_by_by_flow_id",
    "_collection_child_id",
    "_collection_role",
    "_controlled_get_url_payload",
    "_flow_collection",
    "_flow_data_value",
    "_flow_with_collected_by",
    "_include_get_url_payload",
    "_is_http_url",
    "_object_instance_matches",
    "_optional_string_set",
    "_parse_delete_timerange",
    "_parse_query_timerange",
    "_query_timerange",
    "_requested_delete_timerange",
    "_response_get_url_payload",
    "_segment_covered_by_timerange",
    "_segment_object_timerange",
    "_segment_timerange_bounds",
    "_segments_matching_delete_filters",
    "_storage_backend_from_settings",
    "_strip_server_managed_flow_fields",
    "_timerange_covering_segments",
    "_timerange_union",
    "_uncontrolled_get_url_payload",
    "_union_timerange_strings",
    "_valid_tag_value",
    "_validate_uncontrolled_instance_append",
    "cast",
    "dataclass",
    "error_payload",
    "page_sequence",
    "replace",
    "storage_backend_from_settings",
    "tags_from_flow_data",
    "tags_match",
    "timedelta",
    "urlparse",
    "utc_now",
    "uuid4",
    "validation",
    "webhooking",
]


def storage_backend_from_settings(settings: Settings) -> StorageBackend | None:
    if settings.storage_backend is None:
        return None
    return _storage_backend_from_settings(settings.storage_backend)


def _storage_backend_from_settings(configured) -> StorageBackend:
    return StorageBackend(
        id=configured.id,
        label=configured.label,
        provider=configured.provider,
        region=configured.region,
        store_product=configured.store_product,
        store_type=configured.store_type,
        default_storage=configured.default_storage,
        bucket_name=configured.bucket_name,
        endpoint_url=configured.endpoint_url,
        public_endpoint_url=configured.public_endpoint_url,
        access_key=configured.access_key,
        secret_key=configured.secret_key,
    )


def tags_from_flow_data(data: dict) -> dict[str, TagValue]:
    raw = data.get("tags") or {}
    return {str(key): value for key, value in raw.items()}


def _flow_data_value(flow: FlowRecord, name: str) -> object:
    if name in flow.data:
        return flow.data[name]
    essence_parameters = flow.data.get("essence_parameters")
    if isinstance(essence_parameters, dict):
        return essence_parameters.get(name)
    return None


def _object_instance_matches(
    instance: ObjectInstance, *, storage_id: UUID | None, label: str | None
) -> bool:
    if storage_id is not None:
        if (
            instance.storage_backend is None
            or instance.storage_backend.id != storage_id
        ):
            return False
    if label is not None and instance.label != label:
        return False
    return True


def _flow_collection(flow: FlowRecord) -> list[dict]:
    collection = flow.data.get("flow_collection")
    if not isinstance(collection, list):
        return []
    return [dict(item) for item in collection if isinstance(item, dict)]


def _collected_by_by_flow_id(flows: list[FlowRecord]) -> dict[UUID, list[str]]:
    collected_by: dict[UUID, list[str]] = {}
    for parent in flows:
        parent_id = str(parent.id)
        for item in _flow_collection(parent):
            child_id = _collection_child_id(item)
            if child_id is None:
                continue
            parent_ids = collected_by.setdefault(child_id, [])
            if parent_id not in parent_ids:
                parent_ids.append(parent_id)
    return collected_by


def _flow_with_collected_by(flow: FlowRecord, collected_by: list[str]) -> FlowRecord:
    data = dict(flow.data)
    if collected_by:
        data["collected_by"] = collected_by
    else:
        data.pop("collected_by", None)
    return replace(flow, data=data)


def _collection_child_id(item: object) -> UUID | None:
    if not isinstance(item, dict):
        return None
    raw_id = item.get("id")
    if raw_id is None:
        return None
    try:
        return UUID(str(raw_id))
    except ValueError:
        return None


def _collection_role(item: object) -> str | None:
    if not isinstance(item, dict):
        return None
    raw_role = item.get("role")
    if raw_role is None:
        return None
    return str(raw_role)


def _optional_string_set(value: object) -> set[str] | None:
    if value is None:
        return None
    if isinstance(value, list):
        return {str(item) for item in value}
    return set()


def _controlled_get_url_payload(
    get_url: dict[str, Any],
    *,
    storage_backend: StorageBackend,
    verbose_storage: bool = False,
) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "url": get_url.get("url"),
        "label": get_url.get("label") or storage_backend.label,
        "storage_id": str(storage_backend.id),
        "presigned": bool(get_url.get("presigned", False)),
        "controlled": True,
    }
    if verbose_storage:
        payload.update(
            {
                "store_type": storage_backend.store_type,
                "provider": storage_backend.provider,
                "region": storage_backend.region,
                "store_product": storage_backend.store_product,
            }
        )
    return {key: value for key, value in payload.items() if value is not None}


def _uncontrolled_get_url_payload(instance: ObjectInstance) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "url": instance.url,
        "label": instance.label,
        "presigned": instance.presigned,
        "controlled": False,
    }
    return {key: value for key, value in payload.items() if value is not None}


def _response_get_url_payload(
    payload: dict[str, Any],
    *,
    verbose_storage: bool,
) -> dict[str, Any]:
    keys = ["url", "label", "presigned"]
    if verbose_storage:
        keys.extend(
            [
                "storage_id",
                "controlled",
                "store_type",
                "provider",
                "region",
                "store_product",
            ]
        )
    return {key: payload[key] for key in keys if key in payload}


def _include_get_url_payload(
    payload: dict[str, Any],
    *,
    accept_get_urls: set[str] | None = None,
    accept_storage_ids: set[str] | None = None,
    presigned: bool | None = None,
) -> bool:
    if presigned is not None and payload.get("presigned") is not presigned:
        return False
    if accept_get_urls is not None and payload.get("label") not in accept_get_urls:
        return False
    if (
        accept_storage_ids is not None
        and payload.get("storage_id") not in accept_storage_ids
    ):
        return False
    return True


def _append_uncontrolled_instance(
    media_object: MediaObjectRecord, *, url: str, label: str | None, presigned: bool
) -> None:
    _validate_uncontrolled_instance_append(
        media_object,
        url=url,
        label=label,
        presigned=presigned,
    )
    for instance in media_object.instances:
        if (
            not instance.controlled
            and instance.label == label
            and instance.url == url
            and instance.presigned is presigned
        ):
            return
    media_object.instances.append(
        ObjectInstance(
            storage_backend=None,
            url=url,
            label=label,
            controlled=False,
            presigned=presigned,
        )
    )


def _validate_uncontrolled_instance_append(
    media_object: MediaObjectRecord, *, url: str, label: str | None, presigned: bool
) -> None:
    for instance in media_object.instances:
        if instance.controlled or instance.label != label:
            continue
        if instance.url == url and instance.presigned is presigned:
            return
        raise ValueError("conflicting object instance label")


def _is_http_url(value: str) -> bool:
    parsed = urlparse(value)
    return parsed.scheme in {"http", "https"} and bool(parsed.netloc)


def _segments_matching_delete_filters(
    segments: list[SegmentRecord], *, timerange: str | None, object_id: str | None
) -> list[SegmentRecord]:
    requested_range = _parse_delete_timerange(timerange)
    matching: list[SegmentRecord] = []
    for segment in segments:
        if object_id is not None and segment.object_id != object_id:
            continue
        if requested_range is not None and not _segment_covered_by_timerange(
            segment, requested_range
        ):
            continue
        matching.append(segment)
    return matching


def _parse_delete_timerange(timerange: str | None) -> TimeRange | None:
    if timerange is None or timerange in {"", "_"}:
        return None
    try:
        return TimeRange.from_str(timerange)
    except Exception as exc:
        raise BadRequest("Bad request. Invalid query options.") from exc


def _parse_query_timerange(timerange: str | None) -> TimeRange | None:
    if timerange is None or timerange in {"", "_"}:
        return None
    try:
        return TimeRange.from_str(timerange)
    except Exception as exc:
        raise BadRequest("Bad request. Invalid query options.") from exc


def _query_timerange(timerange: str | None) -> QueryTimerange:
    requested_range = _parse_query_timerange(timerange)
    if requested_range is None:
        return QueryTimerange()
    if requested_range.is_empty():
        return QueryTimerange(is_empty=True)

    start = (
        int(requested_range.start.to_nanosec())
        if requested_range.start is not None
        else None
    )
    end = (
        int(requested_range.end.to_nanosec())
        if requested_range.end is not None
        else None
    )
    return QueryTimerange(
        start=start,
        end=end,
        is_point=start is not None and end is not None and start == end,
    )


def _segment_timerange_bounds(timerange: str) -> SegmentTimerangeBounds:
    parsed = validation.parse_timerange(
        timerange,
        field_name="timerange",
        finite=True,
    )
    if parsed.is_empty():
        raise ValueError("timerange must not be empty")
    assert parsed.start is not None
    assert parsed.end is not None
    start = int(parsed.start.to_nanosec())
    end = int(parsed.end.to_nanosec())
    return SegmentTimerangeBounds(
        start=start,
        end=end,
        is_point=start == end,
    )


def _segment_covered_by_timerange(
    segment: SegmentRecord, requested_range: TimeRange
) -> bool:
    try:
        segment_range = TimeRange.from_str(segment.timerange)
    except Exception as exc:
        raise BadRequest("Bad request. Invalid stored Segment timerange.") from exc
    return requested_range.intersect_with(segment_range) == segment_range


def _requested_delete_timerange(
    segments: list[SegmentRecord], *, requested_timerange: str | None
) -> str:
    if requested_timerange not in (None, "", "_"):
        _parse_delete_timerange(requested_timerange)
        return str(requested_timerange)
    return _timerange_covering_segments(segments)


def _timerange_covering_segments(segments: list[SegmentRecord]) -> str:
    return _timerange_union(segments)


def _timerange_union(segments: list[SegmentRecord]) -> str:
    return _union_timerange_strings(segment.timerange for segment in segments) or "()"


def _union_timerange_strings(timeranges: Iterable[str | None]) -> str | None:
    ranges: list[TimeRange] = []
    for timerange_value in timeranges:
        if timerange_value is None:
            continue
        try:
            parsed = TimeRange.from_str(timerange_value)
        except Exception as exc:
            raise BadRequest("Bad request. Invalid stored Segment timerange.") from exc
        if not parsed.is_empty():
            ranges.append(parsed)
    if not ranges:
        return None
    merged = ranges[0]
    for parsed_timerange in ranges[1:]:
        merged = merged.extend_to_encompass_timerange(parsed_timerange)
    return str(merged)


def _strip_server_managed_flow_fields(data: dict[str, Any]) -> None:
    for field_name in _SERVER_MANAGED_FLOW_FIELDS:
        data.pop(field_name, None)


def _segment_object_timerange(segment: SegmentRecord) -> str:
    return segment.object_timerange or segment.timerange


def _clear_worker_claim(record: DeletionRequestRecord | WebhookDeliveryRecord) -> None:
    record.claimed_at = None
    record.claimed_by = None
    record.claim_expires_at = None


def _valid_tag_value(value: TagValue) -> bool:
    if isinstance(value, str):
        return True
    return isinstance(value, list) and all(isinstance(item, str) for item in value)
