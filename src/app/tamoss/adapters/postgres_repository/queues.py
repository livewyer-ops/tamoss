from __future__ import annotations

# mypy: disable-error-code=attr-defined
# Focused store methods run with repository-owned connection and mapper state.
from collections.abc import Callable
from datetime import datetime
from typing import Any
from uuid import UUID

from psycopg import sql
from psycopg.types.json import Jsonb

from tamoss.adapters.postgres_repository.mappers import (
    _delete_request_from_row,
    _delete_request_to_record,
    _object_cleanup_from_row,
    _object_cleanup_to_record,
    _object_copy_from_row,
    _object_copy_to_record,
    _webhook_delivery_from_row,
    _webhook_delivery_to_record,
    _webhook_from_record,
    _webhook_to_record,
)
from tamoss.adapters.postgres_repository.query_filters import (
    _append_tag_filter_clauses,
    _listing_order_sql,
    _where_sql,
)
from tamoss.domain.listings import DeleteRequestSortBy
from tamoss.domain.model import (
    DeletionRequestRecord,
    ObjectCleanupRecord,
    ObjectCopyRecord,
    WebhookDeliveryRecord,
    WebhookRecord,
    utc_now,
)
from tamoss.domain.pagination import Page, resolve_page_window
from tamoss.worker_claims import WorkerClaimLost, WorkerRecord, active_worker_claims

_EMPTY_SQL = sql.SQL("")
_QUEUE_TABLES = {
    WebhookDeliveryRecord: "tamoss_webhook_deliveries",
    DeletionRequestRecord: "tamoss_delete_requests",
    ObjectCleanupRecord: "tamoss_object_cleanups",
    ObjectCopyRecord: "tamoss_object_copies",
}

# Terminal rows are kept for observability but never read by claim queries;
# without retention the queue tables (webhook deliveries especially: one row
# per event per webhook) grow without bound.
_PURGEABLE_QUEUE_TABLES: tuple[tuple[str, tuple[str, ...]], ...] = (
    ("tamoss_webhook_deliveries", ("done", "dead")),
    ("tamoss_delete_requests", ("done",)),
    ("tamoss_object_cleanups", ("done",)),
    ("tamoss_object_copies", ("done",)),
)


class PostgresQueueMixin:
    def renew_worker_claim(self, record: WorkerRecord, lease_seconds: int) -> bool:
        if record.claim_token is None:
            return False
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    """
                    UPDATE {table}
                    SET claim_expires_at = clock_timestamp()
                        + (%s * INTERVAL '1 second')
                    WHERE id = %s AND claimed_at = %s
                      AND claim_expires_at > clock_timestamp()
                    """
                ).format(table=sql.Identifier(_QUEUE_TABLES[type(record)])),
                (lease_seconds, record.id, record.claim_token),
            )
            return cur.rowcount == 1

    def _save_worker_record(
        self, record: WorkerRecord, payload: dict[str, Any], **values: Any
    ) -> None:
        table = sql.Identifier(_QUEUE_TABLES[type(record)])
        active = active_worker_claims.get() or {}
        claim = active.get(record.id, record)
        values.update(
            id=record.id,
            status=record.status,
            claimed_at=record.claimed_at,
            claimed_by=record.claimed_by,
            claim_expires_at=record.claim_expires_at,
            record=Jsonb(payload),
            updated_at=utc_now(),
        )
        assignments = sql.SQL(", ").join(
            sql.SQL("{} = %({})s").format(sql.Identifier(name), sql.SQL(name))
            if name != "claim_expires_at"
            else sql.SQL(
                "claim_expires_at = CASE WHEN %(claimed_at)s::timestamptz IS NULL "
                "THEN %(claim_expires_at)s ELSE GREATEST("
                "{table}.claim_expires_at, %(claim_expires_at)s) END"
            ).format(table=table)
            for name in values
            if name != "id"
        )
        with self._connect() as conn, conn.transaction(), conn.cursor() as cur:
            # Child cleanups are owned by the parent deletion claim, not by
            # their own leases. Check the original parent before saving them.
            if (
                isinstance(record, ObjectCleanupRecord)
                and record.delete_request_id in active
            ):
                parent = active[record.delete_request_id]
                cur.execute(
                    """
                    SELECT id FROM tamoss_delete_requests
                    WHERE id = %s AND claimed_at = %s
                      AND claim_expires_at > clock_timestamp()
                    FOR UPDATE
                    """,
                    (parent.id, parent.claim_token),
                )
                if cur.fetchone() is None:
                    raise WorkerClaimLost(str(parent.id))
            if claim.claim_token is None:
                query = sql.SQL(
                    "INSERT INTO {table} ({columns}) VALUES ({parameters}) "
                    "ON CONFLICT (id) DO UPDATE SET {assignments} "
                    "WHERE {table}.claimed_at IS NULL"
                ).format(
                    table=table,
                    columns=sql.SQL(", ").join(map(sql.Identifier, values)),
                    parameters=sql.SQL(", ").join(map(sql.Placeholder, values)),
                    assignments=assignments,
                )
            else:
                query = sql.SQL(
                    "UPDATE {table} SET {assignments} "
                    "WHERE id = %(id)s AND claimed_at = %(claim_token)s "
                    "AND claim_expires_at > clock_timestamp()"
                ).format(table=table, assignments=assignments)
                values["claim_token"] = claim.claim_token
            cur.execute(query, values)
            if cur.rowcount != 1:
                raise WorkerClaimLost(str(record.id))

    def list_webhooks(self) -> list[WebhookRecord]:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute("SELECT record FROM tamoss_webhooks ORDER BY id")
            return [_webhook_from_record(row[0]) for row in cur.fetchall()]

    def list_webhooks_page(
        self,
        *,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        reverse_order: bool,
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
        direction = sql.SQL("DESC") if reverse_order else sql.SQL("ASC")
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    """
                    SELECT webhook.record
                    FROM tamoss_webhooks AS webhook
                    {}
                    ORDER BY webhook.record->'data'->>'url' {}, webhook.id {}
                    OFFSET %(offset)s
                    LIMIT %(limit)s
                    """
                ).format(where_sql, direction, direction),
                params,
            )
            rows = cur.fetchall()
        webhooks = [_webhook_from_record(row[0]) for row in rows[: window.limit]]
        next_page = (
            str(window.offset + window.limit) if len(rows) > window.limit else None
        )
        return Page(items=webhooks, limit=window.limit, next_page=next_page)

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
        self._save_worker_record(
            delivery,
            _webhook_delivery_to_record(delivery),
            webhook_id=delivery.webhook_id,
            next_attempt_at=delivery.next_attempt_at,
            created_at=delivery.created,
        )

    def claim_webhook_deliveries(
        self, *, worker_id: str, limit: int, lease_seconds: int
    ) -> list[WebhookDeliveryRecord]:
        with self._connect() as conn, conn.cursor() as cur:
            return _claim_worker_records(
                cur,
                table_name="tamoss_webhook_deliveries",
                alias="delivery",
                statuses=("pending", "started"),
                worker_id=worker_id,
                limit=limit,
                lease_seconds=lease_seconds,
                state_column="next_attempt_at",
                mapper=_webhook_delivery_from_row,
                candidate_filter=sql.SQL(
                    "AND (next_attempt_at IS NULL OR next_attempt_at <= NOW())"
                ),
            )

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

    def list_delete_requests_page(
        self,
        *,
        sort_by: DeleteRequestSortBy,
        reverse_order: bool,
        retention_seconds: int,
        page: str | None,
        limit: int | None,
    ) -> Page[DeletionRequestRecord]:
        window = resolve_page_window(page=page, limit=limit)
        created_sql = sql.SQL("request.created_at")
        expiry_sql = (
            sql.SQL("CASE WHEN request.status = 'done' THEN request.updated END")
            if retention_seconds > 0
            else sql.SQL("NULL::timestamptz")
        )
        sort_expression = {
            DeleteRequestSortBy.CREATED: created_sql,
            DeleteRequestSortBy.EXPIRY: expiry_sql,
        }[sort_by]
        order_sql = _listing_order_sql(
            sort_expression,
            sql.SQL("request.id"),
            descending=sort_by.descending(reverse_order=reverse_order),
            missing_first=reverse_order,
        )
        params = {
            "offset": window.offset,
            "limit": window.limit + 1,
            "retention_seconds": retention_seconds,
        }
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    """
                    SELECT
                        request.record,
                        request.status,
                        request.updated,
                        request.claimed_at,
                        request.claimed_by,
                        request.claim_expires_at
                    FROM tamoss_delete_requests AS request
                    ORDER BY {}
                    OFFSET %(offset)s
                    LIMIT %(limit)s
                    """
                ).format(order_sql),
                params,
            )
            rows = cur.fetchall()
        items = [_delete_request_from_row(row) for row in rows[: window.limit]]
        next_page = (
            str(window.offset + window.limit) if len(rows) > window.limit else None
        )
        return Page(items=items, limit=window.limit, next_page=next_page)

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
        self._save_worker_record(
            request,
            _delete_request_to_record(request),
            flow_id=request.flow_id,
            updated=request.updated,
            created_at=request.created,
        )

    def claim_delete_requests(
        self, *, worker_id: str, limit: int, lease_seconds: int
    ) -> list[DeletionRequestRecord]:
        with self._connect() as conn, conn.cursor() as cur:
            return _claim_worker_records(
                cur,
                table_name="tamoss_delete_requests",
                alias="request",
                statuses=("created", "started", "error"),
                worker_id=worker_id,
                limit=limit,
                lease_seconds=lease_seconds,
                state_column="updated",
                touch_column="updated",
                mapper=_delete_request_from_row,
            )

    def list_object_cleanups(
        self,
        *,
        delete_request_id: UUID | None = None,
        statuses: set[str] | None = None,
    ) -> list[ObjectCleanupRecord]:
        clauses: list[Any] = []
        params: dict[str, Any] = {}
        if delete_request_id is not None:
            clauses.append(sql.SQL("delete_request_id = %(delete_request_id)s"))
            params["delete_request_id"] = delete_request_id
        if statuses is not None:
            clauses.append(sql.SQL("status = ANY(%(statuses)s::text[])"))
            params["statuses"] = sorted(statuses)
        where_sql = _where_sql(clauses)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    """
                    SELECT
                        record,
                        status,
                        updated,
                        claimed_at,
                        claimed_by,
                        claim_expires_at
                    FROM tamoss_object_cleanups
                    {}
                    ORDER BY created_at, id
                    """
                ).format(where_sql),
                params,
            )
            return [_object_cleanup_from_row(row) for row in cur.fetchall()]

    def save_object_cleanup(self, cleanup: ObjectCleanupRecord) -> None:
        self._save_worker_record(
            cleanup,
            _object_cleanup_to_record(cleanup),
            delete_request_id=cleanup.delete_request_id,
            object_id=cleanup.object_id,
            storage_backend_id=cleanup.storage_backend_id,
            updated=cleanup.updated,
        )

    def claim_object_cleanups(
        self, *, worker_id: str, limit: int, lease_seconds: int
    ) -> list[ObjectCleanupRecord]:
        with self._connect() as conn, conn.cursor() as cur:
            return _claim_worker_records(
                cur,
                table_name="tamoss_object_cleanups",
                alias="cleanup",
                statuses=("pending", "started", "error"),
                worker_id=worker_id,
                limit=limit,
                lease_seconds=lease_seconds,
                state_column="updated",
                touch_column="updated",
                mapper=_object_cleanup_from_row,
                candidate_filter=sql.SQL("AND delete_request_id IS NULL"),
            )

    def list_object_copies(
        self, *, statuses: set[str] | None = None
    ) -> list[ObjectCopyRecord]:
        clauses: list[Any] = []
        params: dict[str, Any] = {}
        if statuses is not None:
            clauses.append(sql.SQL("status = ANY(%(statuses)s::text[])"))
            params["statuses"] = sorted(statuses)
        where_sql = _where_sql(clauses)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    """
                    SELECT
                        record,
                        status,
                        updated,
                        claimed_at,
                        claimed_by,
                        claim_expires_at
                    FROM tamoss_object_copies
                    {}
                    ORDER BY created_at, id
                    """
                ).format(where_sql),
                params,
            )
            return [_object_copy_from_row(row) for row in cur.fetchall()]

    def save_object_copy(self, copy: ObjectCopyRecord) -> None:
        self._save_worker_record(
            copy,
            _object_copy_to_record(copy),
            object_id=copy.object_id,
            source_storage_backend_id=copy.source_storage_backend_id,
            destination_storage_backend_id=copy.destination_storage_backend_id,
            updated=copy.updated,
        )

    def purge_finished_worker_records(self, *, older_than: datetime, limit: int) -> int:
        if limit < 1:
            return 0
        purged = 0
        with self._connect() as conn, conn.cursor() as cur:
            for table_name, statuses in _PURGEABLE_QUEUE_TABLES:
                if purged >= limit:
                    break
                preserve_dependencies = _EMPTY_SQL
                if table_name == "tamoss_object_cleanups":
                    preserve_dependencies = sql.SQL(
                        "AND NOT EXISTS (SELECT 1 FROM tamoss_delete_requests "
                        "WHERE id = tamoss_object_cleanups.delete_request_id)"
                    )
                elif table_name == "tamoss_delete_requests":
                    preserve_dependencies = sql.SQL(
                        "AND NOT EXISTS (SELECT 1 FROM tamoss_object_cleanups "
                        "WHERE delete_request_id = tamoss_delete_requests.id "
                        "AND status <> 'done')"
                    )
                cur.execute(
                    sql.SQL(
                        """
                        WITH target AS (
                            SELECT id
                            FROM {table}
                            WHERE status = ANY(%(statuses)s::text[])
                              AND updated_at < %(older_than)s
                              {preserve_dependencies}
                            LIMIT %(limit)s
                            FOR UPDATE SKIP LOCKED
                        )
                        DELETE FROM {table} AS finished
                        USING target
                        WHERE finished.id = target.id
                        """
                    ).format(
                        table=sql.Identifier(table_name),
                        preserve_dependencies=preserve_dependencies,
                    ),
                    {
                        "statuses": list(statuses),
                        "older_than": older_than,
                        "limit": limit - purged,
                    },
                )
                purged += cur.rowcount or 0
        return purged

    def claim_object_copies(
        self, *, worker_id: str, limit: int, lease_seconds: int
    ) -> list[ObjectCopyRecord]:
        with self._connect() as conn, conn.cursor() as cur:
            return _claim_worker_records(
                cur,
                table_name="tamoss_object_copies",
                alias="copy",
                statuses=("pending", "started", "error"),
                worker_id=worker_id,
                limit=limit,
                lease_seconds=lease_seconds,
                state_column="updated",
                touch_column="updated",
                mapper=_object_copy_from_row,
            )


def _claim_worker_records[T](
    cur: Any,
    *,
    table_name: str,
    alias: str,
    statuses: tuple[str, ...],
    worker_id: str,
    limit: int,
    lease_seconds: int,
    state_column: str,
    mapper: Callable[[Any], T],
    touch_column: str | None = None,
    candidate_filter: sql.SQL = _EMPTY_SQL,
) -> list[T]:
    alias_identifier = sql.Identifier(alias)
    touch_sql = (
        sql.SQL("{column} = NOW(),").format(column=sql.Identifier(touch_column))
        if touch_column is not None
        else sql.SQL("")
    )
    cur.execute(
        sql.SQL(
            """
            WITH candidates AS (
                SELECT id
                FROM {table}
                WHERE status = ANY(%(statuses)s::text[])
                  {candidate_filter}
                  AND (
                      claim_expires_at IS NULL
                      OR claim_expires_at <= NOW()
                  )
                ORDER BY created_at, id
                LIMIT %(limit)s
                FOR UPDATE SKIP LOCKED
            )
            UPDATE {table} AS {alias}
            SET status = 'started',
                claimed_at = clock_timestamp(),
                claimed_by = %(worker_id)s,
                claim_expires_at = clock_timestamp()
                    + (%(lease_seconds)s * INTERVAL '1 second'),
                {touch_sql}
                updated_at = NOW()
            FROM candidates
            WHERE {alias}.id = candidates.id
            RETURNING
                {alias}.created_at,
                {alias}.id,
                {alias}.record,
                {alias}.status,
                {alias}.{state_column},
                {alias}.claimed_at,
                {alias}.claimed_by,
                {alias}.claim_expires_at
            """
        ).format(
            table=sql.Identifier(table_name),
            alias=alias_identifier,
            candidate_filter=candidate_filter,
            state_column=sql.Identifier(state_column),
            touch_sql=touch_sql,
        ),
        {
            "statuses": list(statuses),
            "worker_id": worker_id,
            "limit": limit,
            "lease_seconds": lease_seconds,
        },
    )
    rows = cur.fetchall()
    rows.sort(key=lambda row: (row[0], str(row[1])))
    return [mapper(row[2:]) for row in rows]
