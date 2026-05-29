from __future__ import annotations

# mypy: disable-error-code=attr-defined
# Focused store methods run with repository-owned connection and mapper state.
from collections.abc import Iterable
from datetime import datetime
from typing import Any
from uuid import UUID

from psycopg import sql

from tamoss.adapters.postgres_repository.mappers import (
    _append_segment,
    _create_object,
    _lock_flow_segments,
    _media_object_from_record,
    _optional_uuid,
    _save_flow,
    _save_object,
    _segment_from_record,
    _storage_backend_from_record,
    _timerange_from_bounds,
)
from tamoss.adapters.postgres_repository.types import (
    MediaObjectRow,
    PostgresCursor,
)
from tamoss.domain.model import (
    FlowRecord,
    MediaObjectRecord,
    SegmentRecord,
    StorageBackend,
)
from tamoss.domain.pagination import Page, resolve_page_window
from tamoss.domain.segments import SegmentDeleteFilter, SegmentTimerangeBounds


class PostgresObjectSegmentMixin:
    def get_object(self, object_id: str) -> MediaObjectRecord | None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                "SELECT record FROM tamoss_media_objects WHERE id = %s",
                (object_id,),
            )
            row = cur.fetchone()
            if row is None:
                return None
            record = row[0]
            storage_backends = _storage_backends_for_object_records(self, cur, [record])
            return _media_object_from_record(
                record,
                storage_backends_by_id=storage_backends,
            )

    def get_objects(self, object_ids: Iterable[str]) -> dict[str, MediaObjectRecord]:
        requested_ids = list(dict.fromkeys(object_ids))
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
            rows = cur.fetchall()
            records = [row[1] for row in rows]
            storage_backends = _storage_backends_for_object_records(
                self,
                cur,
                records,
            )
            return {
                row[0]: _media_object_from_record(
                    row[1],
                    storage_backends_by_id=storage_backends,
                )
                for row in rows
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

    def list_unreferenced_objects_created_before(
        self,
        *,
        before: datetime,
        limit: int,
    ) -> list[MediaObjectRecord]:
        if limit < 1:
            return []
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT record
                FROM tamoss_media_objects
                WHERE cardinality(referenced_by_flows) = 0
                  AND created_at < %(before)s
                ORDER BY created_at, id
                LIMIT %(limit)s
                FOR UPDATE SKIP LOCKED
                """,
                {"before": before, "limit": limit},
            )
            records = [row[0] for row in cur.fetchall()]
            storage_backends = _storage_backends_for_object_records(
                self,
                cur,
                records,
            )
            return [
                _media_object_from_record(
                    record,
                    storage_backends_by_id=storage_backends,
                )
                for record in records
            ]

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

    def list_segments_for_objects(
        self, *, flow_id: UUID, object_ids: Iterable[str]
    ) -> list[SegmentRecord]:
        requested_ids = list(dict.fromkeys(object_ids))
        if not requested_ids:
            return []
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT record
                FROM tamoss_segments
                WHERE flow_id = %(flow_id)s
                  AND object_id = ANY(%(object_ids)s)
                ORDER BY timerange_end, timerange_start, object_id
                """,
                {"flow_id": flow_id, "object_ids": requested_ids},
            )
            return [_segment_from_record(row[0]) for row in cur.fetchall()]

    def segment_delete_timerange(self, delete_filter: SegmentDeleteFilter) -> str:
        where_sql, params = _segment_delete_where_sql(delete_filter)
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
            row = cur.fetchone()
        return _timerange_from_bounds(row[0], row[1]) if row else "()"

    def delete_segment_batch(
        self, delete_filter: SegmentDeleteFilter, *, limit: int
    ) -> list[SegmentRecord]:
        if limit < 1 or delete_filter.timerange_is_empty:
            return []
        where_sql, params = _segment_delete_where_sql(delete_filter)
        params["limit"] = limit
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    """
                    WITH target AS (
                        SELECT flow_id, object_id, timerange
                        FROM tamoss_segments
                        WHERE {}
                        ORDER BY timerange_end, timerange_start, object_id
                        LIMIT %(limit)s
                        FOR UPDATE SKIP LOCKED
                    )
                    DELETE FROM tamoss_segments AS segment
                    USING target
                    WHERE segment.flow_id = target.flow_id
                      AND segment.object_id = target.object_id
                      AND segment.timerange = target.timerange
                    RETURNING segment.record
                    """
                ).format(where_sql),
                params,
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
                WITH candidate_bounds(timerange_start, timerange_end) AS (
                    SELECT *
                    FROM unnest(
                        %(starts)s::bigint[],
                        %(ends)s::bigint[]
                    )
                )
                    SELECT record
                    FROM tamoss_segments AS segment
                    WHERE segment.flow_id = %(flow_id)s
                      AND EXISTS (
                          SELECT 1
                          FROM candidate_bounds AS candidate
                          WHERE segment.timerange_start < candidate.timerange_end
                            AND segment.timerange_end > candidate.timerange_start
                      )
                    ORDER BY
                        segment.timerange_end,
                        segment.timerange_start,
                        segment.object_id
                """,
                {
                    "flow_id": flow_id,
                    "starts": [_query_start(timerange.start) for timerange in bounds],
                    "ends": [
                        _query_end(timerange.start, timerange.end, timerange.is_point)
                        for timerange in bounds
                    ],
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

        query_start = _query_start(timerange_start)
        query_end = _query_end(timerange_start, timerange_end, timerange_is_point)
        if query_end is not None:
            clauses.append("timerange_start < %(timerange_end)s")
            params["timerange_end"] = query_end
        if query_start is not None:
            clauses.append("timerange_end > %(timerange_start)s")
            params["timerange_start"] = query_start

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


def _query_start(start: int | None) -> int | None:
    return start


def _query_end(start: int | None, end: int | None, is_point: bool) -> int | None:
    if is_point and start is not None and end == start:
        return start + 1
    return end


def _segment_delete_where_sql(
    delete_filter: SegmentDeleteFilter,
) -> tuple[Any, dict[str, Any]]:
    clauses = ["flow_id = %(flow_id)s"]
    params: dict[str, Any] = {"flow_id": delete_filter.flow_id}
    if delete_filter.timerange_is_empty:
        clauses.append("FALSE")
    if delete_filter.object_id is not None:
        clauses.append("object_id = %(object_id)s")
        params["object_id"] = delete_filter.object_id
    if delete_filter.timerange_start is not None:
        clauses.append("timerange_start >= %(timerange_start)s")
        params["timerange_start"] = delete_filter.timerange_start
    if delete_filter.timerange_end is not None:
        clauses.append("timerange_end <= %(timerange_end)s")
        params["timerange_end"] = delete_filter.timerange_end
    return sql.SQL(" AND ").join(sql.SQL(clause) for clause in clauses), params


def _storage_backends_for_object_records(
    repository: PostgresObjectSegmentMixin,
    cur: PostgresCursor,
    records: Iterable[MediaObjectRow],
) -> dict[UUID, StorageBackend]:
    storage_backend_ids = {
        storage_backend_id
        for record in records
        for storage_backend_id in _object_storage_backend_ids(record)
    }
    if not storage_backend_ids:
        return {}
    cur.execute(
        """
        SELECT record
        FROM tamoss_storage_backends
        WHERE id = ANY(%(storage_backend_ids)s::uuid[])
        """,
        {"storage_backend_ids": list(storage_backend_ids)},
    )
    return {
        backend.id: backend
        for backend in (
            _storage_backend_from_record(
                row[0],
                configured_storage_backend=repository._configured_storage_backend,
            )
            for row in cur.fetchall()
        )
    }


def _object_storage_backend_ids(record: MediaObjectRow) -> set[UUID]:
    storage_backend_ids: set[UUID] = set()
    for instance in record.get("instances") or []:
        storage_backend_id = _optional_uuid(instance.get("storage_backend_id"))
        if storage_backend_id is not None:
            storage_backend_ids.add(storage_backend_id)
    return storage_backend_ids
