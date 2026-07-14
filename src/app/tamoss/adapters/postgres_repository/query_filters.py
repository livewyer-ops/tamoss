from __future__ import annotations

from dataclasses import replace
from typing import Any
from uuid import UUID

from psycopg import sql
from psycopg.types.json import Jsonb

from tamoss.adapters.postgres_repository.types import PostgresCursor
from tamoss.domain.model import FlowRecord


def _where_sql(clauses: list[sql.Composable]) -> sql.Composable:
    if not clauses:
        return sql.SQL("")
    return sql.SQL("WHERE ") + sql.SQL(" AND ").join(clauses)


def _listing_order_sql(
    value_sql: sql.Composable,
    identity_sql: sql.Composable,
    *,
    descending: bool,
) -> sql.Composable:
    direction = sql.SQL("DESC") if descending else sql.SQL("ASC")
    return sql.SQL("{} {} NULLS LAST, {} ASC").format(
        value_sql,
        direction,
        identity_sql,
    )


def _append_tag_filter_clauses(
    clauses: list[sql.Composable],
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
        value_clauses: list[sql.Composable] = []
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
    clauses: list[sql.Composable],
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

    query_end = (
        timerange_start + 1
        if timerange_is_point
        and timerange_start is not None
        and timerange_end == timerange_start
        else timerange_end
    )
    having: list[sql.Composable] = []
    if query_end is not None:
        params["flow_timerange_end"] = query_end
        having.append(sql.SQL("MIN(segment.timerange_start) < %(flow_timerange_end)s"))
    if timerange_start is not None:
        params["flow_timerange_start"] = timerange_start
        having.append(sql.SQL("MAX(segment.timerange_end) > %(flow_timerange_start)s"))
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


def _flows_with_collected_by(
    cur: PostgresCursor, flows: list[FlowRecord]
) -> list[FlowRecord]:
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
