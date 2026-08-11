from __future__ import annotations

from collections.abc import Iterator
from urllib.parse import unquote, urlparse
from uuid import UUID, uuid4

import pytest
import requests
from botocore.exceptions import BotoCoreError, ClientError
from tamoss.adapters.object_storage import ConfiguredObjectStorage
from tamoss.domain.model import StorageBackend
from tamoss.settings import Settings

from tests.support.s3_storage import (
    empty_and_delete_bucket,
    ensure_bucket,
    s3_backend_record,
    s3_client,
    s3_settings_backend,
)

pytestmark = pytest.mark.needs_s3


@pytest.fixture()
def s3_backend() -> Iterator[StorageBackend]:
    backend = s3_backend_record(
        id=UUID("33333333-3333-4333-8333-333333333333"),
        label="tamoss.storage.primary",
        bucket_name=f"tamoss-adapter-{uuid4().hex[:12]}",
    )
    try:
        s3_client(backend).list_buckets()
    except (BotoCoreError, ClientError) as exc:
        pytest.skip(f"S3-compatible test endpoint is unavailable: {exc}")
    ensure_bucket(backend)
    try:
        yield backend
    finally:
        empty_and_delete_bucket(backend)


@pytest.fixture()
def object_storage(
    s3_backend: StorageBackend,
) -> ConfiguredObjectStorage:
    settings = Settings(
        auth_required=False,
        s3_presign_ttl_seconds=120,
        s3_connect_timeout_seconds=2,
        s3_read_timeout_seconds=2,
        storage_backend=s3_settings_backend(s3_backend),
    )
    return ConfiguredObjectStorage(settings)


def test_s3_presigned_put_and_get_urls_round_trip_uploaded_object(
    object_storage: ConfiguredObjectStorage,
    s3_backend: StorageBackend,
) -> None:
    object_id = f"bbc/adapter/{uuid4()}/segment 01.ts"
    body = b"tamoss configured storage adapter\n"

    put_request = object_storage.build_put_request(
        object_id=object_id,
        content_type="video/mp2t",
        backend=s3_backend,
        presigned=True,
    )
    assert put_request["headers"] == {"Content-Type": "video/mp2t"}

    put_response = requests.put(
        put_request["url"],
        data=body,
        headers=put_request["headers"],
        timeout=5,
    )
    assert put_response.status_code in {200, 201, 204}
    assert object_storage.read(object_id, backend=s3_backend) == body

    get_urls = object_storage.build_get_urls(object_id=object_id, backend=s3_backend)
    assert [item["presigned"] for item in get_urls] == [False, True]
    assert [item["label"] for item in get_urls] == [
        s3_backend.label,
        s3_backend.label,
    ]
    presigned_get_url = next(item for item in get_urls if item["presigned"] is True)
    assert unquote(urlparse(presigned_get_url["url"]).path).endswith(
        f"/{s3_backend.bucket_name}/{object_id}"
    )

    get_response = requests.get(presigned_get_url["url"], timeout=5)
    assert get_response.status_code == 200
    assert get_response.content == body


def test_s3_write_read_and_delete_are_scoped_to_configured_backend(
    object_storage: ConfiguredObjectStorage,
    s3_backend: StorageBackend,
) -> None:
    object_id = f"bbc/adapter/{uuid4()}/object.ts"

    object_storage.write(object_id, b"primary", backend=s3_backend)
    assert object_storage.read(object_id, backend=s3_backend) == b"primary"

    object_storage.delete(object_id, backend=s3_backend)
    assert object_storage.read(object_id, backend=s3_backend) is None
