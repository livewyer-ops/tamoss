from __future__ import annotations

from collections.abc import Sequence
from datetime import datetime
from typing import Any, TypedDict

type JsonRecord = dict[str, Any]
type RecordDateTime = datetime | str | None
type DatabaseRow = Sequence[Any]
type PostgresCursor = Any


class StorageBackendRow(TypedDict, total=False):
    id: str
    label: str
    provider: str
    region: str
    store_product: str
    store_type: str | None
    default_storage: bool
    bucket_name: str | None
    endpoint_url: str | None
    public_endpoint_url: str | None
    tags: dict[str, str | list[str]]


class FlowRow(TypedDict, total=False):
    id: str
    data: JsonRecord
    source_id: str | None
    format: str | None
    container: str | None
    profile_id: str | None
    status: str | None
    init_segments: bool
    label: str | None
    read_only: bool
    tags: dict[str, str | list[str]]
    created: RecordDateTime
    metadata_updated: RecordDateTime
    segments_updated: RecordDateTime


class ObjectInstanceRow(TypedDict, total=False):
    storage_backend_id: str | None
    url: str | None
    label: str | None
    controlled: bool
    presigned: bool


class MediaObjectRow(TypedDict, total=False):
    id: str
    timerange: str | None
    init_object_id: str | None
    first_referenced_by_flow: str | None
    allocated_by_flow: str | None
    referenced_by_flows: list[str]
    instances: list[ObjectInstanceRow]
    key_frame_count: int | None
    bytes_written: int
    object_kind: str
    content_type: str | None
