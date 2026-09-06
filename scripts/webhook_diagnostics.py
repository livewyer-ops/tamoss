#!/usr/bin/env python3
"""Read a bounded, credential-free webhook delivery report from PostgreSQL."""

from __future__ import annotations

import argparse
import json
from typing import Any
from urllib.parse import urlsplit
from uuid import UUID

import psycopg
from psycopg.rows import dict_row
from tamoss.settings import Settings

FILTERS = (
    "flow_ids",
    "source_ids",
    "flow_collected_by_ids",
    "source_collected_by_ids",
    "accept_get_urls",
    "accept_storage_ids",
    "presigned",
    "verbose_storage",
    "include_object_timerange",
)


def report(connection: psycopg.Connection, webhook_id: UUID) -> dict[str, Any]:
    with connection.transaction(), connection.cursor(row_factory=dict_row) as cursor:
        cursor.execute("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY")
        cursor.execute("SET LOCAL statement_timeout = '5s'")
        cursor.execute(
            "SELECT status, record -> 'data' AS data "
            "FROM tamoss_webhooks WHERE id = %s",
            (webhook_id,),
        )
        webhook = cursor.fetchone()
        if webhook is None:
            raise ValueError("Webhook does not exist")
        data = webhook["data"]
        cursor.execute(
            """
            SELECT NOW() AS observed_at,
                COUNT(*) FILTER (WHERE status = 'pending') AS pending,
                COUNT(*) FILTER (WHERE status = 'started') AS started,
                COUNT(*) FILTER (WHERE status = 'done') AS done,
                COUNT(*) FILTER (WHERE status = 'dead') AS dead,
                COUNT(*) FILTER (
                    WHERE status IN ('pending', 'started')
                    AND claim_expires_at <= NOW()
                ) AS expired_claims,
                MIN(created_at) FILTER (
                    WHERE status IN ('pending', 'started')
                ) AS oldest_pending_at,
                MIN(next_attempt_at) FILTER (
                    WHERE status = 'pending'
                ) AS next_attempt_at,
                MAX((record ->> 'updated')::timestamptz) FILTER (
                    WHERE status = 'done'
                ) AS last_success_at,
                MAX((record ->> 'updated')::timestamptz) FILTER (
                    WHERE (record ->> 'attempt_count')::integer > 0
                ) AS last_attempt_activity_at
            FROM tamoss_webhook_deliveries
            WHERE webhook_id = %s
            """,
            (webhook_id,),
        )
        summary = cursor.fetchone()
        cursor.execute(
            """
            SELECT id, status, created_at, updated_at, next_attempt_at,
                claim_expires_at,
                record ->> 'event_type' AS event_type,
                (record ->> 'attempt_count')::integer AS attempt_count,
                (record ->> 'response_status')::integer AS response_status,
                record #>> '{error,type}' AS error_type
            FROM tamoss_webhook_deliveries
            WHERE webhook_id = %s
            ORDER BY updated_at DESC, id DESC
            LIMIT 20
            """,
            (webhook_id,),
        )
        return {
            "webhook_id": str(webhook_id),
            "status": webhook["status"],
            "receiver_host": urlsplit(data.get("url", "")).hostname,
            "events": data.get("events", []),
            "filters": {name: data[name] for name in FILTERS if name in data},
            "retained_deliveries": summary,
            "recent_deliveries": cursor.fetchall(),
        }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--webhook-id", type=UUID, required=True)
    args = parser.parse_args()
    database_url = Settings().database_url_value()
    if not database_url:
        parser.error("Configure POSTGRES_HOST and the existing POSTGRES_* credentials")
    try:
        with psycopg.connect(database_url, connect_timeout=5, autocommit=True) as conn:
            result = report(conn, args.webhook_id)
    except psycopg.Error as exc:
        raise SystemExit(f"Database diagnostic failed: {type(exc).__name__}") from None
    except ValueError as exc:
        raise SystemExit(str(exc)) from None
    print(json.dumps(result, indent=2, default=str))


if __name__ == "__main__":
    main()
