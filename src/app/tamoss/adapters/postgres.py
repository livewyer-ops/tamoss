from __future__ import annotations

from collections.abc import Iterable, Iterator
from contextlib import contextmanager
from contextvars import ContextVar
from dataclasses import replace
from datetime import datetime
from typing import Any
from uuid import UUID

import psycopg
from mediatimestamp import TimeRange, Timestamp
from psycopg import sql
from psycopg.types.json import Jsonb
from psycopg_pool import ConnectionPool

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
)
from tamoss.domain.pagination import Page, resolve_page_window
from tamoss.errors import normalize_error_payload
from tamoss.ports.repositories import SegmentTimerangeBounds

_SERVICE_METADATA_ID = "default"


class PostgresRepository:
    def __init__(
        self,
        *,
        storage_backend: StorageBackend,
        database_url: str | None = None,
        connection: Any | None = None,
        pool: ConnectionPool | None = None,
        pool_min_size: int = 1,
        pool_max_size: int = 10,
    ) -> None:
        if database_url is None and connection is None and pool is None:
            raise ValueError("database_url, connection, or pool is required")
        self._database_url = database_url
        self._connection = connection
        self._pool = pool
        self._owns_pool = False
        self._configured_storage_backend = storage_backend
        self._transaction_connection: ContextVar[Any | None] = ContextVar(
            "tamoss_postgres_transaction_connection",
            default=None,
        )
        if self._pool is None and self._connection is None:
            assert self._database_url is not None
            self._pool = ConnectionPool(
                conninfo=self._database_url,
                min_size=pool_min_size,
                max_size=max(pool_min_size, pool_max_size),
                open=True,
                name="tamoss-postgres",
            )
            self._owns_pool = True
        self._ensure_schema()
        self._upsert_configured_storage_backend(storage_backend)

    @contextmanager
    def unit_of_work(self) -> Iterator[PostgresRepository]:
        active_connection = self._transaction_connection.get()
        if active_connection is not None:
            yield self
            return

        if self._connection is not None:
            with self._connection.transaction():
                token = self._transaction_connection.set(self._connection)
                try:
                    yield self
                finally:
                    self._transaction_connection.reset(token)
            return

        if self._pool is not None:
            with self._pool.connection() as conn, conn.transaction():
                token = self._transaction_connection.set(conn)
                try:
                    yield self
                finally:
                    self._transaction_connection.reset(token)
            return

        assert self._database_url is not None
        conn = psycopg.connect(self._database_url)
        try:
            with conn.transaction():
                token = self._transaction_connection.set(conn)
                try:
                    yield self
                finally:
                    self._transaction_connection.reset(token)
        finally:
            conn.close()

    def lock_flow_segments(self, flow_id: UUID) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            _lock_flow_segments(cur, flow_id)

    def get_service_metadata(self) -> ServiceMetadata | None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT name, description
                FROM tamoss_service_metadata
                WHERE id = %s
                """,
                (_SERVICE_METADATA_ID,),
            )
            row = cur.fetchone()
            if row is None:
                return None
            return ServiceMetadata(name=row[0], description=row[1])

    def save_service_metadata(self, metadata: ServiceMetadata) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                INSERT INTO tamoss_service_metadata (
                    id,
                    name,
                    description,
                    updated_at
                )
                VALUES (%s, %s, %s, NOW())
                ON CONFLICT (id) DO UPDATE SET
                    name = EXCLUDED.name,
                    description = EXCLUDED.description,
                    updated_at = NOW()
                """,
                (_SERVICE_METADATA_ID, metadata.name, metadata.description),
            )

    def list_storage_backends(self) -> list[StorageBackend]:
        return [self._configured_storage_backend]

    def default_storage_backend(self) -> StorageBackend | None:
        return self._configured_storage_backend

    def get_storage_backend(self, storage_id: UUID) -> StorageBackend | None:
        if storage_id == self._configured_storage_backend.id:
            return self._configured_storage_backend
        return None

    def list_flows(self) -> list[FlowRecord]:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute("SELECT record FROM tamoss_flows ORDER BY id")
            return [self._flow_from_record(row[0]) for row in cur.fetchall()]

    def list_flows_page(
        self,
        *,
        source_id: UUID | None,
        timerange_start: int | None,
        timerange_end: int | None,
        timerange_is_empty: bool,
        timerange_is_point: bool,
        format: str | None,
        codec: str | None,
        label: str | None,
        frame_width: int | None,
        frame_height: int | None,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[FlowRecord]:
        window = resolve_page_window(page=page, limit=limit)
        clauses: list[Any] = []
        params: dict[str, Any] = {
            "offset": window.offset,
            "limit": window.limit + 1,
        }
        if source_id is not None:
            clauses.append(sql.SQL("flow.source_id = %(source_id)s"))
            params["source_id"] = source_id
        if format is not None:
            clauses.append(sql.SQL("flow.format = %(format)s"))
            params["format"] = format
        if codec is not None:
            clauses.append(sql.SQL("flow.record #>> '{data,codec}' = %(codec)s"))
            params["codec"] = codec
        if label is not None:
            clauses.append(sql.SQL("flow.record #>> '{data,label}' = %(label)s"))
            params["label"] = label
        if frame_width is not None:
            clauses.append(
                sql.SQL(
                    "flow.record #> '{data,essence_parameters,frame_width}' = "
                    "to_jsonb(%(frame_width)s::int)"
                )
            )
            params["frame_width"] = frame_width
        if frame_height is not None:
            clauses.append(
                sql.SQL(
                    "flow.record #> '{data,essence_parameters,frame_height}' = "
                    "to_jsonb(%(frame_height)s::int)"
                )
            )
            params["frame_height"] = frame_height
        _append_flow_timerange_filter(
            clauses,
            params,
            timerange_start=timerange_start,
            timerange_end=timerange_end,
            timerange_is_empty=timerange_is_empty,
            timerange_is_point=timerange_is_point,
        )
        _append_tag_filter_clauses(
            clauses,
            params,
            column_sql="flow.tags",
            value_filters=tag_values,
            existence_filters=tag_exists,
        )
        where_sql = _where_sql(clauses)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    """
                    SELECT flow.record
                    FROM tamoss_flows AS flow
                    {}
                    ORDER BY flow.id
                    OFFSET %(offset)s
                    LIMIT %(limit)s
                    """
                ).format(where_sql),
                params,
            )
            rows = cur.fetchall()
            flows = [self._flow_from_record(row[0]) for row in rows[: window.limit]]
            flows = _flows_with_collected_by(cur, flows)
        next_page = (
            str(window.offset + window.limit) if len(rows) > window.limit else None
        )
        return Page(items=flows, limit=window.limit, next_page=next_page)

    def flow_timeranges(self, flow_ids: Iterable[UUID]) -> dict[UUID, str]:
        requested_ids = list(dict.fromkeys(flow_ids))
        timeranges = {flow_id: "()" for flow_id in requested_ids}
        if not requested_ids:
            return timeranges

        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT flow_id, MIN(timerange_start), MAX(timerange_end)
                FROM tamoss_segments
                WHERE flow_id = ANY(%(flow_ids)s::uuid[])
                GROUP BY flow_id
                """,
                {"flow_ids": requested_ids},
            )
            rows = cur.fetchall()

        for flow_id, timerange_start, timerange_end in rows:
            timeranges[flow_id] = _timerange_from_bounds(timerange_start, timerange_end)
        return timeranges

    def get_flow(self, flow_id: UUID) -> FlowRecord | None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute("SELECT record FROM tamoss_flows WHERE id = %s", (flow_id,))
            row = cur.fetchone()
            return self._flow_from_record(row[0]) if row else None

    def save_flow(self, flow: FlowRecord) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            _save_flow(cur, flow)

    def delete_flow(self, flow_id: UUID) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute("DELETE FROM tamoss_flows WHERE id = %s", (flow_id,))

    def get_source(self, source_id: UUID) -> SourceRecord | None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute("SELECT record FROM tamoss_sources WHERE id = %s", (source_id,))
            row = cur.fetchone()
            return _source_from_record(row[0]) if row else None

    def save_source(self, source: SourceRecord) -> None:
        record = _source_to_record(source)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                INSERT INTO tamoss_sources (
                    id,
                    format,
                    label,
                    tags,
                    record,
                    metadata_updated,
                    updated_at
                )
                VALUES (
                    %(id)s,
                    %(format)s,
                    %(label)s,
                    %(tags)s,
                    %(record)s,
                    %(metadata_updated)s,
                    NOW()
                )
                ON CONFLICT (id) DO UPDATE SET
                    format = EXCLUDED.format,
                    label = EXCLUDED.label,
                    tags = EXCLUDED.tags,
                    record = EXCLUDED.record,
                    metadata_updated = EXCLUDED.metadata_updated,
                    updated_at = NOW()
                """,
                {
                    "id": source.id,
                    "format": source.format,
                    "label": source.label,
                    "tags": Jsonb(source.tags),
                    "record": Jsonb(record),
                    "metadata_updated": source.metadata_updated,
                },
            )

    def delete_source(self, source_id: UUID) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute("DELETE FROM tamoss_sources WHERE id = %s", (source_id,))

    def list_sources(self) -> list[SourceRecord]:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute("SELECT record FROM tamoss_sources ORDER BY id")
            return [_source_from_record(row[0]) for row in cur.fetchall()]

    def list_sources_page(
        self,
        *,
        label: str | None,
        format: str | None,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[SourceRecord]:
        window = resolve_page_window(page=page, limit=limit)
        clauses: list[Any] = []
        params: dict[str, Any] = {
            "offset": window.offset,
            "limit": window.limit + 1,
        }
        if label is not None:
            clauses.append(sql.SQL("source.label = %(label)s"))
            params["label"] = label
        if format is not None:
            clauses.append(sql.SQL("source.format = %(format)s"))
            params["format"] = format
        _append_tag_filter_clauses(
            clauses,
            params,
            column_sql="source.tags",
            value_filters=tag_values,
            existence_filters=tag_exists,
        )
        where_sql = _where_sql(clauses)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    """
                    SELECT source.record
                    FROM tamoss_sources AS source
                    {}
                    ORDER BY source.id
                    OFFSET %(offset)s
                    LIMIT %(limit)s
                    """
                ).format(where_sql),
                params,
            )
            rows = cur.fetchall()
        sources = [_source_from_record(row[0]) for row in rows[: window.limit]]
        next_page = (
            str(window.offset + window.limit) if len(rows) > window.limit else None
        )
        return Page(items=sources, limit=window.limit, next_page=next_page)

    def source_relationships_for(
        self, source_ids: Iterable[UUID]
    ) -> dict[UUID, SourceRelationships]:
        requested_ids = list(source_ids)
        relationships = {
            source_id: SourceRelationships(source_collection=[], collected_by=[])
            for source_id in requested_ids
        }
        if not requested_ids:
            return relationships

        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT
                    parent.source_id AS parent_source_id,
                    child.source_id AS child_source_id,
                    item.value->>'role' AS role
                FROM tamoss_flows AS parent
                CROSS JOIN LATERAL jsonb_array_elements(
                    CASE
                        WHEN jsonb_typeof(
                            parent.record->'data'->'flow_collection'
                        ) = 'array'
                        THEN parent.record->'data'->'flow_collection'
                        ELSE '[]'::jsonb
                    END
                ) AS item(value)
                JOIN tamoss_flows AS child
                  ON child.id::text = item.value->>'id'
                WHERE parent.source_id IS NOT NULL
                  AND child.source_id IS NOT NULL
                  AND item.value->>'role' IS NOT NULL
                  AND (
                    parent.source_id = ANY(%(source_ids)s::uuid[])
                    OR child.source_id = ANY(%(source_ids)s::uuid[])
                  )
                ORDER BY parent.source_id, child.source_id, role
                """,
                {"source_ids": requested_ids},
            )
            rows = cur.fetchall()

        for parent_source_id, child_source_id, role in rows:
            if parent_source_id in relationships:
                source_item = {"id": str(child_source_id), "role": str(role)}
                source_collection = relationships[parent_source_id].source_collection
                if source_item not in source_collection:
                    source_collection.append(source_item)
            if child_source_id in relationships:
                collected_by = relationships[child_source_id].collected_by
                if parent_source_id not in collected_by:
                    collected_by.append(parent_source_id)
        return relationships

    def get_object(self, object_id: str) -> MediaObjectRecord | None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                "SELECT record FROM tamoss_media_objects WHERE id = %s",
                (object_id,),
            )
            row = cur.fetchone()
            return self._media_object_from_record(row[0]) if row else None

    def get_objects(self, object_ids: Iterable[str]) -> dict[str, MediaObjectRecord]:
        requested_ids = list(set(object_ids))
        if not requested_ids:
            return {}
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT id, record
                FROM tamoss_media_objects
                WHERE id = ANY(%s)
                """,
                (requested_ids,),
            )
            return {
                row[0]: self._media_object_from_record(row[1]) for row in cur.fetchall()
            }

    def save_object(self, media_object: MediaObjectRecord) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            _save_object(cur, media_object)

    def create_object(self, media_object: MediaObjectRecord) -> bool:
        with self._connect() as conn, conn.cursor() as cur:
            return _create_object(cur, media_object)

    def delete_object(self, object_id: str) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute("DELETE FROM tamoss_media_objects WHERE id = %s", (object_id,))

    def list_segments(self, flow_id: UUID) -> list[SegmentRecord]:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT record
                FROM tamoss_segments
                WHERE flow_id = %s
                ORDER BY timerange_end, timerange_start, object_id
                """,
                (flow_id,),
            )
            return [_segment_from_record(row[0]) for row in cur.fetchall()]

    def list_segments_overlapping(
        self,
        *,
        flow_id: UUID,
        timeranges: Iterable[SegmentTimerangeBounds],
    ) -> list[SegmentRecord]:
        bounds = list(timeranges)
        if not bounds:
            return []

        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                WITH candidate_bounds(timerange_start, timerange_end, is_point) AS (
                    SELECT *
                    FROM unnest(
                        %(starts)s::bigint[],
                        %(ends)s::bigint[],
                        %(is_points)s::boolean[]
                    )
                )
                    SELECT record
                    FROM tamoss_segments AS segment
                    WHERE segment.flow_id = %(flow_id)s
                      AND EXISTS (
                          SELECT 1
                          FROM candidate_bounds AS candidate
                          WHERE (
                              candidate.is_point
                              AND (
                                  (
                                      segment.timerange_start < segment.timerange_end
                                      AND segment.timerange_start
                                          <= candidate.timerange_start
                                      AND segment.timerange_end
                                          > candidate.timerange_start
                                  )
                                  OR (
                                      segment.timerange_start = segment.timerange_end
                                      AND segment.timerange_start
                                          = candidate.timerange_start
                                  )
                              )
                          )
                          OR (
                              NOT candidate.is_point
                              AND segment.timerange_start < candidate.timerange_end
                              AND (
                                  (
                                      segment.timerange_start < segment.timerange_end
                                      AND segment.timerange_end
                                          > candidate.timerange_start
                                  )
                                  OR (
                                      segment.timerange_start = segment.timerange_end
                                      AND segment.timerange_start
                                          >= candidate.timerange_start
                                  )
                              )
                          )
                      )
                    ORDER BY
                        segment.timerange_end,
                        segment.timerange_start,
                        segment.object_id
                """,
                {
                    "flow_id": flow_id,
                    "starts": [timerange.start for timerange in bounds],
                    "ends": [timerange.end for timerange in bounds],
                    "is_points": [timerange.is_point for timerange in bounds],
                },
            )
            return [_segment_from_record(row[0]) for row in cur.fetchall()]

    def list_segments_page(
        self,
        *,
        flow_id: UUID,
        object_id: str | None,
        timerange_start: int | None,
        timerange_end: int | None,
        timerange_is_empty: bool,
        timerange_is_point: bool,
        reverse_order: bool,
        page: str | None,
        limit: int | None,
    ) -> Page[SegmentRecord]:
        window = resolve_page_window(page=page, limit=limit)
        if timerange_is_empty:
            return Page(items=[], limit=window.limit, timerange="()")

        clauses = ["flow_id = %(flow_id)s"]
        params: dict[str, Any] = {
            "flow_id": flow_id,
            "offset": window.offset,
            "limit": window.limit + 1,
        }
        if object_id is not None:
            clauses.append("object_id = %(object_id)s")
            params["object_id"] = object_id

        if timerange_is_point and timerange_start is not None:
            clauses.append(
                """
                (
                    (
                        timerange_start < timerange_end
                        AND timerange_start <= %(timerange_start)s
                        AND timerange_end > %(timerange_start)s
                    )
                    OR (
                        timerange_start = timerange_end
                        AND timerange_start = %(timerange_start)s
                    )
                )
                """
            )
            params["timerange_start"] = timerange_start
        else:
            if timerange_end is not None:
                clauses.append("timerange_start < %(timerange_end)s")
                params["timerange_end"] = timerange_end
            if timerange_start is not None:
                clauses.append(
                    """
                    (
                        (
                            timerange_start < timerange_end
                            AND timerange_end > %(timerange_start)s
                        )
                        OR (
                            timerange_start = timerange_end
                            AND timerange_start >= %(timerange_start)s
                        )
                    )
                    """
                )
                params["timerange_start"] = timerange_start

        where_sql = sql.SQL(" AND ").join(sql.SQL(clause) for clause in clauses)
        order_by = (
            "timerange_end DESC, timerange_start DESC, object_id DESC"
            if reverse_order
            else "timerange_end ASC, timerange_start ASC, object_id ASC"
        )
        order_by_sql = sql.SQL(order_by)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    """
                SELECT MIN(timerange_start), MAX(timerange_end)
                FROM tamoss_segments
                WHERE {}
                """
                ).format(where_sql),
                params,
            )
            range_row = cur.fetchone()

            cur.execute(
                sql.SQL(
                    """
                SELECT record
                FROM tamoss_segments
                WHERE {}
                ORDER BY {}
                OFFSET %(offset)s
                LIMIT %(limit)s
                """
                ).format(where_sql, order_by_sql),
                params,
            )
            fetched_segments = [_segment_from_record(row[0]) for row in cur.fetchall()]

        items = fetched_segments[: window.limit]
        next_page = (
            str(window.offset + len(items))
            if len(fetched_segments) > window.limit
            else None
        )
        matched_timerange = (
            _timerange_from_bounds(range_row[0], range_row[1]) if range_row else "()"
        )
        return Page(
            items=items,
            limit=window.limit,
            next_page=next_page,
            timerange=matched_timerange,
        )

    def append_segment(self, segment: SegmentRecord) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            _append_segment(cur, segment)

    def save_registered_segments(
        self,
        *,
        flow: FlowRecord,
        media_objects: Iterable[MediaObjectRecord],
        segments: Iterable[SegmentRecord],
    ) -> None:
        media_objects = list(media_objects)
        segments = list(segments)
        if self._transaction_connection.get() is None:
            with self.unit_of_work():
                self.save_registered_segments(
                    flow=flow,
                    media_objects=media_objects,
                    segments=segments,
                )
            return

        with self._connect() as conn, conn.cursor() as cur:
            _lock_flow_segments(cur, flow.id)
            _save_flow(cur, flow)
            for media_object in media_objects:
                _save_object(cur, media_object)
            for segment in segments:
                _append_segment(cur, segment, reject_overlaps=True)

    def replace_segments(self, flow_id: UUID, segments: list[SegmentRecord]) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute("DELETE FROM tamoss_segments WHERE flow_id = %s", (flow_id,))
            for segment in segments:
                record = _segment_to_record(segment)
                timerange_start, timerange_end = _timerange_bounds(segment.timerange)
                cur.execute(
                    """
                    INSERT INTO tamoss_segments (
                        flow_id,
                        object_id,
                        timerange,
                        timerange_start,
                        timerange_end,
                        record,
                        created
                    )
                    VALUES (
                        %(flow_id)s,
                        %(object_id)s,
                        %(timerange)s,
                        %(timerange_start)s,
                        %(timerange_end)s,
                        %(record)s,
                        %(created)s
                    )
                    """,
                    {
                        "flow_id": segment.flow_id,
                        "object_id": segment.object_id,
                        "timerange": segment.timerange,
                        "timerange_start": timerange_start,
                        "timerange_end": timerange_end,
                        "record": Jsonb(record),
                        "created": segment.created,
                    },
                )

    def list_webhooks(self) -> list[WebhookRecord]:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute("SELECT record FROM tamoss_webhooks ORDER BY id")
            return [_webhook_from_record(row[0]) for row in cur.fetchall()]

    def list_webhooks_page(
        self,
        *,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[WebhookRecord]:
        window = resolve_page_window(page=page, limit=limit)
        clauses: list[Any] = []
        params: dict[str, Any] = {
            "offset": window.offset,
            "limit": window.limit + 1,
        }
        _append_tag_filter_clauses(
            clauses,
            params,
            column_sql="webhook.tags",
            value_filters=tag_values,
            existence_filters=tag_exists,
        )
        where_sql = _where_sql(clauses)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    """
                    SELECT webhook.record
                    FROM tamoss_webhooks AS webhook
                    {}
                    ORDER BY webhook.id
                    OFFSET %(offset)s
                    LIMIT %(limit)s
                    """
                ).format(where_sql),
                params,
            )
            rows = cur.fetchall()
        webhooks = [_webhook_from_record(row[0]) for row in rows[: window.limit]]
        next_page = (
            str(window.offset + window.limit) if len(rows) > window.limit else None
        )
        return Page(items=webhooks, limit=window.limit, next_page=next_page)

    def list_flow_ids_matching_tags_page(
        self,
        *,
        flow_ids: Iterable[UUID],
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[UUID]:
        window = resolve_page_window(page=page, limit=limit)
        requested_ids = list(flow_ids)
        if not requested_ids:
            return Page(items=[], limit=window.limit)

        clauses: list[Any] = [sql.SQL("flow.id = ANY(%(flow_ids)s::uuid[])")]
        params: dict[str, Any] = {
            "flow_ids": requested_ids,
            "offset": window.offset,
            "limit": window.limit + 1,
        }
        _append_tag_filter_clauses(
            clauses,
            params,
            column_sql="flow.tags",
            value_filters=tag_values,
            existence_filters=tag_exists,
        )
        where_sql = _where_sql(clauses)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    """
                    SELECT flow.id
                    FROM tamoss_flows AS flow
                    {}
                    ORDER BY flow.id
                    OFFSET %(offset)s
                    LIMIT %(limit)s
                    """
                ).format(where_sql),
                params,
            )
            rows = cur.fetchall()
        items = [row[0] for row in rows[: window.limit]]
        next_page = (
            str(window.offset + window.limit) if len(rows) > window.limit else None
        )
        return Page(items=items, limit=window.limit, next_page=next_page)

    def get_webhook(self, webhook_id: UUID) -> WebhookRecord | None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                "SELECT record FROM tamoss_webhooks WHERE id = %s",
                (webhook_id,),
            )
            row = cur.fetchone()
            return _webhook_from_record(row[0]) if row else None

    def save_webhook(self, webhook: WebhookRecord) -> None:
        record = _webhook_to_record(webhook)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                INSERT INTO tamoss_webhooks (id, status, tags, record, updated_at)
                VALUES (%(id)s, %(status)s, %(tags)s, %(record)s, NOW())
                ON CONFLICT (id) DO UPDATE SET
                    status = EXCLUDED.status,
                    tags = EXCLUDED.tags,
                    record = EXCLUDED.record,
                    updated_at = NOW()
                """,
                {
                    "id": webhook.id,
                    "status": webhook.status,
                    "tags": Jsonb(webhook.tags),
                    "record": Jsonb(record),
                },
            )

    def delete_webhook(self, webhook_id: UUID) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute("DELETE FROM tamoss_webhooks WHERE id = %s", (webhook_id,))

    def list_webhook_deliveries(self) -> list[WebhookDeliveryRecord]:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT
                    record,
                    status,
                    next_attempt_at,
                    claimed_at,
                    claimed_by,
                    claim_expires_at
                FROM tamoss_webhook_deliveries
                ORDER BY created_at, id
                """
            )
            return [_webhook_delivery_from_row(row) for row in cur.fetchall()]

    def get_webhook_delivery(self, delivery_id: UUID) -> WebhookDeliveryRecord | None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT
                    record,
                    status,
                    next_attempt_at,
                    claimed_at,
                    claimed_by,
                    claim_expires_at
                FROM tamoss_webhook_deliveries
                WHERE id = %s
                """,
                (delivery_id,),
            )
            row = cur.fetchone()
            return _webhook_delivery_from_row(row) if row else None

    def save_webhook_delivery(self, delivery: WebhookDeliveryRecord) -> None:
        record = _webhook_delivery_to_record(delivery)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                INSERT INTO tamoss_webhook_deliveries (
                    id,
                    webhook_id,
                    status,
                    next_attempt_at,
                    claimed_at,
                    claimed_by,
                    claim_expires_at,
                    record,
                    updated_at
                )
                VALUES (
                    %(id)s,
                    %(webhook_id)s,
                    %(status)s,
                    %(next_attempt_at)s,
                    %(claimed_at)s,
                    %(claimed_by)s,
                    %(claim_expires_at)s,
                    %(record)s,
                    NOW()
                )
                ON CONFLICT (id) DO UPDATE SET
                    webhook_id = EXCLUDED.webhook_id,
                    status = EXCLUDED.status,
                    next_attempt_at = EXCLUDED.next_attempt_at,
                    claimed_at = EXCLUDED.claimed_at,
                    claimed_by = EXCLUDED.claimed_by,
                    claim_expires_at = EXCLUDED.claim_expires_at,
                    record = EXCLUDED.record,
                    updated_at = NOW()
                """,
                {
                    "id": delivery.id,
                    "webhook_id": delivery.webhook_id,
                    "status": delivery.status,
                    "next_attempt_at": delivery.next_attempt_at,
                    "claimed_at": delivery.claimed_at,
                    "claimed_by": delivery.claimed_by,
                    "claim_expires_at": delivery.claim_expires_at,
                    "record": Jsonb(record),
                },
            )

    def claim_webhook_deliveries(
        self, *, worker_id: str, limit: int, lease_seconds: int
    ) -> list[WebhookDeliveryRecord]:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                WITH candidates AS (
                    SELECT id
                    FROM tamoss_webhook_deliveries
                    WHERE status IN ('pending', 'started')
                      AND (
                          next_attempt_at IS NULL
                          OR next_attempt_at <= NOW()
                      )
                      AND (
                          claim_expires_at IS NULL
                          OR claim_expires_at <= NOW()
                      )
                    ORDER BY created_at, id
                    LIMIT %(limit)s
                    FOR UPDATE SKIP LOCKED
                )
                UPDATE tamoss_webhook_deliveries AS delivery
                SET status = 'started',
                    claimed_at = NOW(),
                    claimed_by = %(worker_id)s,
                    claim_expires_at = NOW()
                        + (%(lease_seconds)s * INTERVAL '1 second'),
                    updated_at = NOW()
                FROM candidates
                WHERE delivery.id = candidates.id
                RETURNING
                    delivery.record,
                    delivery.status,
                    delivery.next_attempt_at,
                    delivery.claimed_at,
                    delivery.claimed_by,
                    delivery.claim_expires_at
                """,
                {
                    "worker_id": worker_id,
                    "limit": limit,
                    "lease_seconds": lease_seconds,
                },
            )
            return [_webhook_delivery_from_row(row) for row in cur.fetchall()]

    def list_delete_requests(self) -> list[DeletionRequestRecord]:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT
                    record,
                    status,
                    updated,
                    claimed_at,
                    claimed_by,
                    claim_expires_at
                FROM tamoss_delete_requests
                ORDER BY created_at, id
                """
            )
            return [_delete_request_from_row(row) for row in cur.fetchall()]

    def get_delete_request(self, request_id: UUID) -> DeletionRequestRecord | None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT
                    record,
                    status,
                    updated,
                    claimed_at,
                    claimed_by,
                    claim_expires_at
                FROM tamoss_delete_requests
                WHERE id = %s
                """,
                (request_id,),
            )
            row = cur.fetchone()
            return _delete_request_from_row(row) if row else None

    def save_delete_request(self, request: DeletionRequestRecord) -> None:
        record = _delete_request_to_record(request)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                INSERT INTO tamoss_delete_requests (
                    id,
                    flow_id,
                    status,
                    claimed_at,
                    claimed_by,
                    claim_expires_at,
                    record,
                    updated
                )
                VALUES (
                    %(id)s,
                    %(flow_id)s,
                    %(status)s,
                    %(claimed_at)s,
                    %(claimed_by)s,
                    %(claim_expires_at)s,
                    %(record)s,
                    %(updated)s
                )
                ON CONFLICT (id) DO UPDATE SET
                    flow_id = EXCLUDED.flow_id,
                    status = EXCLUDED.status,
                    claimed_at = EXCLUDED.claimed_at,
                    claimed_by = EXCLUDED.claimed_by,
                    claim_expires_at = EXCLUDED.claim_expires_at,
                    record = EXCLUDED.record,
                    updated = EXCLUDED.updated,
                    updated_at = NOW()
                """,
                {
                    "id": request.id,
                    "flow_id": request.flow_id,
                    "status": request.status,
                    "claimed_at": request.claimed_at,
                    "claimed_by": request.claimed_by,
                    "claim_expires_at": request.claim_expires_at,
                    "record": Jsonb(record),
                    "updated": request.updated,
                },
            )

    def claim_delete_requests(
        self, *, worker_id: str, limit: int, lease_seconds: int
    ) -> list[DeletionRequestRecord]:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                WITH candidates AS (
                    SELECT id
                    FROM tamoss_delete_requests
                    WHERE status IN ('created', 'started', 'error')
                      AND (
                          claim_expires_at IS NULL
                          OR claim_expires_at <= NOW()
                      )
                    ORDER BY created_at, id
                    LIMIT %(limit)s
                    FOR UPDATE SKIP LOCKED
                )
                UPDATE tamoss_delete_requests AS request
                SET status = 'started',
                    claimed_at = NOW(),
                    claimed_by = %(worker_id)s,
                    claim_expires_at = NOW()
                        + (%(lease_seconds)s * INTERVAL '1 second'),
                    updated = NOW(),
                    updated_at = NOW()
                FROM candidates
                WHERE request.id = candidates.id
                RETURNING
                    request.record,
                    request.status,
                    request.updated,
                    request.claimed_at,
                    request.claimed_by,
                    request.claim_expires_at
                """,
                {
                    "worker_id": worker_id,
                    "limit": limit,
                    "lease_seconds": lease_seconds,
                },
            )
            return [_delete_request_from_row(row) for row in cur.fetchall()]

    def _ensure_schema(self) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(_TAMOSS_SCHEMA_SQL)

    def _upsert_configured_storage_backend(self, backend: StorageBackend) -> None:
        record = _storage_backend_to_record(backend)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                "DELETE FROM tamoss_storage_backends WHERE id <> %s",
                (backend.id,),
            )
            cur.execute(
                """
                INSERT INTO tamoss_storage_backends (
                    id,
                    label,
                    provider,
                    region,
                    store_product,
                    store_type,
                    default_storage,
                    bucket_name,
                    endpoint_url,
                    public_endpoint_url,
                    record,
                    updated_at
                )
                VALUES (
                    %(id)s,
                    %(label)s,
                    %(provider)s,
                    %(region)s,
                    %(store_product)s,
                    %(store_type)s,
                    TRUE,
                    %(bucket_name)s,
                    %(endpoint_url)s,
                    %(public_endpoint_url)s,
                    %(record)s,
                    NOW()
                )
                ON CONFLICT (id) DO UPDATE SET
                    label = EXCLUDED.label,
                    provider = EXCLUDED.provider,
                    region = EXCLUDED.region,
                    store_product = EXCLUDED.store_product,
                    store_type = EXCLUDED.store_type,
                    default_storage = TRUE,
                    bucket_name = EXCLUDED.bucket_name,
                    endpoint_url = EXCLUDED.endpoint_url,
                    public_endpoint_url = EXCLUDED.public_endpoint_url,
                    record = EXCLUDED.record,
                    updated_at = NOW()
                """,
                {
                    "id": backend.id,
                    "label": backend.label,
                    "provider": backend.provider,
                    "region": backend.region,
                    "store_product": backend.store_product,
                    "store_type": backend.store_type,
                    "bucket_name": backend.bucket_name,
                    "endpoint_url": backend.endpoint_url,
                    "public_endpoint_url": backend.public_endpoint_url,
                    "record": Jsonb(record),
                },
            )

    @contextmanager
    def _connect(self) -> Iterator[Any]:
        active_connection = self._transaction_connection.get()
        if active_connection is not None:
            yield active_connection
            return
        if self._connection is not None:
            yield self._connection
            return
        if self._pool is not None:
            with self._pool.connection() as conn:
                yield conn
            return
        assert self._database_url is not None
        conn = psycopg.connect(self._database_url)
        try:
            yield conn
            conn.commit()
        except Exception:
            conn.rollback()
            raise
        finally:
            conn.close()

    def close(self) -> None:
        if self._owns_pool and self._pool is not None:
            self._pool.close()

    def _storage_backend_from_record(self, record: dict[str, Any]) -> StorageBackend:
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
        )
        configured = self._configured_storage_backend
        if configured.id == backend.id:
            backend.endpoint_url = configured.endpoint_url or backend.endpoint_url
            backend.public_endpoint_url = (
                configured.public_endpoint_url or backend.public_endpoint_url
            )
            backend.access_key = configured.access_key
            backend.secret_key = configured.secret_key
        return backend

    def _flow_from_record(self, record: dict[str, Any]) -> FlowRecord:
        return FlowRecord(
            id=UUID(record["id"]),
            data=dict(record.get("data") or {}),
            source_id=_optional_uuid(record.get("source_id")),
            format=record.get("format"),
            container=record.get("container"),
            read_only=bool(record.get("read_only", False)),
            tags=dict(record.get("tags") or {}),
            created=_datetime_from_record(record.get("created")),
            metadata_updated=_datetime_from_record(record.get("metadata_updated")),
            segments_updated=_optional_datetime_from_record(
                record.get("segments_updated")
            ),
        )

    def _media_object_from_record(self, record: dict[str, Any]) -> MediaObjectRecord:
        return MediaObjectRecord(
            id=record["id"],
            timerange=record.get("timerange"),
            first_referenced_by_flow=_optional_uuid(
                record.get("first_referenced_by_flow")
            ),
            referenced_by_flows={
                UUID(flow_id) for flow_id in record.get("referenced_by_flows", [])
            },
            instances=[
                self._object_instance_from_record(item)
                for item in record.get("instances", [])
                if isinstance(item, dict)
            ],
            key_frame_count=record.get("key_frame_count"),
            bytes_written=int(record.get("bytes_written") or 0),
        )

    def _object_instance_from_record(self, record: dict[str, Any]) -> ObjectInstance:
        storage_backend_id = _optional_uuid(record.get("storage_backend_id"))
        storage_backend = (
            self.get_storage_backend(storage_backend_id)
            if storage_backend_id is not None
            else None
        )
        return ObjectInstance(
            storage_backend=storage_backend,
            url=record.get("url"),
            label=record.get("label"),
            controlled=bool(record.get("controlled", False)),
            presigned=bool(record.get("presigned", False)),
        )


def _where_sql(clauses: list[Any]) -> Any:
    if not clauses:
        return sql.SQL("")
    return sql.SQL("WHERE ") + sql.SQL(" AND ").join(clauses)


def _append_tag_filter_clauses(
    clauses: list[Any],
    params: dict[str, Any],
    *,
    column_sql: str,
    value_filters: dict[str, set[str]],
    existence_filters: dict[str, bool],
) -> None:
    for index, (tag_name, expected_values) in enumerate(sorted(value_filters.items())):
        if not expected_values:
            clauses.append(sql.SQL("FALSE"))
            continue
        value_clauses: list[Any] = []
        for value_index, expected_value in enumerate(sorted(expected_values)):
            scalar_param = f"tag_{index}_{value_index}_scalar"
            array_param = f"tag_{index}_{value_index}_array"
            params[scalar_param] = Jsonb({tag_name: expected_value})
            params[array_param] = Jsonb({tag_name: [expected_value]})
            value_clauses.append(
                sql.SQL(
                    f"({column_sql} @> %({scalar_param})s "
                    f"OR {column_sql} @> %({array_param})s)"
                )
            )
        clauses.append(
            sql.SQL("(") + sql.SQL(" OR ").join(value_clauses) + sql.SQL(")")
        )

    for index, (tag_name, should_exist) in enumerate(sorted(existence_filters.items())):
        param_name = f"tag_exists_{index}"
        params[param_name] = tag_name
        operator = sql.SQL("? %({})s").format(sql.SQL(param_name))
        if should_exist:
            clauses.append(sql.SQL(column_sql) + sql.SQL(" ") + operator)
        else:
            clauses.append(
                sql.SQL("NOT (")
                + sql.SQL(column_sql)
                + sql.SQL(" ")
                + operator
                + sql.SQL(")")
            )


def _append_flow_timerange_filter(
    clauses: list[Any],
    params: dict[str, Any],
    *,
    timerange_start: int | None,
    timerange_end: int | None,
    timerange_is_empty: bool,
    timerange_is_point: bool,
) -> None:
    if timerange_is_empty:
        clauses.append(
            sql.SQL(
                """
                NOT EXISTS (
                    SELECT 1
                    FROM tamoss_segments AS segment
                    WHERE segment.flow_id = flow.id
                )
                """
            )
        )
        return
    if timerange_start is None and timerange_end is None:
        return

    if timerange_is_point and timerange_start is not None:
        params["flow_timerange_start"] = timerange_start
        having_sql = sql.SQL(
            """
            (
                MIN(segment.timerange_start) = MAX(segment.timerange_end)
                AND MIN(segment.timerange_start) = %(flow_timerange_start)s
            )
            OR (
                MIN(segment.timerange_start) < MAX(segment.timerange_end)
                AND MIN(segment.timerange_start) <= %(flow_timerange_start)s
                AND MAX(segment.timerange_end) > %(flow_timerange_start)s
            )
            """
        )
    else:
        having: list[Any] = []
        if timerange_end is not None:
            params["flow_timerange_end"] = timerange_end
            having.append(
                sql.SQL("MIN(segment.timerange_start) < %(flow_timerange_end)s")
            )
        if timerange_start is not None:
            params["flow_timerange_start"] = timerange_start
            having.append(
                sql.SQL(
                    """
                    (
                        (
                            MIN(segment.timerange_start) < MAX(segment.timerange_end)
                            AND MAX(segment.timerange_end) > %(flow_timerange_start)s
                        )
                        OR (
                            MIN(segment.timerange_start) = MAX(segment.timerange_end)
                            AND MIN(segment.timerange_start) >= %(flow_timerange_start)s
                        )
                    )
                    """
                )
            )
        having_sql = sql.SQL(" AND ").join(having)

    clauses.append(
        sql.SQL(
            """
            EXISTS (
                SELECT 1
                FROM tamoss_segments AS segment
                WHERE segment.flow_id = flow.id
                GROUP BY segment.flow_id
                HAVING {}
            )
            """
        ).format(having_sql)
    )


def _flows_with_collected_by(cur: Any, flows: list[FlowRecord]) -> list[FlowRecord]:
    if not flows:
        return flows
    flow_ids = [flow.id for flow in flows]
    cur.execute(
        """
        SELECT child.id AS child_id, parent.id::text AS parent_id
        FROM tamoss_flows AS parent
        CROSS JOIN LATERAL jsonb_array_elements(
            CASE
                WHEN jsonb_typeof(parent.record->'data'->'flow_collection') = 'array'
                THEN parent.record->'data'->'flow_collection'
                ELSE '[]'::jsonb
            END
        ) AS item(value)
        JOIN tamoss_flows AS child
          ON child.id::text = item.value->>'id'
        WHERE child.id = ANY(%(flow_ids)s::uuid[])
        ORDER BY child.id, parent.id
        """,
        {"flow_ids": flow_ids},
    )
    collected_by: dict[UUID, list[str]] = {}
    for child_id, parent_id in cur.fetchall():
        parent_ids = collected_by.setdefault(child_id, [])
        if parent_id not in parent_ids:
            parent_ids.append(parent_id)

    results: list[FlowRecord] = []
    for flow in flows:
        data = dict(flow.data)
        parent_ids = collected_by.get(flow.id, [])
        if parent_ids:
            data["collected_by"] = parent_ids
        else:
            data.pop("collected_by", None)
        results.append(replace(flow, data=data))
    return results


def _save_flow(cur: Any, flow: FlowRecord) -> None:
    record = _flow_to_record(flow)
    cur.execute(
        """
        INSERT INTO tamoss_flows (
            id,
            source_id,
            format,
            container,
            read_only,
            tags,
            record,
            metadata_updated,
            segments_updated,
            updated_at
        )
        VALUES (
            %(id)s,
            %(source_id)s,
            %(format)s,
            %(container)s,
            %(read_only)s,
            %(tags)s,
            %(record)s,
            %(metadata_updated)s,
            %(segments_updated)s,
            NOW()
        )
        ON CONFLICT (id) DO UPDATE SET
            source_id = EXCLUDED.source_id,
            format = EXCLUDED.format,
            container = EXCLUDED.container,
            read_only = EXCLUDED.read_only,
            tags = EXCLUDED.tags,
            record = EXCLUDED.record,
            metadata_updated = EXCLUDED.metadata_updated,
            segments_updated = EXCLUDED.segments_updated,
            updated_at = NOW()
        """,
        {
            "id": flow.id,
            "source_id": flow.source_id,
            "format": flow.format,
            "container": flow.container,
            "read_only": flow.read_only,
            "tags": Jsonb(flow.tags),
            "record": Jsonb(record),
            "metadata_updated": flow.metadata_updated,
            "segments_updated": flow.segments_updated,
        },
    )


def _save_object(cur: Any, media_object: MediaObjectRecord) -> None:
    record = _media_object_to_record(media_object)
    cur.execute(
        """
        INSERT INTO tamoss_media_objects (
            id,
            first_referenced_by_flow,
            referenced_by_flows,
            record,
            updated_at
        )
        VALUES (
            %(id)s,
            %(first_referenced_by_flow)s,
            %(referenced_by_flows)s,
            %(record)s,
            NOW()
        )
        ON CONFLICT (id) DO UPDATE SET
            first_referenced_by_flow = EXCLUDED.first_referenced_by_flow,
            referenced_by_flows = EXCLUDED.referenced_by_flows,
            record = EXCLUDED.record,
            updated_at = NOW()
        """,
        {
            "id": media_object.id,
            "first_referenced_by_flow": media_object.first_referenced_by_flow,
            "referenced_by_flows": [
                str(flow_id) for flow_id in media_object.referenced_by_flows
            ],
            "record": Jsonb(record),
        },
    )


def _create_object(cur: Any, media_object: MediaObjectRecord) -> bool:
    record = _media_object_to_record(media_object)
    cur.execute(
        """
        INSERT INTO tamoss_media_objects (
            id,
            first_referenced_by_flow,
            referenced_by_flows,
            record,
            updated_at
        )
        VALUES (
            %(id)s,
            %(first_referenced_by_flow)s,
            %(referenced_by_flows)s,
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
            "record": Jsonb(record),
        },
    )
    return cur.fetchone() is not None


def _lock_flow_segments(cur: Any, flow_id: UUID) -> None:
    cur.execute(
        "SELECT pg_advisory_xact_lock(hashtextextended(%s, 0))",
        (f"tamoss_segments:{flow_id}",),
    )


def _append_segment(
    cur: Any, segment: SegmentRecord, *, reject_overlaps: bool = False
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
            timerange,
            timerange_start,
            timerange_end,
            record,
            created
        )
        VALUES (
            %(flow_id)s,
            %(object_id)s,
            %(timerange)s,
            %(timerange_start)s,
            %(timerange_end)s,
            %(record)s,
            %(created)s
        )
        ON CONFLICT (flow_id, object_id, timerange) DO UPDATE SET
            timerange_start = EXCLUDED.timerange_start,
            timerange_end = EXCLUDED.timerange_end,
            record = EXCLUDED.record,
            updated_at = NOW()
        """,
        {
            "flow_id": segment.flow_id,
            "object_id": segment.object_id,
            "timerange": segment.timerange,
            "timerange_start": timerange_start,
            "timerange_end": timerange_end,
            "record": Jsonb(record),
            "created": segment.created,
        },
    )


def _raise_if_segment_overlaps(
    cur: Any, *, flow_id: UUID, timerange_start: int, timerange_end: int
) -> None:
    if timerange_start == timerange_end:
        cur.execute(
            """
            SELECT 1
            FROM tamoss_segments
            WHERE flow_id = %(flow_id)s
              AND (
                  (
                      timerange_start < timerange_end
                      AND timerange_start <= %(timerange_start)s
                      AND timerange_end > %(timerange_start)s
                  )
                  OR (
                      timerange_start = timerange_end
                      AND timerange_start = %(timerange_start)s
                  )
              )
            LIMIT 1
            """,
            {
                "flow_id": flow_id,
                "timerange_start": timerange_start,
            },
        )
    else:
        cur.execute(
            """
            SELECT 1
            FROM tamoss_segments
            WHERE flow_id = %(flow_id)s
              AND timerange_start < %(timerange_end)s
              AND (
                  (
                      timerange_start < timerange_end
                      AND timerange_end > %(timerange_start)s
                  )
                  OR (
                      timerange_start = timerange_end
                      AND timerange_start >= %(timerange_start)s
                  )
              )
            LIMIT 1
            """,
            {
                "flow_id": flow_id,
                "timerange_start": timerange_start,
                "timerange_end": timerange_end,
            },
        )
    if cur.fetchone() is not None:
        raise ValueError("Segment timerange overlaps with an existing segment")


def _storage_backend_to_record(backend: StorageBackend) -> dict[str, Any]:
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
    }


def _source_to_record(source: SourceRecord) -> dict[str, Any]:
    return {
        "id": str(source.id),
        "format": source.format,
        "label": source.label,
        "description": source.description,
        "tags": source.tags,
        "created": source.created.isoformat(),
        "metadata_updated": source.metadata_updated.isoformat(),
    }


def _source_from_record(record: dict[str, Any]) -> SourceRecord:
    return SourceRecord(
        id=UUID(record["id"]),
        format=record.get("format"),
        label=record.get("label"),
        description=record.get("description"),
        tags=dict(record.get("tags") or {}),
        created=_datetime_from_record(record.get("created")),
        metadata_updated=_datetime_from_record(record.get("metadata_updated")),
    )


def _flow_to_record(flow: FlowRecord) -> dict[str, Any]:
    return {
        "id": str(flow.id),
        "data": flow.data,
        "source_id": str(flow.source_id) if flow.source_id else None,
        "format": flow.format,
        "container": flow.container,
        "read_only": flow.read_only,
        "tags": flow.tags,
        "created": flow.created.isoformat(),
        "metadata_updated": flow.metadata_updated.isoformat(),
        "segments_updated": flow.segments_updated.isoformat()
        if flow.segments_updated
        else None,
    }


def _media_object_to_record(media_object: MediaObjectRecord) -> dict[str, Any]:
    return {
        "id": media_object.id,
        "timerange": media_object.timerange,
        "first_referenced_by_flow": str(media_object.first_referenced_by_flow)
        if media_object.first_referenced_by_flow
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
    }


def _object_instance_to_record(instance: ObjectInstance) -> dict[str, Any]:
    return {
        "storage_backend_id": str(instance.storage_backend.id)
        if instance.storage_backend is not None
        else None,
        "url": instance.url,
        "label": instance.label,
        "controlled": instance.controlled,
        "presigned": instance.presigned,
    }


def _segment_to_record(segment: SegmentRecord) -> dict[str, Any]:
    return {
        "flow_id": str(segment.flow_id),
        "object_id": segment.object_id,
        "timerange": segment.timerange,
        "ts_offset": segment.ts_offset,
        "last_duration": segment.last_duration,
        "object_timerange": segment.object_timerange,
        "sample_offset": segment.sample_offset,
        "sample_count": segment.sample_count,
        "get_urls": segment.get_urls,
        "key_frame_count": segment.key_frame_count,
        "created": segment.created.isoformat(),
    }


def _segment_from_record(record: dict[str, Any]) -> SegmentRecord:
    return SegmentRecord(
        flow_id=UUID(record["flow_id"]),
        object_id=record["object_id"],
        timerange=record["timerange"],
        ts_offset=record.get("ts_offset"),
        last_duration=record.get("last_duration"),
        object_timerange=record.get("object_timerange"),
        sample_offset=record.get("sample_offset"),
        sample_count=record.get("sample_count"),
        get_urls=list(record.get("get_urls") or []),
        key_frame_count=record.get("key_frame_count"),
        created=_datetime_from_record(record.get("created")),
    )


def _webhook_to_record(webhook: WebhookRecord) -> dict[str, Any]:
    data = _normalized_webhook_data(webhook.data)
    return {
        "id": str(webhook.id),
        "data": data,
        "status": webhook.status,
        "tags": webhook.tags,
    }


def _webhook_from_record(record: dict[str, Any]) -> WebhookRecord:
    return WebhookRecord(
        id=UUID(record["id"]),
        data=_normalized_webhook_data(dict(record.get("data") or {})),
        status=record["status"],
        tags=dict(record.get("tags") or {}),
    )


def _webhook_delivery_to_record(delivery: WebhookDeliveryRecord) -> dict[str, Any]:
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


def _webhook_delivery_from_row(row: Any) -> WebhookDeliveryRecord:
    delivery = _webhook_delivery_from_record(row[0])
    delivery.status = row[1]
    delivery.next_attempt_at = _optional_datetime_from_record(row[2])
    delivery.claimed_at = _optional_datetime_from_record(row[3])
    delivery.claimed_by = row[4]
    delivery.claim_expires_at = _optional_datetime_from_record(row[5])
    return delivery


def _webhook_delivery_from_record(record: dict[str, Any]) -> WebhookDeliveryRecord:
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
        error=normalize_error_payload(record.get("error")),
    )


def _delete_request_to_record(request: DeletionRequestRecord) -> dict[str, Any]:
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
        "segments_to_delete": [
            _segment_to_record(segment) for segment in request.segments_to_delete
        ],
    }


def _delete_request_from_row(row: Any) -> DeletionRequestRecord:
    request = _delete_request_from_record(row[0])
    request.status = row[1]
    request.updated = _datetime_from_record(row[2])
    request.claimed_at = _optional_datetime_from_record(row[3])
    request.claimed_by = row[4]
    request.claim_expires_at = _optional_datetime_from_record(row[5])
    return request


def _delete_request_from_record(record: dict[str, Any]) -> DeletionRequestRecord:
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
        error=normalize_error_payload(record.get("error")),
        segments_to_delete=[
            _segment_from_record(segment)
            for segment in record.get("segments_to_delete", [])
            if isinstance(segment, dict)
        ],
    )


def _normalized_webhook_data(data: dict[str, Any]) -> dict[str, Any]:
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
    if parsed.start is None or parsed.end is None:
        raise ValueError("Segment timerange must have finite start and end bounds.")
    if parsed.is_empty():
        raise ValueError("Segment timerange must not be empty.")
    return int(parsed.start.to_nanosec()), int(parsed.end.to_nanosec())


def _timerange_from_bounds(start: int | None, end: int | None) -> str:
    if start is None or end is None:
        return "()"
    start_ts = Timestamp.from_nanosec(start)
    end_ts = Timestamp.from_nanosec(end)
    if start == end:
        return f"[{start_ts}]"
    return f"[{start_ts}_{end_ts})"


_TAMOSS_SCHEMA_SQL = """
CREATE TABLE IF NOT EXISTS tamoss_service_metadata (
    id TEXT PRIMARY KEY,
    name TEXT,
    description TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tamoss_storage_backends (
    id UUID PRIMARY KEY,
    label TEXT NOT NULL,
    provider TEXT NOT NULL,
    region TEXT NOT NULL,
    store_product TEXT NOT NULL,
    store_type TEXT NOT NULL DEFAULT 'http_object_store',
    default_storage BOOLEAN NOT NULL DEFAULT FALSE,
    bucket_name TEXT,
    endpoint_url TEXT,
    public_endpoint_url TEXT,
    record JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tamoss_storage_backends_one_default
ON tamoss_storage_backends(default_storage)
WHERE default_storage IS TRUE;

CREATE TABLE IF NOT EXISTS tamoss_sources (
    id UUID PRIMARY KEY,
    format TEXT,
    label TEXT,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    record JSONB NOT NULL,
    metadata_updated TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tamoss_sources_format ON tamoss_sources(format);
CREATE INDEX IF NOT EXISTS idx_tamoss_sources_tags ON tamoss_sources USING GIN(tags);

CREATE TABLE IF NOT EXISTS tamoss_flows (
    id UUID PRIMARY KEY,
    source_id UUID,
    format TEXT,
    container TEXT,
    read_only BOOLEAN NOT NULL DEFAULT FALSE,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    record JSONB NOT NULL,
    metadata_updated TIMESTAMPTZ NOT NULL,
    segments_updated TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tamoss_flows_source_id ON tamoss_flows(source_id);
CREATE INDEX IF NOT EXISTS idx_tamoss_flows_format ON tamoss_flows(format);
CREATE INDEX IF NOT EXISTS idx_tamoss_flows_tags ON tamoss_flows USING GIN(tags);

CREATE TABLE IF NOT EXISTS tamoss_media_objects (
    id TEXT PRIMARY KEY,
    first_referenced_by_flow UUID,
    referenced_by_flows TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    record JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tamoss_media_objects_referenced_by_flows
ON tamoss_media_objects USING GIN(referenced_by_flows);

CREATE TABLE IF NOT EXISTS tamoss_segments (
    flow_id UUID NOT NULL,
    object_id TEXT NOT NULL,
    timerange TEXT NOT NULL,
    timerange_start BIGINT NOT NULL,
    timerange_end BIGINT NOT NULL,
    record JSONB NOT NULL,
    created TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_tamoss_segments_timerange_bounds
        CHECK (timerange_start <= timerange_end),
    PRIMARY KEY (flow_id, object_id, timerange),
    FOREIGN KEY (flow_id) REFERENCES tamoss_flows(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tamoss_segments_flow_timerange
ON tamoss_segments(flow_id, timerange_end, timerange_start);
CREATE INDEX IF NOT EXISTS idx_tamoss_segments_flow_object_timerange
ON tamoss_segments(flow_id, object_id, timerange_end, timerange_start);
CREATE INDEX IF NOT EXISTS idx_tamoss_segments_object_id
ON tamoss_segments(object_id);

CREATE TABLE IF NOT EXISTS tamoss_webhooks (
    id UUID PRIMARY KEY,
    status TEXT NOT NULL,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    record JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tamoss_webhooks_status ON tamoss_webhooks(status);
CREATE INDEX IF NOT EXISTS idx_tamoss_webhooks_tags ON tamoss_webhooks USING GIN(tags);

CREATE TABLE IF NOT EXISTS tamoss_webhook_deliveries (
    id UUID PRIMARY KEY,
    webhook_id UUID NOT NULL,
    status TEXT NOT NULL,
    next_attempt_at TIMESTAMPTZ,
    claimed_at TIMESTAMPTZ,
    claimed_by TEXT,
    claim_expires_at TIMESTAMPTZ,
    record JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE tamoss_webhook_deliveries
ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;
ALTER TABLE tamoss_webhook_deliveries
ADD COLUMN IF NOT EXISTS claimed_by TEXT;
ALTER TABLE tamoss_webhook_deliveries
ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_tamoss_webhook_deliveries_webhook_id
ON tamoss_webhook_deliveries(webhook_id);
CREATE INDEX IF NOT EXISTS idx_tamoss_webhook_deliveries_status
ON tamoss_webhook_deliveries(status, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_tamoss_webhook_deliveries_claim
ON tamoss_webhook_deliveries(status, next_attempt_at, claim_expires_at);

CREATE TABLE IF NOT EXISTS tamoss_delete_requests (
    id UUID PRIMARY KEY,
    flow_id UUID NOT NULL,
    status TEXT NOT NULL,
    claimed_at TIMESTAMPTZ,
    claimed_by TEXT,
    claim_expires_at TIMESTAMPTZ,
    record JSONB NOT NULL,
    updated TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE tamoss_delete_requests
ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;
ALTER TABLE tamoss_delete_requests
ADD COLUMN IF NOT EXISTS claimed_by TEXT;
ALTER TABLE tamoss_delete_requests
ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_tamoss_delete_requests_flow_id
ON tamoss_delete_requests(flow_id);
CREATE INDEX IF NOT EXISTS idx_tamoss_delete_requests_status
ON tamoss_delete_requests(status);
CREATE INDEX IF NOT EXISTS idx_tamoss_delete_requests_claim
ON tamoss_delete_requests(status, claim_expires_at);
"""
