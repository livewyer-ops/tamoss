from __future__ import annotations

# mypy: disable-error-code=attr-defined
# Focused store methods run with repository-owned connection and mapper state.
from collections.abc import Callable
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
    _where_sql,
)
from tamoss.domain.model import (
    DeletionRequestRecord,
    ObjectCleanupRecord,
    ObjectCopyRecord,
    WebhookDeliveryRecord,
    WebhookRecord,
)
from tamoss.domain.pagination import Page, resolve_page_window

_EMPTY_SQL = sql.SQL("")


class PostgresQueueMixin:
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
                    created_at,
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
                    %(created_at)s,
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
                    "created_at": delivery.created,
                },
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
        record = _object_cleanup_to_record(cleanup)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                INSERT INTO tamoss_object_cleanups (
                    id,
                    delete_request_id,
                    object_id,
                    storage_backend_id,
                    status,
                    claimed_at,
                    claimed_by,
                    claim_expires_at,
                    record,
                    updated
                )
                VALUES (
                    %(id)s,
                    %(delete_request_id)s,
                    %(object_id)s,
                    %(storage_backend_id)s,
                    %(status)s,
                    %(claimed_at)s,
                    %(claimed_by)s,
                    %(claim_expires_at)s,
                    %(record)s,
                    %(updated)s
                )
                ON CONFLICT (id) DO UPDATE SET
                    delete_request_id = EXCLUDED.delete_request_id,
                    object_id = EXCLUDED.object_id,
                    storage_backend_id = EXCLUDED.storage_backend_id,
                    status = EXCLUDED.status,
                    claimed_at = EXCLUDED.claimed_at,
                    claimed_by = EXCLUDED.claimed_by,
                    claim_expires_at = EXCLUDED.claim_expires_at,
                    record = EXCLUDED.record,
                    updated = EXCLUDED.updated,
                    updated_at = NOW()
                """,
                {
                    "id": cleanup.id,
                    "delete_request_id": cleanup.delete_request_id,
                    "object_id": cleanup.object_id,
                    "storage_backend_id": cleanup.storage_backend_id,
                    "status": cleanup.status,
                    "claimed_at": cleanup.claimed_at,
                    "claimed_by": cleanup.claimed_by,
                    "claim_expires_at": cleanup.claim_expires_at,
                    "record": Jsonb(record),
                    "updated": cleanup.updated,
                },
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
        record = _object_copy_to_record(copy)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                INSERT INTO tamoss_object_copies (
                    id,
                    object_id,
                    source_storage_backend_id,
                    destination_storage_backend_id,
                    status,
                    claimed_at,
                    claimed_by,
                    claim_expires_at,
                    record,
                    updated
                )
                VALUES (
                    %(id)s,
                    %(object_id)s,
                    %(source_storage_backend_id)s,
                    %(destination_storage_backend_id)s,
                    %(status)s,
                    %(claimed_at)s,
                    %(claimed_by)s,
                    %(claim_expires_at)s,
                    %(record)s,
                    %(updated)s
                )
                ON CONFLICT (id) DO UPDATE SET
                    object_id = EXCLUDED.object_id,
                    source_storage_backend_id = EXCLUDED.source_storage_backend_id,
                    destination_storage_backend_id =
                        EXCLUDED.destination_storage_backend_id,
                    status = EXCLUDED.status,
                    claimed_at = EXCLUDED.claimed_at,
                    claimed_by = EXCLUDED.claimed_by,
                    claim_expires_at = EXCLUDED.claim_expires_at,
                    record = EXCLUDED.record,
                    updated = EXCLUDED.updated,
                    updated_at = NOW()
                """,
                {
                    "id": copy.id,
                    "object_id": copy.object_id,
                    "source_storage_backend_id": copy.source_storage_backend_id,
                    "destination_storage_backend_id": (
                        copy.destination_storage_backend_id
                    ),
                    "status": copy.status,
                    "claimed_at": copy.claimed_at,
                    "claimed_by": copy.claimed_by,
                    "claim_expires_at": copy.claim_expires_at,
                    "record": Jsonb(record),
                    "updated": copy.updated,
                },
            )

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
                claimed_at = NOW(),
                claimed_by = %(worker_id)s,
                claim_expires_at = NOW()
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
