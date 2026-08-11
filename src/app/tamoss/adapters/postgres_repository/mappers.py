from __future__ import annotations

from collections.abc import Iterable, Sequence
from datetime import datetime
from typing import Any
from uuid import UUID

from mediatimestamp import TimeRange, Timestamp
from psycopg.types.json import Jsonb

from tamoss.adapters.postgres_repository.types import (
    DatabaseRow,
    FlowRow,
    JsonRecord,
    MediaObjectRow,
    ObjectInstanceRow,
    PostgresCursor,
    StorageBackendRow,
)
from tamoss.domain.exceptions import SEGMENT_OVERLAP_MESSAGE, SegmentOverlapError
from tamoss.domain.model import (
    DeletionRequestRecord,
    DomainErrorPayload,
    FlowRecord,
    MediaObjectRecord,
    ObjectCleanupRecord,
    ObjectCopyRecord,
    ObjectInstance,
    SegmentRecord,
    SourceRecord,
    StorageBackend,
    WebhookDeliveryRecord,
    WebhookRecord,
)
from tamoss.domain.timeranges import finite_normalized_timerange_bounds
from tamoss.errors import normalize_error_payload


def _storage_backend_from_record(
    record: StorageBackendRow,
    *,
    configured_storage_backend: StorageBackend,
) -> StorageBackend:
    backend = StorageBackend(
        id=UUID(record["id"]),
        label=record["label"],
        provider=record["provider"],
        region=record["region"],
        store_product=record["store_product"],
        store_type=record.get("store_type") or "http_object_store",
        default_storage=bool(record.get("default_storage", False)),
        bucket_name=record.get("bucket_name"),
        endpoint_url=record.get("endpoint_url"),
        public_endpoint_url=record.get("public_endpoint_url"),
        tags=dict(record.get("tags") or {}),
    )
    if configured_storage_backend.id == backend.id or (
        backend.endpoint_url is not None
        and configured_storage_backend.endpoint_url is not None
        and backend.endpoint_url == configured_storage_backend.endpoint_url
    ):
        backend.endpoint_url = (
            configured_storage_backend.endpoint_url or backend.endpoint_url
        )
        backend.public_endpoint_url = (
            configured_storage_backend.public_endpoint_url
            or backend.public_endpoint_url
        )
        backend.access_key = configured_storage_backend.access_key
        backend.secret_key = configured_storage_backend.secret_key
    return backend


def _flow_from_record(record: FlowRow) -> FlowRecord:
    return FlowRecord(
        id=UUID(record["id"]),
        data=dict(record.get("data") or {}),
        source_id=_optional_uuid(record.get("source_id")),
        format=record.get("format"),
        container=record.get("container"),
        profile_id=_optional_uuid(record.get("profile_id")),
        status=record.get("status"),
        init_segments=bool(record.get("init_segments", False)),
        read_only=bool(record.get("read_only", False)),
        tags=dict(record.get("tags") or {}),
        created=_datetime_from_record(record.get("created")),
        metadata_updated=_datetime_from_record(record.get("metadata_updated")),
        segments_updated=_optional_datetime_from_record(record.get("segments_updated")),
    )


def _media_object_from_record(
    record: MediaObjectRow,
    *,
    storage_backends_by_id: dict[UUID, StorageBackend] | None = None,
) -> MediaObjectRecord:
    return MediaObjectRecord(
        id=record["id"],
        timerange=record.get("timerange"),
        init_object_id=record.get("init_object_id"),
        first_referenced_by_flow=_optional_uuid(record.get("first_referenced_by_flow")),
        allocated_by_flow=_optional_uuid(record.get("allocated_by_flow")),
        referenced_by_flows={
            UUID(flow_id) for flow_id in record.get("referenced_by_flows", [])
        },
        instances=[
            _object_instance_from_record(
                item,
                storage_backends_by_id=storage_backends_by_id,
            )
            for item in record.get("instances") or []
        ],
        key_frame_count=record.get("key_frame_count"),
        bytes_written=int(record.get("bytes_written") or 0),
        object_kind=record.get("object_kind") or "unassigned",
        content_type=record.get("content_type"),
        created=_datetime_from_record(record.get("created")),
    )


def _object_instance_from_record(
    record: ObjectInstanceRow,
    *,
    storage_backends_by_id: dict[UUID, StorageBackend] | None = None,
) -> ObjectInstance:
    storage_backend_id = _optional_uuid(record.get("storage_backend_id"))
    storage_backend = (
        storage_backends_by_id.get(storage_backend_id)
        if storage_backend_id is not None and storage_backends_by_id is not None
        else None
    )
    return ObjectInstance(
        storage_backend=storage_backend,
        url=record.get("url"),
        label=record.get("label"),
        controlled=bool(record.get("controlled", False)),
        presigned=bool(record.get("presigned", False)),
    )


def _save_flow(cur: PostgresCursor, flow: FlowRecord) -> None:
    record = _flow_to_record(flow)
    cur.execute(
        """
        INSERT INTO tamoss_flows (
            id,
            source_id,
            format,
            container,
            profile_id,
            status,
            init_segments,
            label,
            flow_collection_ids,
            read_only,
            tags,
            record,
            metadata_updated,
            created,
            segments_updated,
            updated_at
        )
        VALUES (
            %(id)s,
            %(source_id)s,
            %(format)s,
            %(container)s,
            %(profile_id)s,
            %(status)s,
            %(init_segments)s,
            %(label)s,
            %(flow_collection_ids)s,
            %(read_only)s,
            %(tags)s,
            %(record)s,
            %(metadata_updated)s,
            %(created)s,
            %(segments_updated)s,
            NOW()
        )
        ON CONFLICT (id) DO UPDATE SET
            source_id = EXCLUDED.source_id,
            format = EXCLUDED.format,
            container = EXCLUDED.container,
            profile_id = EXCLUDED.profile_id,
            status = EXCLUDED.status,
            init_segments = EXCLUDED.init_segments,
            label = EXCLUDED.label,
            flow_collection_ids = EXCLUDED.flow_collection_ids,
            read_only = EXCLUDED.read_only,
            tags = EXCLUDED.tags,
            record = EXCLUDED.record,
            metadata_updated = EXCLUDED.metadata_updated,
            created = EXCLUDED.created,
            segments_updated = EXCLUDED.segments_updated,
            updated_at = NOW()
        """,
        {
            "id": flow.id,
            "source_id": flow.source_id,
            "format": flow.format,
            "container": flow.container,
            "profile_id": flow.profile_id,
            "status": flow.status,
            "init_segments": flow.init_segments,
            "label": flow.data.get("label"),
            "flow_collection_ids": [
                UUID(str(item["id"]))
                for item in (flow.data.get("flow_collection") or [])
            ],
            "read_only": flow.read_only,
            "tags": Jsonb(flow.tags),
            "record": Jsonb(record),
            "metadata_updated": flow.metadata_updated,
            "created": flow.created,
            "segments_updated": flow.segments_updated,
        },
    )


def _save_object(cur: PostgresCursor, media_object: MediaObjectRecord) -> None:
    record = _media_object_to_record(media_object)
    cur.execute(
        """
        INSERT INTO tamoss_media_objects (
            id,
            first_referenced_by_flow,
            referenced_by_flows,
            object_kind,
            content_type,
            record,
            updated_at
        )
        VALUES (
            %(id)s,
            %(first_referenced_by_flow)s,
            %(referenced_by_flows)s,
            %(object_kind)s,
            %(content_type)s,
            %(record)s,
            NOW()
        )
        ON CONFLICT (id) DO UPDATE SET
            first_referenced_by_flow = EXCLUDED.first_referenced_by_flow,
            referenced_by_flows = EXCLUDED.referenced_by_flows,
            object_kind = EXCLUDED.object_kind,
            content_type = EXCLUDED.content_type,
            record = EXCLUDED.record,
            updated_at = NOW()
        """,
        {
            "id": media_object.id,
            "first_referenced_by_flow": media_object.first_referenced_by_flow,
            "referenced_by_flows": [
                str(flow_id) for flow_id in media_object.referenced_by_flows
            ],
            "object_kind": media_object.object_kind,
            "content_type": media_object.content_type,
            "record": Jsonb(record),
        },
    )


def _save_objects(
    cur: PostgresCursor, media_objects: Sequence[MediaObjectRecord]
) -> None:
    if not media_objects:
        return
    cur.executemany(
        """
        INSERT INTO tamoss_media_objects (
            id,
            first_referenced_by_flow,
            referenced_by_flows,
            object_kind,
            content_type,
            record,
            updated_at
        )
        VALUES (
            %(id)s,
            %(first_referenced_by_flow)s,
            %(referenced_by_flows)s,
            %(object_kind)s,
            %(content_type)s,
            %(record)s,
            NOW()
        )
        ON CONFLICT (id) DO UPDATE SET
            first_referenced_by_flow = EXCLUDED.first_referenced_by_flow,
            referenced_by_flows = EXCLUDED.referenced_by_flows,
            object_kind = EXCLUDED.object_kind,
            content_type = EXCLUDED.content_type,
            record = EXCLUDED.record,
            updated_at = NOW()
        """,
        [
            {
                "id": media_object.id,
                "first_referenced_by_flow": media_object.first_referenced_by_flow,
                "referenced_by_flows": [
                    str(flow_id) for flow_id in media_object.referenced_by_flows
                ],
                "object_kind": media_object.object_kind,
                "content_type": media_object.content_type,
                "record": Jsonb(_media_object_to_record(media_object)),
            }
            for media_object in media_objects
        ],
    )


def _create_object(cur: PostgresCursor, media_object: MediaObjectRecord) -> bool:
    record = _media_object_to_record(media_object)
    cur.execute(
        """
        INSERT INTO tamoss_media_objects (
            id,
            first_referenced_by_flow,
            referenced_by_flows,
            object_kind,
            content_type,
            record,
            updated_at
        )
        VALUES (
            %(id)s,
            %(first_referenced_by_flow)s,
            %(referenced_by_flows)s,
            %(object_kind)s,
            %(content_type)s,
            %(record)s,
            NOW()
        )
        ON CONFLICT (id) DO NOTHING
        RETURNING id
        """,
        {
            "id": media_object.id,
            "first_referenced_by_flow": media_object.first_referenced_by_flow,
            "referenced_by_flows": [
                str(flow_id) for flow_id in media_object.referenced_by_flows
            ],
            "object_kind": media_object.object_kind,
            "content_type": media_object.content_type,
            "record": Jsonb(record),
        },
    )
    return cur.fetchone() is not None


def _create_objects(
    cur: PostgresCursor, media_objects: Sequence[MediaObjectRecord]
) -> set[str]:
    if not media_objects:
        return set()
    cur.execute(
        """
        INSERT INTO tamoss_media_objects (
            id,
            first_referenced_by_flow,
            referenced_by_flows,
            object_kind,
            content_type,
            record,
            updated_at
        )
        SELECT
            new_object.id,
            new_object.first_referenced_by_flow,
            ARRAY(SELECT jsonb_array_elements_text(new_object.referenced_by_flows)),
            new_object.object_kind,
            new_object.content_type,
            new_object.record,
            NOW()
        FROM unnest(
            %(ids)s::text[],
            %(first_referenced_by_flows)s::uuid[],
            %(referenced_by_flows)s::jsonb[],
            %(object_kinds)s::text[],
            %(content_types)s::text[],
            %(records)s::jsonb[]
        ) AS new_object(
            id,
            first_referenced_by_flow,
            referenced_by_flows,
            object_kind,
            content_type,
            record
        )
        ON CONFLICT (id) DO NOTHING
        RETURNING id
        """,
        {
            "ids": [media_object.id for media_object in media_objects],
            "first_referenced_by_flows": [
                media_object.first_referenced_by_flow for media_object in media_objects
            ],
            "referenced_by_flows": [
                Jsonb([str(flow_id) for flow_id in media_object.referenced_by_flows])
                for media_object in media_objects
            ],
            "object_kinds": [
                media_object.object_kind for media_object in media_objects
            ],
            "content_types": [
                media_object.content_type for media_object in media_objects
            ],
            "records": [
                Jsonb(_media_object_to_record(media_object))
                for media_object in media_objects
            ],
        },
    )
    return {row[0] for row in cur.fetchall()}


def _lock_flow_segments(cur: PostgresCursor, flow_id: UUID) -> None:
    cur.execute(
        "SELECT pg_advisory_xact_lock(hashtextextended(%s, 0))",
        (f"tamoss_segments:{flow_id}",),
    )


def _lock_media_objects(cur: PostgresCursor, object_ids: Iterable[str]) -> None:
    # Advisory locks also serialize registrations for object IDs whose rows do
    # not exist yet. Sorting establishes one global order for multi-object
    # batches, while FOR UPDATE makes other writers of existing rows wait too.
    for object_id in sorted(set(object_ids)):
        cur.execute(
            "SELECT pg_advisory_xact_lock(hashtextextended(%s, 0))",
            (f"tamoss_media_objects:{object_id}",),
        )
        cur.execute(
            "SELECT id FROM tamoss_media_objects WHERE id = %s FOR UPDATE",
            (object_id,),
        )


def _append_segment(
    cur: PostgresCursor, segment: SegmentRecord, *, reject_overlaps: bool = False
) -> None:
    record = _segment_to_record(segment)
    timerange_start, timerange_end = _timerange_bounds(segment.timerange)
    if reject_overlaps:
        _raise_if_segment_overlaps(
            cur,
            flow_id=segment.flow_id,
            timerange_start=timerange_start,
            timerange_end=timerange_end,
        )
    cur.execute(
        """
        INSERT INTO tamoss_segments (
            flow_id,
            object_id,
            init_object_id,
            timerange,
            timerange_start,
            timerange_end,
            record,
            created
        )
        VALUES (
            %(flow_id)s,
            %(object_id)s,
            %(init_object_id)s,
            %(timerange)s,
            %(timerange_start)s,
            %(timerange_end)s,
            %(record)s,
            %(created)s
        )
        ON CONFLICT (flow_id, object_id, timerange) DO UPDATE SET
            init_object_id = EXCLUDED.init_object_id,
            timerange_start = EXCLUDED.timerange_start,
            timerange_end = EXCLUDED.timerange_end,
            record = EXCLUDED.record,
            updated_at = NOW()
        """,
        {
            "flow_id": segment.flow_id,
            "object_id": segment.object_id,
            "init_object_id": segment.init_object_id,
            "timerange": segment.timerange,
            "timerange_start": timerange_start,
            "timerange_end": timerange_end,
            "record": Jsonb(record),
            "created": segment.created,
        },
    )


def _append_segments(
    cur: PostgresCursor,
    segments: Sequence[SegmentRecord],
    *,
    reject_overlaps: bool = False,
) -> None:
    if not segments:
        return
    rows: list[dict[str, Any]] = []
    bounds_by_flow: dict[UUID, list[tuple[int, int]]] = {}
    for segment in segments:
        timerange_start, timerange_end = _timerange_bounds(segment.timerange)
        bounds_by_flow.setdefault(segment.flow_id, []).append(
            (timerange_start, timerange_end)
        )
        rows.append(
            {
                "flow_id": segment.flow_id,
                "object_id": segment.object_id,
                "init_object_id": segment.init_object_id,
                "timerange": segment.timerange,
                "timerange_start": timerange_start,
                "timerange_end": timerange_end,
                "record": Jsonb(_segment_to_record(segment)),
                "created": segment.created,
            }
        )
    if reject_overlaps:
        # Callers validate the batch against itself before writing; this
        # set-based check guards against concurrent rows already committed,
        # replacing one round trip per segment with one per flow.
        for flow_id, bounds in bounds_by_flow.items():
            _raise_if_segments_overlap(cur, flow_id=flow_id, bounds=bounds)
    cur.executemany(
        """
        INSERT INTO tamoss_segments (
            flow_id,
            object_id,
            init_object_id,
            timerange,
            timerange_start,
            timerange_end,
            record,
            created
        )
        VALUES (
            %(flow_id)s,
            %(object_id)s,
            %(init_object_id)s,
            %(timerange)s,
            %(timerange_start)s,
            %(timerange_end)s,
            %(record)s,
            %(created)s
        )
        ON CONFLICT (flow_id, object_id, timerange) DO UPDATE SET
            init_object_id = EXCLUDED.init_object_id,
            timerange_start = EXCLUDED.timerange_start,
            timerange_end = EXCLUDED.timerange_end,
            record = EXCLUDED.record,
            updated_at = NOW()
        """,
        rows,
    )


def _raise_if_segments_overlap(
    cur: PostgresCursor,
    *,
    flow_id: UUID,
    bounds: Sequence[tuple[int, int]],
) -> None:
    cur.execute(
        """
        SELECT 1
        FROM tamoss_segments AS segment
        WHERE segment.flow_id = %(flow_id)s
          AND EXISTS (
              SELECT 1
              FROM unnest(
                  %(starts)s::bigint[],
                  %(ends)s::bigint[]
              ) AS candidate(timerange_start, timerange_end)
              WHERE segment.timerange_start < candidate.timerange_end
                AND segment.timerange_end > candidate.timerange_start
          )
        LIMIT 1
        """,
        {
            "flow_id": flow_id,
            "starts": [start for start, _ in bounds],
            "ends": [end for _, end in bounds],
        },
    )
    if cur.fetchone() is not None:
        raise SegmentOverlapError(SEGMENT_OVERLAP_MESSAGE)


def _raise_if_segment_overlaps(
    cur: PostgresCursor, *, flow_id: UUID, timerange_start: int, timerange_end: int
) -> None:
    cur.execute(
        """
        SELECT 1
        FROM tamoss_segments
        WHERE flow_id = %(flow_id)s
          AND timerange_start < %(timerange_end)s
          AND timerange_end > %(timerange_start)s
        LIMIT 1
        """,
        {
            "flow_id": flow_id,
            "timerange_start": timerange_start,
            "timerange_end": timerange_end,
        },
    )
    if cur.fetchone() is not None:
        raise SegmentOverlapError(SEGMENT_OVERLAP_MESSAGE)


def _storage_backend_to_record(backend: StorageBackend) -> JsonRecord:
    return {
        "id": str(backend.id),
        "label": backend.label,
        "provider": backend.provider,
        "region": backend.region,
        "store_product": backend.store_product,
        "store_type": backend.store_type,
        "default_storage": backend.default_storage,
        "bucket_name": backend.bucket_name,
        "endpoint_url": backend.endpoint_url,
        "public_endpoint_url": backend.public_endpoint_url,
        "tags": backend.tags,
    }


def _source_to_record(source: SourceRecord) -> JsonRecord:
    return {
        "id": str(source.id),
        "format": source.format,
        "label": source.label,
        "description": source.description,
        "tags": source.tags,
        "created": source.created.isoformat(),
        "metadata_updated": source.metadata_updated.isoformat(),
    }


def _source_from_record(record: JsonRecord) -> SourceRecord:
    return SourceRecord(
        id=UUID(record["id"]),
        format=record.get("format"),
        label=record.get("label"),
        description=record.get("description"),
        tags=dict(record.get("tags") or {}),
        created=_datetime_from_record(record.get("created")),
        metadata_updated=_datetime_from_record(record.get("metadata_updated")),
    )


def _flow_to_record(flow: FlowRecord) -> JsonRecord:
    return {
        "id": str(flow.id),
        "data": flow.data,
        "source_id": str(flow.source_id) if flow.source_id else None,
        "format": flow.format,
        "container": flow.container,
        "profile_id": str(flow.profile_id) if flow.profile_id else None,
        "status": flow.status,
        "init_segments": flow.init_segments,
        "read_only": flow.read_only,
        "tags": flow.tags,
        "created": flow.created.isoformat(),
        "metadata_updated": flow.metadata_updated.isoformat(),
        "segments_updated": flow.segments_updated.isoformat()
        if flow.segments_updated
        else None,
    }


def _media_object_to_record(media_object: MediaObjectRecord) -> JsonRecord:
    return {
        "id": media_object.id,
        "timerange": media_object.timerange,
        "init_object_id": media_object.init_object_id,
        "first_referenced_by_flow": str(media_object.first_referenced_by_flow)
        if media_object.first_referenced_by_flow
        else None,
        "allocated_by_flow": str(media_object.allocated_by_flow)
        if media_object.allocated_by_flow
        else None,
        "referenced_by_flows": [
            str(flow_id)
            for flow_id in sorted(media_object.referenced_by_flows, key=str)
        ],
        "instances": [
            _object_instance_to_record(instance) for instance in media_object.instances
        ],
        "key_frame_count": media_object.key_frame_count,
        "bytes_written": media_object.bytes_written,
        "object_kind": media_object.object_kind,
        "content_type": media_object.content_type,
        "created": media_object.created.isoformat(),
    }


def _object_instance_to_record(instance: ObjectInstance) -> JsonRecord:
    return {
        "storage_backend_id": str(instance.storage_backend.id)
        if instance.storage_backend is not None
        else None,
        "url": instance.url,
        "label": instance.label,
        "controlled": instance.controlled,
        "presigned": instance.presigned,
    }


def _segment_to_record(segment: SegmentRecord) -> JsonRecord:
    return {
        "flow_id": str(segment.flow_id),
        "object_id": segment.object_id,
        "init_object_id": segment.init_object_id,
        "timerange": segment.timerange,
        "ts_offset": segment.ts_offset,
        "last_duration": segment.last_duration,
        "object_timerange": segment.object_timerange,
        "sample_offset": segment.sample_offset,
        "sample_count": segment.sample_count,
        "key_frame_count": segment.key_frame_count,
        "created": segment.created.isoformat(),
    }


def _segment_from_record(record: JsonRecord) -> SegmentRecord:
    return SegmentRecord(
        flow_id=UUID(record["flow_id"]),
        object_id=record["object_id"],
        timerange=record["timerange"],
        init_object_id=record.get("init_object_id"),
        ts_offset=record.get("ts_offset"),
        last_duration=record.get("last_duration"),
        object_timerange=record.get("object_timerange"),
        sample_offset=record.get("sample_offset"),
        sample_count=record.get("sample_count"),
        key_frame_count=record.get("key_frame_count"),
        created=_datetime_from_record(record.get("created")),
    )


def _webhook_to_record(webhook: WebhookRecord) -> JsonRecord:
    data = _normalized_webhook_data(webhook.data)
    return {
        "id": str(webhook.id),
        "data": data,
        "status": webhook.status,
        "tags": webhook.tags,
    }


def _webhook_from_record(record: JsonRecord) -> WebhookRecord:
    return WebhookRecord(
        id=UUID(record["id"]),
        data=_normalized_webhook_data(dict(record.get("data") or {})),
        status=record["status"],
        tags=dict(record.get("tags") or {}),
    )


def _webhook_delivery_to_record(delivery: WebhookDeliveryRecord) -> JsonRecord:
    return {
        "id": str(delivery.id),
        "webhook_id": str(delivery.webhook_id),
        "webhook_snapshot": delivery.webhook_snapshot,
        "event_type": delivery.event_type,
        "event_timestamp": delivery.event_timestamp.isoformat(),
        "payload": delivery.payload,
        "status": delivery.status,
        "created": delivery.created.isoformat(),
        "updated": delivery.updated.isoformat(),
        "attempt_count": delivery.attempt_count,
        "next_attempt_at": delivery.next_attempt_at.isoformat()
        if delivery.next_attempt_at
        else None,
        "response_status": delivery.response_status,
        "error": normalize_error_payload(delivery.error),
    }


def _webhook_delivery_from_row(row: DatabaseRow) -> WebhookDeliveryRecord:
    delivery = _webhook_delivery_from_record(row[0])
    delivery.status = row[1]
    delivery.next_attempt_at = _optional_datetime_from_record(row[2])
    delivery.claimed_at = _optional_datetime_from_record(row[3])
    delivery.claimed_by = row[4]
    delivery.claim_expires_at = _optional_datetime_from_record(row[5])
    return delivery


def _webhook_delivery_from_record(record: JsonRecord) -> WebhookDeliveryRecord:
    next_attempt_at = record.get("next_attempt_at")
    return WebhookDeliveryRecord(
        id=UUID(record["id"]),
        webhook_id=UUID(record["webhook_id"]),
        webhook_snapshot=dict(record.get("webhook_snapshot") or {}),
        event_type=record["event_type"],
        event_timestamp=_datetime_from_record(record.get("event_timestamp")),
        payload=dict(record.get("payload") or {}),
        status=record["status"],
        created=_datetime_from_record(record.get("created")),
        updated=_datetime_from_record(record.get("updated")),
        attempt_count=int(record.get("attempt_count") or 0),
        next_attempt_at=_datetime_from_record(next_attempt_at)
        if next_attempt_at
        else None,
        response_status=record.get("response_status"),
        error=DomainErrorPayload.from_json_dict(record.get("error")),
    )


def _delete_request_to_record(request: DeletionRequestRecord) -> JsonRecord:
    return {
        "id": str(request.id),
        "flow_id": str(request.flow_id),
        "timerange_to_delete": request.timerange_to_delete,
        "delete_flow": request.delete_flow,
        "status": request.status,
        "created": request.created.isoformat(),
        "updated": request.updated.isoformat(),
        "timerange_remaining": request.timerange_remaining,
        "created_by": request.created_by,
        "error": normalize_error_payload(request.error),
    }


def _delete_request_from_row(row: DatabaseRow) -> DeletionRequestRecord:
    request = _delete_request_from_record(row[0])
    request.status = row[1]
    request.updated = _datetime_from_record(row[2])
    request.claimed_at = _optional_datetime_from_record(row[3])
    request.claimed_by = row[4]
    request.claim_expires_at = _optional_datetime_from_record(row[5])
    return request


def _delete_request_from_record(record: JsonRecord) -> DeletionRequestRecord:
    return DeletionRequestRecord(
        id=UUID(record["id"]),
        flow_id=UUID(record["flow_id"]),
        timerange_to_delete=record["timerange_to_delete"],
        delete_flow=bool(record["delete_flow"]),
        status=record["status"],
        created=_datetime_from_record(record.get("created")),
        updated=_datetime_from_record(record.get("updated")),
        timerange_remaining=record.get("timerange_remaining"),
        created_by=record.get("created_by"),
        error=DomainErrorPayload.from_json_dict(record.get("error")),
    )


def _object_cleanup_to_record(cleanup: ObjectCleanupRecord) -> JsonRecord:
    return {
        "id": str(cleanup.id),
        "delete_request_id": str(cleanup.delete_request_id)
        if cleanup.delete_request_id
        else None,
        "object_id": cleanup.object_id,
        "storage_backend_id": str(cleanup.storage_backend_id),
        "status": cleanup.status,
        "created": cleanup.created.isoformat(),
        "updated": cleanup.updated.isoformat(),
        "attempt_count": cleanup.attempt_count,
        "error": normalize_error_payload(cleanup.error),
    }


def _object_cleanup_from_row(row: DatabaseRow) -> ObjectCleanupRecord:
    cleanup = _object_cleanup_from_record(row[0])
    cleanup.status = row[1]
    cleanup.updated = _datetime_from_record(row[2])
    cleanup.claimed_at = _optional_datetime_from_record(row[3])
    cleanup.claimed_by = row[4]
    cleanup.claim_expires_at = _optional_datetime_from_record(row[5])
    return cleanup


def _object_cleanup_from_record(record: JsonRecord) -> ObjectCleanupRecord:
    return ObjectCleanupRecord(
        id=UUID(record["id"]),
        delete_request_id=_optional_uuid(record.get("delete_request_id")),
        object_id=record["object_id"],
        storage_backend_id=UUID(record["storage_backend_id"]),
        status=record["status"],
        created=_datetime_from_record(record.get("created")),
        updated=_datetime_from_record(record.get("updated")),
        attempt_count=int(record.get("attempt_count") or 0),
        error=DomainErrorPayload.from_json_dict(record.get("error")),
    )


def _object_copy_to_record(copy: ObjectCopyRecord) -> JsonRecord:
    return {
        "id": str(copy.id),
        "object_id": copy.object_id,
        "source_storage_backend_id": str(copy.source_storage_backend_id),
        "destination_storage_backend_id": str(copy.destination_storage_backend_id),
        "status": copy.status,
        "created": copy.created.isoformat(),
        "updated": copy.updated.isoformat(),
        "attempt_count": copy.attempt_count,
        "error": normalize_error_payload(copy.error),
    }


def _object_copy_from_row(row: DatabaseRow) -> ObjectCopyRecord:
    copy = _object_copy_from_record(row[0])
    copy.status = row[1]
    copy.updated = _datetime_from_record(row[2])
    copy.claimed_at = _optional_datetime_from_record(row[3])
    copy.claimed_by = row[4]
    copy.claim_expires_at = _optional_datetime_from_record(row[5])
    return copy


def _object_copy_from_record(record: JsonRecord) -> ObjectCopyRecord:
    return ObjectCopyRecord(
        id=UUID(record["id"]),
        object_id=record["object_id"],
        source_storage_backend_id=UUID(record["source_storage_backend_id"]),
        destination_storage_backend_id=UUID(record["destination_storage_backend_id"]),
        status=record["status"],
        created=_datetime_from_record(record.get("created")),
        updated=_datetime_from_record(record.get("updated")),
        attempt_count=int(record.get("attempt_count") or 0),
        error=DomainErrorPayload.from_json_dict(record.get("error")),
    )


def _normalized_webhook_data(data: JsonRecord) -> JsonRecord:
    normalized = dict(data)
    if "error" not in normalized:
        return normalized
    error = normalize_error_payload(normalized.get("error"))
    if error is None:
        normalized.pop("error", None)
    else:
        normalized["error"] = error
    return normalized


def _optional_uuid(value: Any) -> UUID | None:
    return UUID(str(value)) if value else None


def _datetime_from_record(value: Any) -> datetime:
    if isinstance(value, datetime):
        return value
    if isinstance(value, str):
        return datetime.fromisoformat(value)
    return datetime.now().astimezone()


def _optional_datetime_from_record(value: Any) -> datetime | None:
    return _datetime_from_record(value) if value else None


def _timerange_bounds(timerange: str) -> tuple[int, int]:
    try:
        parsed = TimeRange.from_str(timerange)
    except Exception as exc:
        raise ValueError("Segment timerange is invalid.") from exc
    try:
        bounds = finite_normalized_timerange_bounds(parsed)
    except ValueError as exc:
        if "finite" in str(exc):
            raise ValueError(
                "Segment timerange must have finite start and end bounds."
            ) from exc
        raise ValueError("Segment timerange must not be empty.") from exc
    assert bounds.start is not None
    assert bounds.end is not None
    return bounds.start, bounds.end


def _timerange_from_bounds(start: int | None, end: int | None) -> str:
    if start is None or end is None:
        return "()"
    start_ts = Timestamp.from_nanosec(start)
    end_ts = Timestamp.from_nanosec(end)
    if start == end:
        return f"[{start_ts}]"
    return f"[{start_ts}_{end_ts})"
