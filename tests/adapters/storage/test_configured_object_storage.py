from __future__ import annotations

import os
from collections.abc import Iterator
from urllib.parse import unquote, urlparse
from uuid import UUID, uuid4

import boto3
import pytest
import requests
from botocore.config import Config
from botocore.exceptions import BotoCoreError, ClientError
from tamoss.adapters.object_storage import ConfiguredObjectStorage
from tamoss.domain.model import StorageBackend
from tamoss.settings import Settings, StorageBackendSettings

pytestmark = pytest.mark.needs_s3


@pytest.fixture()
def s3_backend() -> Iterator[StorageBackend]:
    backend = _s3_backend(
        id=UUID("33333333-3333-4333-8333-333333333333"),
        label="tamoss.storage.primary",
        bucket_name=f"tamoss-adapter-{uuid4().hex[:12]}",
    )
    try:
        _client(backend).list_buckets()
    except (BotoCoreError, ClientError) as exc:
        pytest.skip(f"S3-compatible test endpoint is unavailable: {exc}")
    _ensure_bucket(backend)
    try:
        yield backend
    finally:
        _empty_and_delete_bucket(backend)


@pytest.fixture()
def object_storage(
    s3_backend: StorageBackend,
) -> ConfiguredObjectStorage:
    settings = Settings(
        auth_required=False,
        public_base_url="http://testserver",
        s3_presign_ttl_seconds=120,
        s3_connect_timeout_seconds=2,
        s3_read_timeout_seconds=2,
        storage_backend=_settings_backend(s3_backend),
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
        flow_container="video/mp2t",
        backend=s3_backend,
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


def _s3_backend(
    *,
    id: UUID,
    label: str,
    bucket_name: str,
) -> StorageBackend:
    endpoint = _s3_endpoint()
    return StorageBackend(
        id=id,
        label=label,
        provider="tamoss",
        region="us-east-1",
        store_product="s3",
        default_storage=True,
        bucket_name=bucket_name,
        endpoint_url=endpoint,
        public_endpoint_url=endpoint,
        access_key=_s3_access_key(),
        secret_key=_s3_secret_key(),
    )


def _settings_backend(backend: StorageBackend) -> StorageBackendSettings:
    return StorageBackendSettings(
        id=backend.id,
        label=backend.label,
        provider=backend.provider,
        region=backend.region,
        store_product=backend.store_product,
        default_storage=backend.default_storage,
        bucket_name=backend.bucket_name,
        endpoint_url=backend.endpoint_url,
        public_endpoint_url=backend.public_endpoint_url,
        access_key=backend.access_key,
        secret_key=backend.secret_key,
    )


def _client(backend: StorageBackend):
    return boto3.client(
        "s3",
        endpoint_url=backend.endpoint_url,
        aws_access_key_id=backend.access_key,
        aws_secret_access_key=backend.secret_key,
        region_name=backend.region,
        config=Config(
            s3={"addressing_style": "path"},
            connect_timeout=2,
            read_timeout=2,
            retries={"max_attempts": 2, "mode": "standard"},
        ),
    )


def _empty_and_delete_bucket(backend: StorageBackend) -> None:
    if not backend.bucket_name:
        return
    client = _client(backend)
    try:
        response = client.list_objects_v2(Bucket=backend.bucket_name)
    except ClientError:
        return
    for item in response.get("Contents", []):
        client.delete_object(Bucket=backend.bucket_name, Key=item["Key"])
    try:
        client.delete_bucket(Bucket=backend.bucket_name)
    except ClientError:
        return


def _ensure_bucket(backend: StorageBackend) -> None:
    client = _client(backend)
    try:
        client.head_bucket(Bucket=backend.bucket_name)
    except ClientError:
        client.create_bucket(Bucket=backend.bucket_name)


def _s3_endpoint() -> str:
    return (
        os.getenv("TAMOSS_TEST_S3_ENDPOINT")
        or os.getenv("TAMOSS_S3_ENDPOINT")
        or "http://127.0.0.1:9000"
    )


def _s3_access_key() -> str:
    return (
        os.getenv("TAMOSS_TEST_S3_ACCESS_KEY")
        or os.getenv("BUCKET_USER")
        or os.getenv("TAMOSS_S3_ACCESS_KEY")
        or "rustfsadmin"
    )


def _s3_secret_key() -> str:
    return (
        os.getenv("TAMOSS_TEST_S3_SECRET_KEY")
        or os.getenv("BUCKET_PASSWORD")
        or os.getenv("TAMOSS_S3_SECRET_KEY")
        or "rustfsadmin"
    )
