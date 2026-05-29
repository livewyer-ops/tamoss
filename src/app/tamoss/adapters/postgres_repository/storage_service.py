from __future__ import annotations

# mypy: disable-error-code=attr-defined
# Focused store methods run with repository-owned connection and mapper state.
from uuid import UUID

from psycopg.types.json import Jsonb

from tamoss.adapters.postgres_repository.mappers import (
    _lock_flow_segments,
    _storage_backend_from_record,
    _storage_backend_to_record,
)
from tamoss.domain.model import ServiceMetadata, StorageBackend

_SERVICE_METADATA_ID = "default"


class PostgresStorageServiceMixin:
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
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT record
                FROM tamoss_storage_backends
                ORDER BY default_storage DESC, label, id
                """
            )
            return [
                _storage_backend_from_record(
                    row[0],
                    configured_storage_backend=self._configured_storage_backend,
                )
                for row in cur.fetchall()
            ]

    def default_storage_backend(self) -> StorageBackend | None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT record
                FROM tamoss_storage_backends
                WHERE default_storage IS TRUE
                ORDER BY updated_at DESC
                LIMIT 1
                """
            )
            row = cur.fetchone()
            if row is None:
                return None
            return _storage_backend_from_record(
                row[0],
                configured_storage_backend=self._configured_storage_backend,
            )

    def get_storage_backend(self, storage_id: UUID) -> StorageBackend | None:
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                SELECT record
                FROM tamoss_storage_backends
                WHERE id = %s
                """,
                (storage_id,),
            )
            row = cur.fetchone()
            if row is None:
                return None
            return _storage_backend_from_record(
                row[0],
                configured_storage_backend=self._configured_storage_backend,
            )

    def _upsert_configured_storage_backend(self, backend: StorageBackend) -> None:
        record = _storage_backend_to_record(backend)
        with self._connect() as conn, conn.cursor() as cur:
            cur.execute(
                """
                UPDATE tamoss_storage_backends
                SET default_storage = FALSE,
                    record = jsonb_set(
                        record,
                        '{default_storage}',
                        'false'::jsonb,
                        true
                    ),
                    updated_at = NOW()
                WHERE id <> %s
                  AND default_storage IS TRUE
                """,
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
