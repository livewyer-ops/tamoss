from __future__ import annotations

import os
from collections.abc import Iterable
from pathlib import Path
from uuid import UUID

import psycopg
from tamoss.application.use_cases import TamossUseCases
from tamoss.auth import Identity
from tamoss.domain.model import (
    ObjectGetUrlBatchKey,
    ObjectGetUrlRequest,
    ObjectStorageMetadata,
    StorageBackend,
)
from tamoss.settings import Settings, StorageBackendSettings

from tests.support import paths

PRIMARY_BACKEND_ID = UUID("11111111-1111-4111-8111-111111111111")
REPLACEMENT_BACKEND_ID = UUID("22222222-2222-4222-8222-222222222222")
SCHEMA_ASSETS_DIR = paths.SCHEMA_ASSETS_DIR


def database_url() -> str:
    return (
        os.getenv("TAMOSS_TEST_DB_URL") or "postgresql://tams:tams@127.0.0.1:55432/tams"
    )


def execute_sql_file(connection: psycopg.Connection, path: Path) -> None:
    with connection.cursor() as cur:
        cur.execute(path.read_text(encoding="utf-8"))


def primary_backend() -> StorageBackend:
    return StorageBackend(
        id=PRIMARY_BACKEND_ID,
        label="tamoss.postgres.primary",
        provider="tamoss",
        region="us-east-1",
        store_product="s3",
        default_storage=True,
        bucket_name="primary",
        endpoint_url="https://objects.internal.example.test",
        public_endpoint_url="https://objects.example.test",
    )


def replacement_backend() -> StorageBackend:
    return StorageBackend(
        id=REPLACEMENT_BACKEND_ID,
        label="tamoss.postgres.replacement",
        provider="tamoss",
        region="us-east-1",
        store_product="s3",
        default_storage=True,
        bucket_name="replacement",
        endpoint_url="https://objects.internal.example.test",
        public_endpoint_url="https://objects.example.test",
    )


def use_cases(
    repository,
    *,
    object_storage: RecordingObjectStorage | None = None,
) -> TamossUseCases:
    return TamossUseCases(
        repository=repository,
        object_storage=object_storage or RecordingObjectStorage(),
        settings=Settings(
            auth_required=False,
            storage_backend=StorageBackendSettings(
                id=PRIMARY_BACKEND_ID,
                label="tamoss.postgres.primary",
                provider="tamoss",
                region="us-east-1",
                store_product="s3",
                default_storage=True,
                bucket_name="primary",
                endpoint_url="https://objects.internal.example.test",
                public_endpoint_url="https://objects.example.test",
                access_key="access",
                secret_key="secret",
            ),
        ),
    )


def identity() -> Identity:
    return Identity(subject="postgres-test", method="test")


def video_flow_write(flow_id: UUID, source_id: UUID) -> dict[str, object]:
    return {
        "id": str(flow_id),
        "source_id": str(source_id),
        "format": "urn:x-nmos:format:video",
        "codec": "video/h264",
        "container": "video/mp2t",
        "essence_parameters": {
            "frame_width": 1920,
            "frame_height": 1080,
            "frame_rate": {"numerator": 25, "denominator": 1},
        },
    }


def multi_flow_write(flow_id: UUID, source_id: UUID) -> dict[str, object]:
    return {
        "id": str(flow_id),
        "source_id": str(source_id),
        "format": "urn:x-nmos:format:multi",
        "container": "video/mp2t",
    }


class RecordingObjectStorage:
    """Postgres integration fake for repository-owned object records.

    Unlike the shared in-memory object-storage fake, this fake reports metadata
    for objects that only exist in Postgres fixtures. That keeps repository
    deletion tests focused on database state while still recording cleanup calls.
    """

    def __init__(self) -> None:
        self.deleted: list[tuple[UUID, str]] = []
        self.deleted_batches: list[list[tuple[UUID, str]]] = []
        self.copied: list[tuple[UUID, UUID, str]] = []

    def build_put_request(
        self,
        *,
        object_id: str,
        content_type: str,
        backend: StorageBackend,
        presigned: bool,
    ) -> dict[str, object]:
        return {
            "url": f"https://objects.example.test/{object_id}",
            "content-type": content_type,
            "headers": {"Content-Type": content_type},
        }

    def build_get_url(self, *, object_id: str, backend: StorageBackend) -> str:
        return f"https://objects.example.test/{object_id}"

    def build_get_urls(
        self, *, object_id: str, backend: StorageBackend
    ) -> list[dict[str, object]]:
        return [
            {
                "url": f"https://objects.example.test/{object_id}",
                "label": backend.label,
                "presigned": False,
            },
            {
                "url": f"https://objects.example.test/{object_id}",
                "label": backend.label,
                "presigned": True,
            },
        ]

    def build_get_urls_batch(
        self, requests: Iterable[ObjectGetUrlRequest]
    ) -> dict[ObjectGetUrlBatchKey, list[dict[str, object]]]:
        unique_requests: dict[ObjectGetUrlBatchKey, ObjectGetUrlRequest] = {}
        for request in requests:
            key = (request.backend.id, request.object_id)
            existing = unique_requests.get(key)
            if existing is None:
                unique_requests[key] = request
                continue
            existing.include_direct = existing.include_direct or request.include_direct
            existing.include_presigned = (
                existing.include_presigned or request.include_presigned
            )
        result: dict[ObjectGetUrlBatchKey, list[dict[str, object]]] = {}
        for key, request in unique_requests.items():
            all_urls = self.build_get_urls(object_id=key[1], backend=request.backend)
            result[key] = [
                item
                for item in all_urls
                if (request.include_direct and item.get("presigned") is False)
                or (request.include_presigned and item.get("presigned") is True)
            ]
        return result

    def write(
        self, object_id: str, data: bytes, *, backend: StorageBackend | None = None
    ) -> None:
        return None

    def read(
        self, object_id: str, *, backend: StorageBackend | None = None
    ) -> bytes | None:
        return None

    def object_metadata(
        self, object_id: str, *, backend: StorageBackend | None = None
    ) -> ObjectStorageMetadata | None:
        return ObjectStorageMetadata(content_length=1)

    def copy(
        self,
        object_id: str,
        *,
        source_backend: StorageBackend,
        destination_backend: StorageBackend,
    ) -> None:
        self.copied.append((source_backend.id, destination_backend.id, object_id))

    def delete(self, object_id: str, *, backend: StorageBackend | None = None) -> None:
        self.delete_batch([object_id], backend=backend)

    def delete_batch(
        self, object_ids: Iterable[str], *, backend: StorageBackend | None = None
    ) -> None:
        assert backend is not None
        batch = [(backend.id, object_id) for object_id in dict.fromkeys(object_ids)]
        if not batch:
            return
        self.deleted_batches.append(batch)
        self.deleted.extend(batch)
