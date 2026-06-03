from __future__ import annotations

import base64
import hashlib
import os
from collections.abc import Iterator
from uuid import UUID, uuid4

import boto3
import pytest
from botocore.config import Config
from botocore.exceptions import BotoCoreError, ClientError
from tamoss.domain.model import StorageBackend
from tamoss.settings import StorageBackendSettings


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


def s3_backend_record(
    *,
    id: UUID,
    label: str,
    bucket_name: str,
) -> StorageBackend:
    endpoint = s3_endpoint()
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
        access_key=s3_access_key(),
        secret_key=s3_secret_key(),
    )


def s3_settings_backend(backend: StorageBackend) -> StorageBackendSettings:
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


def checksum_value(body: bytes, algorithm: str) -> str:
    digest = hashlib.new(algorithm, body).digest()
    return base64.b64encode(digest).decode("ascii")


def s3_client(backend: StorageBackend):
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


def ensure_bucket(backend: StorageBackend) -> None:
    client = s3_client(backend)
    try:
        client.head_bucket(Bucket=backend.bucket_name)
    except ClientError:
        client.create_bucket(Bucket=backend.bucket_name)


def empty_and_delete_bucket(backend: StorageBackend) -> None:
    if not backend.bucket_name:
        return
    client = s3_client(backend)
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


def s3_endpoint() -> str:
    return (
        os.getenv("TAMOSS_TEST_S3_ENDPOINT")
        or os.getenv("TAMOSS_S3_ENDPOINT")
        or "http://127.0.0.1:9000"
    )


def s3_access_key() -> str:
    return (
        os.getenv("TAMOSS_TEST_S3_ACCESS_KEY")
        or os.getenv("BUCKET_USER")
        or os.getenv("TAMOSS_S3_ACCESS_KEY")
        or "rustfsadmin"
    )


def s3_secret_key() -> str:
    return (
        os.getenv("TAMOSS_TEST_S3_SECRET_KEY")
        or os.getenv("BUCKET_PASSWORD")
        or os.getenv("TAMOSS_S3_SECRET_KEY")
        or "rustfsadmin"
    )
