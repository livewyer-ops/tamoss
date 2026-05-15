from __future__ import annotations

from datetime import datetime
from typing import Any, Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, field_validator


class BoundaryModel(BaseModel):
    model_config = ConfigDict(extra="allow", populate_by_name=True)


class ServiceInfo(BoundaryModel):
    type: str
    api_version: str
    service_version: str
    name: str | None = None
    description: str | None = None
    event_stream_mechanisms: list[dict[str, str]] = Field(default_factory=list)
    min_object_timeout: str
    min_presigned_url_timeout: str


class ServiceInfoUpdate(BoundaryModel):
    name: str | None = None
    description: str | None = None


class StorageBackendResponse(BoundaryModel):
    id: UUID
    label: str | None = None
    default_storage: bool = False
    store_type: str
    provider: str
    region: str | None = None
    availability_zone: str | None = None
    store_product: str


class WebhookPost(BoundaryModel):
    url: str
    events: list[str]
    api_key_name: str | None = None
    api_key_value: str | None = None
    status: Literal["created", "disabled"] | None = None
    flow_ids: list[UUID] | None = None
    source_ids: list[UUID] | None = None
    flow_collected_by_ids: list[UUID] | None = None
    source_collected_by_ids: list[UUID] | None = None
    accept_get_urls: list[str] | None = None
    accept_storage_ids: list[UUID] | None = None
    presigned: bool | None = None
    verbose_storage: bool | None = None
    tags: dict[str, str | list[str]] | None = None


class WebhookPut(WebhookPost):
    id: UUID
    status: Literal["created", "disabled"]


class ErrorPayload(BoundaryModel):
    type: str
    summary: str
    traceback: list[str] = Field(default_factory=list)
    time: datetime


class WebhookResponse(BoundaryModel):
    id: UUID
    url: str
    events: list[str]
    status: Literal["created", "started", "disabled", "error"]
    api_key_name: str | None = None
    flow_ids: list[UUID] | None = None
    source_ids: list[UUID] | None = None
    flow_collected_by_ids: list[UUID] | None = None
    source_collected_by_ids: list[UUID] | None = None
    accept_get_urls: list[str] | None = None
    accept_storage_ids: list[UUID] | None = None
    presigned: bool | None = None
    verbose_storage: bool | None = None
    tags: dict[str, str | list[str]] = Field(default_factory=dict)
    error: ErrorPayload | None = None


class DeletionRequestResponse(BoundaryModel):
    id: UUID
    flow_id: UUID
    timerange_to_delete: str
    delete_flow: bool
    status: Literal["created", "started", "done", "error"]
    timerange_remaining: str | None = None
    created: str | None = None
    created_by: str | None = None
    updated: str | None = None
    expiry: str | None = None
    error: ErrorPayload | None = None


class FlowCollectionItem(BoundaryModel):
    id: UUID
    role: str
    container_mapping: dict[str, Any] | None = None


class CollectionItem(BoundaryModel):
    id: UUID
    role: str


class FlowWrite(BoundaryModel):
    id: UUID
    source_id: UUID
    format: str | None = None
    label: str | None = None
    description: str | None = None
    created_by: str | None = None
    updated_by: str | None = None
    metadata_version: str | None = None
    generation: int | None = None
    codec: str | None = None
    container: str | None = None
    avg_bit_rate: int | None = None
    max_bit_rate: int | None = None
    segment_duration: dict[str, Any] | None = None
    timerange: str | None = None
    read_only: bool | None = None
    flow_collection: list[FlowCollectionItem] | None = None
    collected_by: list[UUID] | None = None
    container_mapping: dict[str, Any] | None = None
    essence_parameters: dict[str, Any] | None = None
    tags: dict[str, str | list[str]] | None = None


class SourceResponse(BoundaryModel):
    id: UUID
    format: str | None = None
    label: str | None = None
    description: str | None = None
    tags: dict[str, str | list[str]] = Field(default_factory=dict)
    source_collection: list[CollectionItem] | None = None
    collected_by: list[UUID] | None = None


class FlowStoragePost(BoundaryModel):
    limit: int | None = None
    object_ids: list[str] | None = None
    storage_id: UUID | None = None

    @field_validator("limit")
    @classmethod
    def validate_limit(cls, value: int | None) -> int | None:
        if value is not None and value < 1:
            raise ValueError("limit must be greater than zero")
        return value


class HTTPRequestInfo(BoundaryModel):
    url: str
    body: str | None = None
    content_type: str | None = Field(default=None, alias="content-type")
    headers: dict[str, str] | None = None


class MediaObjectAllocation(BoundaryModel):
    object_id: str
    put_url: HTTPRequestInfo


class FlowStorageResponse(BoundaryModel):
    media_objects: list[MediaObjectAllocation] = Field(default_factory=list)


class GetUrl(BoundaryModel):
    url: str
    label: str | None = None
    storage_id: UUID | None = None
    presigned: bool | None = None
    controlled: bool | None = None
    store_type: str | None = None
    provider: str | None = None
    region: str | None = None
    availability_zone: str | None = None
    store_product: str | None = None


class FlowSegmentPost(BoundaryModel):
    object_id: str
    timerange: str
    ts_offset: str | None = None
    last_duration: str | None = None
    object_timerange: str | None = None
    sample_offset: int | None = None
    sample_count: int | None = None
    get_urls: list[dict[str, Any]] | None = None
    key_frame_count: int | None = None


class FlowSegmentResponse(BoundaryModel):
    object_id: str
    timerange: str
    ts_offset: str | None = None
    last_duration: str | None = None
    object_timerange: str | None = None
    sample_offset: int | None = None
    sample_count: int | None = None
    get_urls: list[GetUrl] = Field(default_factory=list)
    key_frame_count: int | None = None


class FailedSegment(BoundaryModel):
    object_id: str
    timerange: str | None = None
    error: ErrorPayload


class FailedSegmentsResponse(BoundaryModel):
    failed_segments: list[FailedSegment]


class ObjectResponse(BoundaryModel):
    id: str
    referenced_by_flows: list[UUID]
    first_referenced_by_flow: UUID | None = None
    timerange: str
    get_urls: list[GetUrl] = Field(default_factory=list)
    key_frame_count: int | None = None


class MediaObjectRegistration(BoundaryModel):
    storage_id: UUID | None = None
    url: str | None = None
    label: str | None = None
