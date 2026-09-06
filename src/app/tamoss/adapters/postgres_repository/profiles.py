from __future__ import annotations

# mypy: disable-error-code=attr-defined
# Focused store methods run with repository-owned connection state.
from datetime import datetime
from typing import Any
from uuid import UUID

from psycopg import sql
from psycopg.types.json import Jsonb

from tamoss.domain.model import ProfileRecord
from tamoss.domain.pagination import Page, resolve_page_window


class PostgresProfileMixin:
    def lock_profile(self, profile_id: UUID) -> None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                "SELECT pg_advisory_xact_lock(hashtextextended(%s, 0))",
                (f"tamoss-profile:{profile_id}",),
            )

    def list_profiles_page(
        self,
        *,
        format: str | None,
        codec: str | None,
        label: str | None,
        page: str | None,
        limit: int | None,
    ) -> Page[ProfileRecord]:
        window = resolve_page_window(page=page, limit=limit)
        clauses: list[sql.Composable] = []
        params: dict[str, Any] = {
            "offset": window.offset,
            "limit": window.limit + 1,
        }
        if format is not None:
            clauses.append(sql.SQL("format = %(format)s"))
            params["format"] = format
        if codec is not None:
            clauses.append(sql.SQL("codec = %(codec)s"))
            params["codec"] = codec
        if label is not None:
            clauses.append(sql.SQL("label = %(label)s"))
            params["label"] = label
        where: sql.Composable = sql.SQL("")
        if clauses:
            where = sql.SQL("WHERE ") + sql.SQL(" AND ").join(clauses)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                sql.SQL(
                    """
                    SELECT record
                    FROM tamoss_profiles
                    {}
                    ORDER BY id
                    OFFSET %(offset)s
                    LIMIT %(limit)s
                    """
                ).format(where),
                params,
            )
            rows = cur.fetchall()
        profiles = [_profile_from_record(row[0]) for row in rows[: window.limit]]
        next_page = (
            str(window.offset + window.limit) if len(rows) > window.limit else None
        )
        return Page(items=profiles, limit=window.limit, next_page=next_page)

    def get_profile(self, profile_id: UUID) -> ProfileRecord | None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                "SELECT record FROM tamoss_profiles WHERE id = %s", (profile_id,)
            )
            row = cur.fetchone()
        return _profile_from_record(row[0]) if row else None

    def create_profile(self, profile: ProfileRecord) -> bool:
        record = _profile_to_record(profile)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                INSERT INTO tamoss_profiles (
                    id, format, codec, label, tags, record, created
                )
                VALUES (
                    %(id)s, %(format)s, %(codec)s, %(label)s,
                    %(tags)s, %(record)s, %(created)s
                )
                ON CONFLICT (id) DO NOTHING
                RETURNING id
                """,
                {
                    "id": profile.id,
                    "format": profile.flow_metadata["format"],
                    "codec": profile.flow_metadata.get("codec"),
                    "label": profile.label,
                    "tags": Jsonb(profile.tags),
                    "record": Jsonb(record),
                    "created": profile.created,
                },
            )
            return cur.fetchone() is not None

    def count_flows_by_profile(self, profile_id: UUID) -> int:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                "SELECT COUNT(*) FROM tamoss_flows WHERE profile_id = %s",
                (profile_id,),
            )
            row = cur.fetchone()
        return int(row[0]) if row is not None else 0

    def delete_profile(self, profile_id: UUID) -> bool:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                "DELETE FROM tamoss_profiles WHERE id = %s RETURNING id",
                (profile_id,),
            )
            return cur.fetchone() is not None


def _profile_to_record(profile: ProfileRecord) -> dict[str, Any]:
    return {
        "id": str(profile.id),
        "flow_metadata": profile.flow_metadata,
        "label": profile.label,
        "description": profile.description,
        "created_by": profile.created_by,
        "created": profile.created.isoformat(),
        "tags": profile.tags,
    }


def _profile_from_record(record: dict[str, Any]) -> ProfileRecord:
    return ProfileRecord(
        id=UUID(record["id"]),
        flow_metadata=dict(record["flow_metadata"]),
        label=record.get("label"),
        description=record.get("description"),
        created_by=record.get("created_by"),
        created=datetime.fromisoformat(str(record["created"]).replace("Z", "+00:00")),
        tags=dict(record.get("tags") or {}),
    )
