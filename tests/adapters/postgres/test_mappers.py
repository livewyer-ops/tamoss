from __future__ import annotations

from uuid import uuid4

from tamoss.adapters.postgres_repository.mappers import (
    _media_object_from_record,
)
from tamoss.domain.model import StorageBackend


def test_media_object_mapper_uses_supplied_storage_backends() -> None:
    storage_backends = {
        uuid4(): _storage_backend("primary"),
        uuid4(): _storage_backend("archive"),
    }
    storage_backends = {backend.id: backend for backend in storage_backends.values()}
    record = {
        "id": "bbc/object.ts",
        "referenced_by_flows": [],
        "instances": [
            {
                "storage_backend_id": str(storage_backend_id),
                "controlled": True,
            }
            for index in range(50)
            for storage_backend_id in [list(storage_backends)[index % 2]]
        ],
    }

    media_object = _media_object_from_record(
        record,
        storage_backends_by_id=storage_backends,
    )

    assert {
        instance.storage_backend.id
        for instance in media_object.instances
        if instance.storage_backend is not None
    } == set(storage_backends)


def _storage_backend(label: str) -> StorageBackend:
    return StorageBackend(
        id=uuid4(),
        label=f"tamoss.storage.{label}",
        provider="tamoss",
        region="us-east-1",
        store_product="s3",
        bucket_name=f"tamoss-{label}",
    )
