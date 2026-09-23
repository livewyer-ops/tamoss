from __future__ import annotations

from collections.abc import Iterator
from dataclasses import replace
from urllib.parse import unquote, urlparse
from uuid import UUID, uuid4

import pytest
import requests
from botocore.exceptions import BotoCoreError, ClientError
from tamoss.adapters.object_storage import ConfiguredObjectStorage
from tamoss.domain.model import ObjectGetUrlRequest, StorageBackend
from tamoss.settings import Settings

from tests.support.s3_storage import (
    checksum_value,
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

    get_urls = object_storage.build_get_urls_batch(
        [ObjectGetUrlRequest(object_id=object_id, backend=s3_backend)]
    )[(s3_backend.id, object_id)]
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


def test_signed_reads_return_native_checksums_for_mixed_objects(
    object_storage: ConfiguredObjectStorage,
    s3_backend: StorageBackend,
) -> None:
    body = b"media with stored checksums\n"
    client = s3_client(s3_backend)
    algorithms = ("SHA256", "SHA1")
    for algorithm in algorithms:
        client.put_object(
            Bucket=s3_backend.bucket_name,
            Key=algorithm,
            Body=body,
            **{f"Checksum{algorithm}": checksum_value(body, algorithm.lower())},
        )

    urls = object_storage.build_get_urls_batch(
        ObjectGetUrlRequest(object_id=algorithm, backend=s3_backend)
        for algorithm in algorithms
    )
    for algorithm in algorithms:
        expected = checksum_value(body, algorithm.lower())
        metadata = client.head_object(
            Bucket=s3_backend.bucket_name, Key=algorithm, ChecksumMode="ENABLED"
        )
        assert metadata[f"Checksum{algorithm}"] == expected
        get_url = next(
            item["url"]
            for item in urls[(s3_backend.id, algorithm)]
            if item["presigned"]
        )
        response = requests.get(get_url, timeout=5)
        assert response.status_code == 200, response.text
        assert response.content == body
        checked = client.get_object(
            Bucket=s3_backend.bucket_name, Key=algorithm, ChecksumMode="ENABLED"
        )
        assert checked[f"Checksum{algorithm}"] == expected
        assert checked["Body"].read() == body

        bad_key = f"invalid-{algorithm}"
        with pytest.raises(ClientError) as error:
            client.put_object(
                Bucket=s3_backend.bucket_name,
                Key=bad_key,
                Body=body,
                **{f"Checksum{algorithm}": checksum_value(b"wrong", algorithm.lower())},
            )
        assert error.value.response["Error"]["Code"] == "BadDigest"
        assert object_storage.object_metadata(bad_key, backend=s3_backend) is None


def test_streamed_copy_preserves_source_checksum_algorithms(
    object_storage: ConfiguredObjectStorage,
    s3_backend: StorageBackend,
) -> None:
    assert s3_backend.endpoint_url is not None
    source = replace(s3_backend, endpoint_url=s3_backend.endpoint_url.rstrip("/"))
    # Distinct endpoint URLs exercise streaming with the same real S3 fixture.
    destination = replace(
        source,
        id=uuid4(),
        bucket_name=f"tamoss-checksum-copy-{uuid4().hex[:12]}",
        endpoint_url=f"{source.endpoint_url}/",
    )
    ensure_bucket(destination)
    body = b"copy media and its checksum algorithm\n"
    try:
        for algorithm in ("SHA256", "SHA1"):
            checksum = checksum_value(body, algorithm.lower())
            s3_client(source).put_object(
                Bucket=source.bucket_name,
                Key=algorithm,
                Body=body,
                ContentType="video/mp2t",
                Metadata={"origin": "source"},
                **{f"Checksum{algorithm}": checksum},
            )
            object_storage.copy(
                algorithm, source_backend=source, destination_backend=destination
            )
            response = s3_client(destination).get_object(
                Bucket=destination.bucket_name,
                Key=algorithm,
                ChecksumMode="ENABLED",
            )
            assert response["Body"].read() == body
            assert response[f"Checksum{algorithm}"] == checksum
            assert response["ContentType"] == "video/mp2t"
            assert response["Metadata"] == {"origin": "source"}
    finally:
        empty_and_delete_bucket(destination)


def test_managed_multipart_copy_preserves_bytes_and_metadata(
    object_storage, s3_backend
):
    destination = replace(
        s3_backend, id=uuid4(), bucket_name=f"tamoss-copy-{uuid4().hex[:12]}"
    )
    ensure_bucket(destination)
    body = bytes(range(256)) * (64 * 1024)
    object_id = "media/large.ts"
    source_client = s3_client(s3_backend)
    source_client.put_object(
        Bucket=s3_backend.bucket_name,
        Key=object_id,
        Body=body,
        ContentType="video/mp2t",
        Metadata={"origin": "source"},
    )
    parts = []
    destination_client = object_storage._s3_client(destination)
    destination_client.meta.events.register(
        "before-call.s3.UploadPartCopy", lambda **kwargs: parts.append(kwargs)
    )
    try:
        object_storage.copy(
            object_id, source_backend=s3_backend, destination_backend=destination
        )
        assert len(parts) >= 2
        assert object_storage.read(object_id, backend=destination) == body
        metadata = destination_client.head_object(
            Bucket=destination.bucket_name, Key=object_id
        )
        assert metadata["ContentType"] == "video/mp2t"
        assert metadata["Metadata"] == {"origin": "source"}
    finally:
        empty_and_delete_bucket(destination)


@pytest.mark.tamoss_security
def test_rustfs_presigned_checksum_headers_require_signing(
    s3_backend: StorageBackend,
) -> None:
    client = s3_client(s3_backend)
    body = b"signed checksum evidence\n"
    checksum = checksum_value(body, "sha256")
    params = {"Bucket": s3_backend.bucket_name, "Key": "signed"}
    put_url = client.generate_presigned_url(
        "put_object", Params={**params, "ChecksumSHA256": checksum}, ExpiresIn=120
    )
    uploaded = requests.put(
        put_url, data=body, headers={"x-amz-checksum-sha256": checksum}, timeout=5
    )
    assert uploaded.status_code == 200, uploaded.text

    for operation, method in (
        ("get_object", requests.get),
        ("head_object", requests.head),
    ):
        checked_url = client.generate_presigned_url(
            operation, Params={**params, "ChecksumMode": "ENABLED"}, ExpiresIn=120
        )
        checked = method(
            checked_url, headers={"x-amz-checksum-mode": "ENABLED"}, timeout=5
        )
        assert checked.status_code == 200, checked.text
        assert checked.headers["x-amz-checksum-sha256"] == checksum
        if operation == "get_object":
            assert checked.content == body

        plain_url = client.generate_presigned_url(
            operation, Params=params, ExpiresIn=120
        )
        denied = method(
            plain_url, headers={"x-amz-checksum-mode": "ENABLED"}, timeout=5
        )
        assert denied.status_code == 403, denied.text
        assert method(plain_url, timeout=5).status_code == 200

    for name, value in (
        ("x-amz-checksum-sha256", checksum),
        ("x-amz-checksum-sha1", checksum_value(body, "sha1")),
        ("x-amz-meta-unapproved", "probe"),
        ("x-amz-tagging", "probe=true"),
        ("x-amz-acl", "private"),
    ):
        put_url = client.generate_presigned_url(
            "put_object",
            Params={"Bucket": s3_backend.bucket_name, "Key": name},
            ExpiresIn=120,
        )
        denied = requests.put(put_url, data=body, headers={name: value}, timeout=5)
        assert denied.status_code == 403, denied.text
        assert "AccessDenied" in denied.text

    stored = client.list_objects_v2(Bucket=s3_backend.bucket_name)
    assert [item["Key"] for item in stored["Contents"]] == ["signed"]
