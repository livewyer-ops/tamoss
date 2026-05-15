from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any
from uuid import UUID


def utc_now() -> datetime:
    return datetime.now(timezone.utc)


@dataclass
class StorageBackend:
    id: UUID
    label: str
    provider: str
    region: str
    store_product: str
    store_type: str = "http_object_store"
    default_storage: bool = False
    bucket_name: str | None = None
    endpoint_url: str | None = None
    public_endpoint_url: str | None = None
    access_key: str | None = None
    secret_key: str | None = None


@dataclass
class ServiceMetadata:
    name: str | None = None
    description: str | None = None


@dataclass
class WebhookRecord:
    id: UUID
    data: dict
    status: str
    tags: dict[str, str | list[str]] = field(default_factory=dict)


@dataclass
class WebhookDeliveryRecord:
    id: UUID
    webhook_id: UUID
    webhook_snapshot: dict
    event_type: str
    event_timestamp: datetime
    payload: dict
    status: str
    created: datetime = field(default_factory=utc_now)
    updated: datetime = field(default_factory=utc_now)
    attempt_count: int = 0
    next_attempt_at: datetime | None = None
    response_status: int | None = None
    error: dict[str, Any] | None = None
    claimed_at: datetime | None = None
    claimed_by: str | None = None
    claim_expires_at: datetime | None = None


@dataclass
class DeletionRequestRecord:
    id: UUID
    flow_id: UUID
    timerange_to_delete: str
    delete_flow: bool
    status: str
    created: datetime = field(default_factory=utc_now)
    updated: datetime = field(default_factory=utc_now)
    timerange_remaining: str | None = None
    created_by: str | None = None
    error: dict[str, Any] | None = None
    claimed_at: datetime | None = None
    claimed_by: str | None = None
    claim_expires_at: datetime | None = None
    segments_to_delete: list[SegmentRecord] = field(default_factory=list)


@dataclass
class SourceRecord:
    id: UUID
    format: str | None
    label: str | None = None
    description: str | None = None
    tags: dict[str, str | list[str]] = field(default_factory=dict)
    created: datetime = field(default_factory=utc_now)
    metadata_updated: datetime = field(default_factory=utc_now)


@dataclass
class SourceRelationships:
    source_collection: list[dict[str, str]]
    collected_by: list[UUID]


@dataclass
class FlowRecord:
    id: UUID
    data: dict
    source_id: UUID | None
    format: str | None
    container: str | None
    read_only: bool = False
    tags: dict[str, str | list[str]] = field(default_factory=dict)
    created: datetime = field(default_factory=utc_now)
    metadata_updated: datetime = field(default_factory=utc_now)
    segments_updated: datetime | None = None


@dataclass
class ObjectInstance:
    storage_backend: StorageBackend | None
    url: str | None
    label: str | None
    controlled: bool
    presigned: bool = False


@dataclass
class MediaObjectRecord:
    id: str
    timerange: str | None = None
    first_referenced_by_flow: UUID | None = None
    referenced_by_flows: set[UUID] = field(default_factory=set)
    instances: list[ObjectInstance] = field(default_factory=list)
    key_frame_count: int | None = None
    bytes_written: int = 0


@dataclass
class SegmentRecord:
    flow_id: UUID
    object_id: str
    timerange: str
    ts_offset: str | None = None
    last_duration: str | None = None
    object_timerange: str | None = None
    sample_offset: int | None = None
    sample_count: int | None = None
    get_urls: list[dict] = field(default_factory=list)
    key_frame_count: int | None = None
    created: datetime = field(default_factory=utc_now)
