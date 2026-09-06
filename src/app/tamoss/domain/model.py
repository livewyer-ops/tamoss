from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import Any
from uuid import UUID


def utc_now() -> datetime:
    return datetime.now(UTC)


@dataclass(frozen=True)
class DomainErrorPayload:
    type: str
    summary: str
    time: str
    incident_id: str | None = None
    traceback: tuple[str, ...] = field(default_factory=tuple)

    @classmethod
    def create(
        cls,
        error_type: str,
        summary: str,
        *,
        incident_id: str | None = None,
        traceback: tuple[str, ...] = (),
    ) -> DomainErrorPayload:
        return cls(
            type=error_type,
            summary=summary,
            time=utc_now().isoformat(),
            incident_id=incident_id,
            traceback=traceback,
        )

    @classmethod
    def from_json_dict(cls, value: object) -> DomainErrorPayload | None:
        if value is None:
            return None
        if isinstance(value, cls):
            return value
        if not isinstance(value, Mapping):
            raise ValueError("Error payload must be a JSON object.")

        error_type = value.get("type")
        if not isinstance(error_type, str) or not error_type:
            raise ValueError("Error payload requires a non-empty string type.")

        summary = value.get("summary")
        if not isinstance(summary, str) or not summary:
            raise ValueError("Error payload requires a non-empty string summary.")

        raw_time = value.get("time")
        if isinstance(raw_time, datetime):
            timestamp = raw_time
        elif isinstance(raw_time, str) and raw_time:
            timestamp = datetime.fromisoformat(raw_time.replace("Z", "+00:00"))
        else:
            raise ValueError("Error payload requires a date-time string time.")
        if timestamp.tzinfo is None or timestamp.utcoffset() is None:
            raise ValueError("Error payload time must include timezone information.")

        raw_traceback = value.get("traceback")
        if raw_traceback is None:
            traceback = ()
        elif isinstance(raw_traceback, list) and all(
            isinstance(line, str) for line in raw_traceback
        ):
            traceback = tuple(raw_traceback)
        else:
            raise ValueError("Error payload traceback must be a list of strings.")

        incident_id = value.get("incident_id")
        if incident_id is not None and not isinstance(incident_id, str):
            raise ValueError("Error payload incident_id must be a string.")

        return cls(
            type=error_type,
            summary=summary,
            time=timestamp.isoformat(),
            incident_id=incident_id,
            traceback=traceback,
        )

    def to_json_dict(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "type": self.type,
            "summary": self.summary,
            "time": self.time,
        }
        if self.traceback:
            payload["traceback"] = list(self.traceback)
        if self.incident_id is not None:
            payload["incident_id"] = self.incident_id
        return payload


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
    tags: dict[str, str | list[str]] = field(default_factory=dict)


@dataclass
class ProfileRecord:
    id: UUID
    flow_metadata: dict[str, Any]
    label: str | None = None
    description: str | None = None
    created_by: str | None = None
    created: datetime = field(default_factory=utc_now)
    tags: dict[str, str | list[str]] = field(default_factory=dict)


ObjectGetUrlBatchKey = tuple[UUID, str]


@dataclass(slots=True)
class ObjectGetUrlRequest:
    object_id: str
    backend: StorageBackend
    include_direct: bool = True
    include_presigned: bool = True


@dataclass(frozen=True)
class ObjectStorageMetadata:
    content_length: int | None = None
    content_type: str | None = None
    etag: str | None = None
    checksum: str | None = None
    observed_at: datetime = field(default_factory=utc_now)


@dataclass
class ServiceMetadata:
    name: str | None = None
    description: str | None = None


@dataclass
class WebhookRecord:
    id: UUID
    data: dict[str, Any]
    status: str
    tags: dict[str, str | list[str]] = field(default_factory=dict)


@dataclass
class WebhookDeliveryRecord:
    id: UUID
    webhook_id: UUID
    webhook_snapshot: dict[str, Any]
    event_type: str
    event_timestamp: datetime
    payload: dict[str, Any]
    status: str
    created: datetime = field(default_factory=utc_now)
    updated: datetime = field(default_factory=utc_now)
    attempt_count: int = 0
    next_attempt_at: datetime | None = None
    response_status: int | None = None
    error: DomainErrorPayload | None = None
    claimed_at: datetime | None = None
    claimed_by: str | None = None
    claim_expires_at: datetime | None = None
    claim_token: datetime | None = field(default=None, repr=False, compare=False)


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
    error: DomainErrorPayload | None = None
    claimed_at: datetime | None = None
    claimed_by: str | None = None
    claim_expires_at: datetime | None = None
    claim_token: datetime | None = field(default=None, repr=False, compare=False)


@dataclass
class ObjectCleanupRecord:
    id: UUID
    object_id: str
    storage_backend_id: UUID
    status: str
    delete_request_id: UUID | None = None
    created: datetime = field(default_factory=utc_now)
    updated: datetime = field(default_factory=utc_now)
    attempt_count: int = 0
    error: DomainErrorPayload | None = None
    claimed_at: datetime | None = None
    claimed_by: str | None = None
    claim_expires_at: datetime | None = None
    claim_token: datetime | None = field(default=None, repr=False, compare=False)


@dataclass
class ObjectCopyRecord:
    id: UUID
    object_id: str
    source_storage_backend_id: UUID
    destination_storage_backend_id: UUID
    status: str
    created: datetime = field(default_factory=utc_now)
    updated: datetime = field(default_factory=utc_now)
    attempt_count: int = 0
    error: DomainErrorPayload | None = None
    claimed_at: datetime | None = None
    claimed_by: str | None = None
    claim_expires_at: datetime | None = None
    claim_token: datetime | None = field(default=None, repr=False, compare=False)


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
    data: dict[str, Any]
    source_id: UUID | None
    format: str | None
    container: str | None
    profile_id: UUID | None = None
    status: str | None = None
    init_segments: bool = False
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
    init_object_id: str | None = None
    first_referenced_by_flow: UUID | None = None
    allocated_by_flow: UUID | None = None
    referenced_by_flows: set[UUID] = field(default_factory=set)
    instances: list[ObjectInstance] = field(default_factory=list)
    key_frame_count: int | None = None
    bytes_written: int = 0
    object_kind: str = "unassigned"
    content_type: str | None = None
    created: datetime = field(default_factory=utc_now)


@dataclass
class SegmentRecord:
    flow_id: UUID
    object_id: str
    timerange: str
    init_object_id: str | None = None
    ts_offset: str | None = None
    last_duration: str | None = None
    object_timerange: str | None = None
    sample_offset: int | None = None
    sample_count: int | None = None
    key_frame_count: int | None = None
    created: datetime = field(default_factory=utc_now)
