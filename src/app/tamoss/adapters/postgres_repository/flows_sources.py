from __future__ import annotations

# mypy: disable-error-code=attr-defined
# Focused store methods run with repository-owned connection and mapper state.
from collections.abc import Iterable
from typing import Any
from uuid import UUID

from psycopg import sql
from psycopg.types.json import Jsonb

from tamoss.adapters.postgres_repository.mappers import (
    _flow_from_record,
    _save_flow,
    _source_from_record,
    _source_to_record,
    _timerange_from_bounds,
)
from tamoss.adapters.postgres_repository.query_filters import (
    _append_flow_collected_by_filter,
    _append_flow_timerange_filter,
    _append_listing_cursor_filter,
    _append_source_collected_by_filter,
    _append_tag_filter_clauses,
    _flows_with_collected_by,
    _listing_order_sql,
    _where_sql,
)
from tamoss.domain.listing_pagination import listing_page, listing_window
from tamoss.domain.listings import FlowSortBy, SourceSortBy
from tamoss.domain.model import FlowRecord, SourceRecord, SourceRelationships
from tamoss.domain.pagination import Page, resolve_page_window


class PostgresFlowSourceMixin:
    def list_flows_by_source(self, source_id: UUID) -> list[FlowRecord]:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                "SELECT record FROM tamoss_flows WHERE source_id = %s ORDER BY id",
                (source_id,),
            )
            return [_flow_from_record(row[0]) for row in cur.fetchall()]

    def list_flows_collecting(self, flow_ids: Iterable[UUID]) -> list[FlowRecord]:
        requested_ids = list(dict.fromkeys(flow_ids))
        if not requested_ids:
            return []
        clause = sql.SQL(" OR ").join(
            sql.SQL("flow.flow_collection_ids @> ARRAY[%s]::uuid[]")
            for _ in requested_ids
        )
        params = requested_ids
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    """
                    SELECT flow.record
                    FROM tamoss_flows AS flow
                    WHERE {}
                    ORDER BY flow.id
                    """
                ).format(clause),
                params,
            )
            return [_flow_from_record(row[0]) for row in cur.fetchall()]

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
        profile_id: UUID | None,
        status: str | None,
        init_segments: bool | None,
        collected_by_ids: set[UUID] | None,
        top_level_only: bool,
        sort_by: FlowSortBy,
        reverse_order: bool,
        frame_width: int | None,
        frame_height: int | None,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[FlowRecord]:
        window = listing_window(
            page=page,
            limit=limit,
            resource="flows",
            sort_by=sort_by,
            reverse_order=reverse_order,
        )
        clauses: list[sql.Composable] = []
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
            clauses.append(sql.SQL("flow.label = %(label)s"))
            params["label"] = label
        if profile_id is not None:
            clauses.append(sql.SQL("flow.profile_id = %(profile_id)s"))
            params["profile_id"] = profile_id
        if status is not None:
            clauses.append(sql.SQL("flow.status = %(status)s"))
            params["status"] = status
        if init_segments is not None:
            clauses.append(sql.SQL("flow.init_segments = %(init_segments)s"))
            params["init_segments"] = init_segments
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
        _append_flow_collected_by_filter(
            clauses,
            params,
            collected_by_ids=collected_by_ids,
            top_level_only=top_level_only,
        )
        sort_expression = {
            FlowSortBy.CREATED: sql.SQL("flow.created"),
            FlowSortBy.METADATA_UPDATED: sql.SQL("flow.metadata_updated"),
            FlowSortBy.LABEL: sql.SQL("flow.label"),
        }[sort_by]
        _append_listing_cursor_filter(
            clauses,
            params,
            window,
            value_sql=sort_expression,
            identity_sql=sql.SQL("flow.id"),
            timestamp=sort_by != FlowSortBy.LABEL,
        )
        where_sql = _where_sql(clauses)
        descending = sort_by.descending(reverse_order=reverse_order)
        order_sql = _listing_order_sql(
            sort_expression,
            sql.SQL("flow.id"),
            descending=descending,
            missing_first=reverse_order,
        )
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    """
                    SELECT flow.record
                    FROM tamoss_flows AS flow
                    {}
                    ORDER BY {}
                    OFFSET %(offset)s
                    LIMIT %(limit)s
                    """
                ).format(where_sql, order_sql),
                params,
            )
            rows = cur.fetchall()
            flows = [_flow_from_record(row[0]) for row in rows]
            flows = _flows_with_collected_by(cur, flows)
        return listing_page(
            flows,
            window,
            value=lambda flow: {
                FlowSortBy.CREATED: flow.created,
                FlowSortBy.METADATA_UPDATED: flow.metadata_updated,
                FlowSortBy.LABEL: flow.data.get("label"),
            }[sort_by],
            identity=lambda flow: flow.id,
        )

    def flow_timeranges(self, flow_ids: Iterable[UUID]) -> dict[UUID, str]:
        requested_ids = list(dict.fromkeys(flow_ids))
        timeranges = dict.fromkeys(requested_ids, "()")
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
            return _flow_from_record(row[0]) if row else None

    def save_flow(self, flow: FlowRecord) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            _save_flow(cur, flow)

    def delete_flow(self, flow_id: UUID) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute("DELETE FROM tamoss_flows WHERE id = %s", (flow_id,))

    def lock_source(self, source_id: UUID) -> None:
        if self._transaction_connection.get() is None:
            raise RuntimeError("Source locks require a unit of work.")
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                "SELECT id FROM tamoss_sources WHERE id = %s FOR UPDATE", (source_id,)
            )

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
                    created,
                    updated_at
                )
                VALUES (
                    %(id)s,
                    %(format)s,
                    %(label)s,
                    %(tags)s,
                    %(record)s,
                    %(metadata_updated)s,
                    %(created)s,
                    NOW()
                )
                ON CONFLICT (id) DO UPDATE SET
                    format = EXCLUDED.format,
                    label = EXCLUDED.label,
                    tags = EXCLUDED.tags,
                    record = EXCLUDED.record,
                    metadata_updated = EXCLUDED.metadata_updated,
                    created = EXCLUDED.created,
                    updated_at = NOW()
                """,
                {
                    "id": source.id,
                    "format": source.format,
                    "label": source.label,
                    "tags": Jsonb(source.tags),
                    "record": Jsonb(record),
                    "metadata_updated": source.metadata_updated,
                    "created": source.created,
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
        collected_by_ids: set[UUID] | None,
        top_level_only: bool,
        sort_by: SourceSortBy,
        reverse_order: bool,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[SourceRecord]:
        window = listing_window(
            page=page,
            limit=limit,
            resource="sources",
            sort_by=sort_by,
            reverse_order=reverse_order,
        )
        clauses: list[sql.Composable] = []
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
        _append_source_collected_by_filter(
            clauses,
            params,
            collected_by_ids=collected_by_ids,
            top_level_only=top_level_only,
        )
        sort_expression = {
            SourceSortBy.CREATED: sql.SQL("source.created"),
            SourceSortBy.UPDATED: sql.SQL("source.metadata_updated"),
            SourceSortBy.LABEL: sql.SQL("source.label"),
        }[sort_by]
        _append_listing_cursor_filter(
            clauses,
            params,
            window,
            value_sql=sort_expression,
            identity_sql=sql.SQL("source.id"),
            timestamp=sort_by != SourceSortBy.LABEL,
        )
        where_sql = _where_sql(clauses)
        descending = sort_by.descending(reverse_order=reverse_order)
        order_sql = _listing_order_sql(
            sort_expression,
            sql.SQL("source.id"),
            descending=descending,
            missing_first=reverse_order,
        )
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    """
                    SELECT source.record
                    FROM tamoss_sources AS source
                    {}
                    ORDER BY {}
                    OFFSET %(offset)s
                    LIMIT %(limit)s
                    """
                ).format(where_sql, order_sql),
                params,
            )
            rows = cur.fetchall()
        sources = [_source_from_record(row[0]) for row in rows]
        return listing_page(
            sources,
            window,
            value=lambda source: {
                SourceSortBy.CREATED: source.created,
                SourceSortBy.UPDATED: source.metadata_updated,
                SourceSortBy.LABEL: source.label,
            }[sort_by],
            identity=lambda source: source.id,
        )

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
                ) WITH ORDINALITY AS item(value, ordinality)
                JOIN tamoss_flows AS child
                  ON child.id::text = item.value->>'id'
                WHERE parent.source_id IS NOT NULL
                  AND child.source_id IS NOT NULL
                  AND (
                    parent.source_id = ANY(%(source_ids)s::uuid[])
                    OR child.source_id = ANY(%(source_ids)s::uuid[])
                  )
                ORDER BY parent.source_id, parent.id, item.ordinality
                """,
                {"source_ids": requested_ids},
            )
            rows = cur.fetchall()

        for parent_source_id, child_source_id, role in rows:
            if parent_source_id in relationships:
                source_item = {"id": str(child_source_id)}
                if role is not None:
                    source_item["role"] = str(role)
                source_collection = relationships[parent_source_id].source_collection
                if source_item not in source_collection:
                    source_collection.append(source_item)
            if child_source_id in relationships:
                collected_by = relationships[child_source_id].collected_by
                if parent_source_id not in collected_by:
                    collected_by.append(parent_source_id)
        return relationships

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

        clauses: list[sql.Composable] = [sql.SQL("flow.id = ANY(%(flow_ids)s::uuid[])")]
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
