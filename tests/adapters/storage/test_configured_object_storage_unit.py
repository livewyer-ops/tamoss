from __future__ import annotations

import base64
import hashlib
import io
import json
import os
from dataclasses import replace
from datetime import UTC, datetime, timedelta
from unittest.mock import Mock
from urllib.parse import parse_qs, urlparse
from uuid import UUID
from xml.etree import ElementTree

import boto3
import botocore.session
import pytest
from botocore.awsrequest import AWSResponse
from botocore.exceptions import ClientError
from botocore.stub import ANY, Stubber
from prometheus_client import REGISTRY
from tamoss.adapters.object_storage import ConfiguredObjectStorage
from tamoss.domain.model import ObjectGetUrlRequest, StorageBackend
from tamoss.errors import ConfigurationError
from tamoss.settings import Settings, StorageBackendSettings


def test_s3_clients_are_reused_and_configured_with_connection_pool(
    monkeypatch,
) -> None:
    created_clients: list[dict] = []

    class FakeS3Client:
        meta = Mock()

        def __init__(self, kwargs: dict) -> None:
            self.kwargs = kwargs

        def generate_presigned_url(self, *args, **kwargs) -> str:
            return "https://storage.example.test/presigned"

        def put_object(self, **kwargs) -> None:
            return None

    def fake_boto3_client(*args, **kwargs):
        created_clients.append(kwargs)
        return FakeS3Client(kwargs)

    monkeypatch.setattr(
        "tamoss.adapters.object_storage.boto3.client", fake_boto3_client
    )

    backend = _s3_backend()
    storage = ConfiguredObjectStorage(
        Settings(
            auth_required=False,
            s3_max_pool_connections=17,
            storage_backend=StorageBackendSettings(
                id=backend.id,
                label=backend.label,
                provider=backend.provider,
                region=backend.region,
                store_product=backend.store_product,
                default_storage=True,
                bucket_name=backend.bucket_name,
                endpoint_url=backend.endpoint_url,
                public_endpoint_url=backend.public_endpoint_url,
                access_key=backend.access_key,
                secret_key=backend.secret_key,
            ),
        )
    )

    for _ in range(2):
        storage.build_put_request(
            object_id="media/object.ts",
            content_type="video/mp2t",
            backend=backend,
            presigned=True,
        )
        storage.write("media/object.ts", b"body", backend=backend)

    assert len(created_clients) == 2
    assert {kwargs["config"].max_pool_connections for kwargs in created_clients} == {17}


@pytest.mark.parametrize("empty_key", [None, ""])
def test_keyless_default_backend_presigns_with_native_web_identity_credentials(
    native_web_identity, empty_key
) -> None:
    _, original, stubber, _, _, role_arn = native_web_identity
    backend = replace(original, access_key=empty_key, secret_key=empty_key)
    storage = ConfiguredObjectStorage(
        Settings(storage_backend=_settings_backend(backend))
    )
    stubber.add_response(
        "assume_role_with_web_identity",
        {
            "Credentials": {
                "AccessKeyId": "ASIAXXXXXXXXXXXXXXXX",
                "SecretAccessKey": "s" * 40,
                "SessionToken": "test-role-session-token",
                "Expiration": datetime.now(UTC) + timedelta(hours=1),
            }
        },
        {
            "RoleArn": role_arn,
            "RoleSessionName": ANY,
            "WebIdentityToken": "original-token",
        },
    )
    put = storage.build_put_request(
        object_id="media/object.ts",
        content_type="video/mp2t",
        backend=backend,
        presigned=True,
    )
    get = storage.build_get_urls_batch(
        [
            ObjectGetUrlRequest(
                object_id="media/object.ts", backend=backend, include_direct=False
            )
        ]
    )[(backend.id, "media/object.ts")][0]
    for request in (put, get):
        query = parse_qs(urlparse(request["url"]).query)
        assert query["X-Amz-Credential"][0].startswith("ASIAXXXXXXXXXXXXXXXX/")
        assert query["X-Amz-Security-Token"] == ["test-role-session-token"]
        assert urlparse(request["url"]).netloc == "storage.public.example.test"
    internal = storage._s3_client(backend)
    assert internal is storage._s3_client(backend)
    assert internal.meta.endpoint_url == backend.endpoint_url
    secondary = replace(
        _external_backend(),
        access_key="secondary-access",
        secret_key="secondary-secret",
    )
    secondary_url = storage.build_get_urls_batch(
        [
            ObjectGetUrlRequest(
                object_id="media/object.ts", backend=secondary, include_direct=False
            )
        ]
    )[(secondary.id, "media/object.ts")][0]["url"]
    query = parse_qs(urlparse(secondary_url).query)
    assert query["X-Amz-Credential"][0].startswith("secondary-access/")
    assert "X-Amz-Security-Token" not in query
    assert urlparse(secondary_url).netloc == "s3.eu-central-003.backblazeb2.com"
    stubber.assert_no_pending_responses()


@pytest.mark.parametrize(
    ("access_key", "secret_key", "default_backend", "configured_keys"),
    [
        ("partial", None, True, False),
        (None, "partial", True, False),
        (None, None, False, False),
        (None, None, True, True),
    ],
)
def test_web_identity_does_not_replace_missing_or_partial_backend_credentials(
    monkeypatch, access_key, secret_key, default_backend, configured_keys
) -> None:
    monkeypatch.setenv("AWS_ROLE_ARN", "arn:aws:iam::123456789012:role/tamoss")
    monkeypatch.setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "/test-token")
    configured = _s3_backend()
    if not configured_keys:
        configured = replace(configured, access_key=None, secret_key=None)
    backend = replace(
        _s3_backend() if default_backend else _external_backend(),
        access_key=access_key,
        secret_key=secret_key,
    )
    storage = ConfiguredObjectStorage(
        Settings(storage_backend=_settings_backend(configured))
    )

    with pytest.raises(ConfigurationError, match="missing S3 endpoint or credentials"):
        storage.build_get_urls_batch(
            [ObjectGetUrlRequest(object_id="media/object.ts", backend=backend)]
        )


def test_runtime_credentials_file_supplies_non_default_backend_credentials(
    monkeypatch,
    tmp_path,
) -> None:
    created_clients: list[dict] = []

    class FakeS3Client:
        meta = Mock()

        def generate_presigned_url(self, *args, **kwargs) -> str:
            return "https://storage.example.test/presigned"

    def fake_boto3_client(*args, **kwargs):
        created_clients.append(kwargs)
        return FakeS3Client()

    monkeypatch.setattr(
        "tamoss.adapters.object_storage.boto3.client", fake_boto3_client
    )

    backend = _external_backend()
    credentials_file = tmp_path / "credentials.json"
    _write_credentials_file(
        credentials_file,
        backend.id,
        access_key="external-access",
        secret_key="external-secret",
    )

    storage = ConfiguredObjectStorage(
        Settings(
            auth_required=False,
            storage_backend=_settings_backend(_s3_backend()),
            storage_backend_credentials_file=str(credentials_file),
        )
    )

    request = storage.build_put_request(
        object_id="media/object.ts",
        content_type="video/mp2t",
        backend=backend,
        presigned=True,
    )

    assert request["url"] == "https://storage.example.test/presigned"
    assert created_clients[0]["aws_access_key_id"] == "external-access"
    assert created_clients[0]["aws_secret_access_key"] == "external-secret"


def test_build_put_request_uses_flow_container_as_content_type(monkeypatch) -> None:
    presign_params: list[dict] = []

    class FakeS3Client:
        meta = Mock()

        def generate_presigned_url(self, *args, **kwargs) -> str:
            presign_params.append(kwargs["Params"])
            return "https://storage.example.test/presigned"

    monkeypatch.setattr(
        "tamoss.adapters.object_storage.boto3.client",
        lambda *args, **kwargs: FakeS3Client(),
    )

    backend = _s3_backend()
    storage = ConfiguredObjectStorage(
        Settings(
            auth_required=False,
            storage_backend=_settings_backend(backend),
        )
    )

    request = storage.build_put_request(
        object_id="media/subtitles.ttml",
        content_type="application/ttml+xml",
        backend=backend,
        presigned=True,
    )

    assert request["content-type"] == "application/ttml+xml"
    assert request["headers"] == {"Content-Type": "application/ttml+xml"}
    assert presign_params == [
        {
            "Bucket": "tamoss-test",
            "Key": "media/subtitles.ttml",
            "ContentType": "application/ttml+xml",
        }
    ]


def test_build_put_request_can_return_direct_public_url_without_presigning(
    monkeypatch,
) -> None:
    def unexpected_client(*_args, **_kwargs):
        raise AssertionError("non-presigned PUT must not construct an S3 client")

    monkeypatch.setattr(
        "tamoss.adapters.object_storage.boto3.client", unexpected_client
    )
    backend = _s3_backend()
    storage = ConfiguredObjectStorage(
        Settings(
            auth_required=False,
            storage_backend=_settings_backend(backend),
        )
    )

    request = storage.build_put_request(
        object_id="media/segment 01.m4s",
        content_type="video/iso.segment",
        backend=backend,
        presigned=False,
    )

    assert request == {
        "url": (
            "https://storage.public.example.test/tamoss-test/media/segment%2001.m4s"
        ),
        "content-type": "video/iso.segment",
        "headers": {"Content-Type": "video/iso.segment"},
    }


def test_presigned_put_urls_do_not_outlive_allocated_object_timeout(
    monkeypatch,
) -> None:
    presign_calls: list[tuple[str, int]] = []

    class FakeS3Client:
        meta = Mock()

        def generate_presigned_url(self, *args, **kwargs) -> str:
            presign_calls.append((args[0], kwargs["ExpiresIn"]))
            return f"https://storage.example.test/{args[0]}"

    monkeypatch.setattr(
        "tamoss.adapters.object_storage.boto3.client",
        lambda *args, **kwargs: FakeS3Client(),
    )

    backend = _s3_backend()
    storage = ConfiguredObjectStorage(
        Settings(
            auth_required=False,
            min_object_timeout="300:0",
            min_presigned_url_timeout="30:0",
            s3_presign_ttl_seconds=3600,
            storage_backend=_settings_backend(backend),
        )
    )

    storage.build_put_request(
        object_id="media/object.ts",
        content_type="video/mp2t",
        backend=backend,
        presigned=True,
    )
    storage.build_get_urls_batch(
        [ObjectGetUrlRequest(object_id="media/object.ts", backend=backend)]
    )

    assert presign_calls == [("put_object", 300), ("get_object", 3600)]


def test_runtime_credentials_file_takes_precedence_over_persisted_credentials(
    monkeypatch,
    tmp_path,
) -> None:
    created_clients: list[dict] = []

    class FakeS3Client:
        meta = Mock()

        def generate_presigned_url(self, *args, **kwargs) -> str:
            return "https://storage.example.test/presigned"

    def fake_boto3_client(*args, **kwargs):
        created_clients.append(kwargs)
        return FakeS3Client()

    monkeypatch.setattr(
        "tamoss.adapters.object_storage.boto3.client", fake_boto3_client
    )

    backend = _external_backend()
    backend.access_key = "persisted-access"
    backend.secret_key = "persisted-secret"
    credentials_file = tmp_path / "credentials.json"
    _write_credentials_file(
        credentials_file,
        backend.id,
        access_key="runtime-access",
        secret_key="runtime-secret",
    )

    storage = ConfiguredObjectStorage(
        Settings(
            auth_required=False,
            storage_backend=_settings_backend(_s3_backend()),
            storage_backend_credentials_file=str(credentials_file),
        )
    )

    storage.build_get_urls_batch(
        [ObjectGetUrlRequest(object_id="media/object.ts", backend=backend)]
    )

    assert created_clients[0]["aws_access_key_id"] == "runtime-access"
    assert created_clients[0]["aws_secret_access_key"] == "runtime-secret"


def test_build_get_urls_batch_deduplicates_presigned_requests(monkeypatch) -> None:
    calls: list[tuple[str, str, int]] = []

    class FakeS3Client:
        meta = Mock()

        def generate_presigned_url(self, *args, **kwargs) -> str:
            calls.append(
                (
                    kwargs["Params"]["Bucket"],
                    kwargs["Params"]["Key"],
                    kwargs["ExpiresIn"],
                )
            )
            return f"https://storage.example.test/{kwargs['Params']['Key']}"

    monkeypatch.setattr(
        "tamoss.adapters.object_storage.boto3.client",
        lambda *args, **kwargs: FakeS3Client(),
    )

    backend = _s3_backend()
    storage = ConfiguredObjectStorage(
        Settings(
            auth_required=False,
            s3_presign_ttl_seconds=90,
            storage_backend=_settings_backend(backend),
        )
    )

    result = storage.build_get_urls_batch(
        [
            ObjectGetUrlRequest(object_id="media/a.ts", backend=backend),
            ObjectGetUrlRequest(object_id="media/a.ts", backend=backend),
            ObjectGetUrlRequest(object_id="media/b.ts", backend=backend),
        ]
    )

    assert set(result) == {
        (backend.id, "media/a.ts"),
        (backend.id, "media/b.ts"),
    }
    assert sorted(call[1] for call in calls) == ["media/a.ts", "media/b.ts"]
    assert {call[2] for call in calls} == {90}


def test_build_get_urls_batch_skips_presign_when_only_direct_urls_requested(
    monkeypatch,
) -> None:
    calls: list[str] = []

    class FakeS3Client:
        meta = Mock()

        def generate_presigned_url(self, *args, **kwargs) -> str:
            calls.append(kwargs["Params"]["Key"])
            return "https://storage.example.test/presigned"

    monkeypatch.setattr(
        "tamoss.adapters.object_storage.boto3.client",
        lambda *args, **kwargs: FakeS3Client(),
    )

    backend = _s3_backend()
    storage = ConfiguredObjectStorage(
        Settings(
            auth_required=False,
            storage_backend=_settings_backend(backend),
        )
    )

    result = storage.build_get_urls_batch(
        [
            ObjectGetUrlRequest(
                object_id="media/a.ts",
                backend=backend,
                include_direct=True,
                include_presigned=False,
            )
        ]
    )

    assert calls == []
    assert result[(backend.id, "media/a.ts")] == [
        {
            "url": "https://storage.public.example.test/tamoss-test/media/a.ts",
            "label": backend.label,
            "presigned": False,
        }
    ]


@pytest.fixture()
def captured_s3_requests(monkeypatch):
    backend = _s3_backend()
    storage = ConfiguredObjectStorage(
        Settings(auth_required=False, storage_backend=_settings_backend(backend))
    )
    requests = []

    def send(request):
        requests.append(request)
        return AWSResponse(
            request.url,
            200,
            {},
            Mock(stream=lambda: iter([b"<DeleteResult/>"])),
        )

    client = storage._s3_client(backend)
    monkeypatch.setattr(client._endpoint.http_session, "send", send)
    yield storage, requests
    client.close()


def test_delete_batch_deduplicates_and_chunks_signed_sha256_requests(
    captured_s3_requests,
) -> None:
    storage, requests = captured_s3_requests
    object_ids = [f"media/{index}.ts" for index in range(1001)]

    storage.delete_batch([*object_ids, object_ids[0]])

    assert len(requests) == 2
    batches = []
    for request in requests:
        headers = {key.lower(): value for key, value in request.headers.items()}
        assert headers["x-amz-checksum-sha256"] == base64.b64encode(
            hashlib.sha256(request.body).digest()
        )
        assert b"x-amz-checksum-sha256" in headers["authorization"]
        assert urlparse(request.url).path == "/tamoss-test"
        batches.append(
            [
                node.text
                for node in ElementTree.fromstring(request.body).findall(
                    "./{*}Object/{*}Key"
                )
            ]
        )
    assert batches == [object_ids[:1000], object_ids[1000:]]


def test_write_sends_signed_payload_without_optional_checksum_trailer(
    captured_s3_requests,
) -> None:
    storage, requests = captured_s3_requests
    body = b"media bytes"

    storage.write("media/object.ts", body)

    assert len(requests) == 1
    request = requests[0]
    headers = {key.lower(): value for key, value in request.headers.items()}
    assert "content-encoding" not in headers
    assert "x-amz-trailer" not in headers
    assert "transfer-encoding" not in headers
    assert headers["content-length"] == str(len(body))
    assert headers["x-amz-content-sha256"] == hashlib.sha256(body).hexdigest().encode()
    assert request.body.read() == body


@pytest.mark.parametrize("operation", ["put_object", "upload_part"])
def test_explicit_upload_checksum_uses_header_without_consuming_body(
    captured_s3_requests, operation
) -> None:
    storage, requests = captured_s3_requests
    body = b"media with an explicit checksum"
    client = storage._s3_client(_s3_backend())
    params = (
        {"UploadId": "upload", "PartNumber": 1} if operation == "upload_part" else {}
    )

    getattr(client, operation)(
        Bucket="tamoss-test",
        Key="media/object.ts",
        Body=io.BytesIO(body),
        ChecksumAlgorithm="SHA256",
        **params,
    )

    assert len(requests) == 1
    request = requests[0]
    headers = {key.lower(): value for key, value in request.headers.items()}
    assert "content-encoding" not in headers
    assert "x-amz-trailer" not in headers
    assert "transfer-encoding" not in headers
    assert headers["x-amz-checksum-sha256"] == base64.b64encode(
        hashlib.sha256(body).digest()
    )
    assert b"x-amz-checksum-sha256" in headers["authorization"]
    assert request.body.read() == body


def test_copy_uses_server_side_copy_for_backends_on_same_endpoint(monkeypatch) -> None:
    calls: list[dict] = []

    class FakeS3Client:
        meta = Mock()

        def copy(self, **kwargs) -> None:
            assert isinstance(kwargs.pop("SourceClient"), FakeS3Client)
            calls.append(kwargs)

    monkeypatch.setattr(
        "tamoss.adapters.object_storage.boto3.client",
        lambda *args, **kwargs: FakeS3Client(),
    )

    source = _s3_backend()
    destination = replace(
        source,
        id=UUID("77777777-7777-4777-8777-777777777777"),
        bucket_name="tamoss-copy",
    )
    storage = ConfiguredObjectStorage(
        Settings(
            auth_required=False,
            storage_backend=_settings_backend(source),
        )
    )

    storage.copy(
        "media/object.ts",
        source_backend=source,
        destination_backend=destination,
    )

    assert calls == [
        {
            "Bucket": "tamoss-copy",
            "Key": "media/object.ts",
            "CopySource": {
                "Bucket": "tamoss-test",
                "Key": "media/object.ts",
            },
            "ExtraArgs": {"MetadataDirective": "COPY"},
        }
    ]


def test_copy_raises_same_endpoint_copy_errors(monkeypatch) -> None:
    get_object_called = False

    class FakeS3Client:
        meta = Mock()

        def copy(self, **kwargs) -> None:
            raise ClientError(
                {
                    "Error": {
                        "Code": "AccessDenied",
                        "Message": "copy denied",
                    }
                },
                "CopyObject",
            )

        def get_object(self, **kwargs):
            nonlocal get_object_called
            get_object_called = True
            return {"Body": io.BytesIO(b"should-not-stream")}

    monkeypatch.setattr(
        "tamoss.adapters.object_storage.boto3.client",
        lambda *args, **kwargs: FakeS3Client(),
    )

    source = _s3_backend()
    destination = replace(
        source,
        id=UUID("77777777-7777-4777-8777-777777777777"),
        bucket_name="tamoss-copy",
    )
    storage = ConfiguredObjectStorage(
        Settings(
            auth_required=False,
            storage_backend=_settings_backend(source),
        )
    )

    with pytest.raises(ClientError):
        storage.copy(
            "media/object.ts",
            source_backend=source,
            destination_backend=destination,
        )

    assert get_object_called is False


@pytest.mark.parametrize("deleted", [[], [{"Key": "first"}]])
def test_partial_batch_deletion_fails_and_can_be_retried(monkeypatch, deleted) -> None:
    client = Mock()
    client.delete_objects.side_effect = [
        {"Deleted": deleted, "Errors": [{"Key": "second", "Code": "AccessDenied"}]},
        {"Deleted": [{"Key": "first"}, {"Key": "second"}]},
    ]
    backend = _s3_backend()
    storage = ConfiguredObjectStorage(
        Settings(auth_required=False, storage_backend=_settings_backend(backend))
    )
    monkeypatch.setattr(storage, "_s3_client", lambda _: client)
    with pytest.raises(RuntimeError, match="incomplete"):
        storage.delete_batch(["first", "second"], backend=backend)
    storage.delete_batch(["first", "second"], backend=backend)
    assert client.delete_objects.call_count == 2


@pytest.mark.parametrize(
    "algorithm", [None, "SHA256", "SHA1", "CRC64NVME", "CRC32C", "CRC32"]
)
def test_copy_streams_between_different_s3_endpoints(monkeypatch, algorithm) -> None:
    uploaded: list[tuple[str, str, bytes, dict | None]] = []
    source = _s3_backend()
    destination = replace(
        _s3_backend(),
        id=UUID("77777777-7777-4777-8777-777777777777"),
        bucket_name="tamoss-copy",
        endpoint_url="https://storage-copy.internal.example.test",
        public_endpoint_url="https://storage-copy.public.example.test",
    )

    class SourceS3Client:
        meta = Mock()

        def get_object(self, **kwargs):
            assert kwargs == {
                "Bucket": "tamoss-test",
                "Key": "media/object.ts",
                "ChecksumMode": "ENABLED",
            }
            return {
                "Body": io.BytesIO(b"copied-bytes"),
                "ContentType": "video/mp2t",
                "Metadata": {"origin": "primary"},
                **({f"Checksum{algorithm}": "source-checksum"} if algorithm else {}),
            }

    class DestinationS3Client:
        meta = Mock()

        def upload_fileobj(self, body, bucket, key, **kwargs) -> None:
            uploaded.append((bucket, key, body.read(), kwargs.get("ExtraArgs")))

    def fake_boto3_client(*args, **kwargs):
        if kwargs["endpoint_url"] == source.endpoint_url:
            return SourceS3Client()
        return DestinationS3Client()

    monkeypatch.setattr(
        "tamoss.adapters.object_storage.boto3.client",
        fake_boto3_client,
    )

    storage = ConfiguredObjectStorage(
        Settings(
            auth_required=False,
            storage_backend=_settings_backend(source),
        )
    )

    storage.copy(
        "media/object.ts",
        source_backend=source,
        destination_backend=destination,
    )

    assert uploaded == [
        (
            "tamoss-copy",
            "media/object.ts",
            b"copied-bytes",
            {
                "ContentType": "video/mp2t",
                "Metadata": {"origin": "primary"},
                **({"ChecksumAlgorithm": algorithm} if algorithm else {}),
            },
        )
    ]


@pytest.mark.parametrize("use_web_identity", [False, True])
@pytest.mark.parametrize("default_backend", [False, True])
def test_runtime_credentials_file_reloads_on_mtime_change(
    monkeypatch,
    tmp_path,
    use_web_identity,
    default_backend,
) -> None:
    configured = _s3_backend()
    if use_web_identity:
        monkeypatch.setenv("AWS_ROLE_ARN", "arn:aws:iam::123456789012:role/tamoss")
        monkeypatch.setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "/test-token")
        configured = replace(configured, access_key=None, secret_key=None)
    created_clients: list[dict] = []
    closed_clients: list[str] = []

    class FakeS3Client:
        meta = Mock()

        def __init__(self, access_key: str) -> None:
            self.access_key = access_key

        def generate_presigned_url(self, *args, **kwargs) -> str:
            return "https://storage.example.test/presigned"

        def close(self) -> None:
            closed_clients.append(self.access_key)

    def fake_boto3_client(*args, **kwargs):
        created_clients.append(kwargs)
        return FakeS3Client(kwargs["aws_access_key_id"])

    monkeypatch.setattr(
        "tamoss.adapters.object_storage.boto3.client", fake_boto3_client
    )

    backend = (
        replace(_s3_backend(), access_key=None, secret_key=None)
        if default_backend
        else _external_backend()
    )
    credentials_file = tmp_path / "credentials.json"
    _write_credentials_file(
        credentials_file,
        backend.id,
        access_key="external-access-1",
        secret_key="external-secret-1",
    )

    storage = ConfiguredObjectStorage(
        Settings(
            auth_required=False,
            storage_backend=_settings_backend(configured),
            storage_backend_credentials_file=str(credentials_file),
        )
    )

    storage.build_get_urls_batch(
        [ObjectGetUrlRequest(object_id="media/object.ts", backend=backend)]
    )
    _write_credentials_file(
        credentials_file,
        backend.id,
        access_key="external-access-2",
        secret_key="external-secret-2",
    )
    os.utime(credentials_file, ns=(2_000_000_000, 2_000_000_000))
    storage.build_get_urls_batch(
        [ObjectGetUrlRequest(object_id="media/object.ts", backend=backend)]
    )

    assert [item["aws_access_key_id"] for item in created_clients] == [
        "external-access-1",
        "external-access-2",
    ]
    assert closed_clients == ["external-access-1"]


def test_check_backend_uses_head_bucket_without_creating_bucket(
    monkeypatch,
) -> None:
    calls: list[tuple[str, dict]] = []

    class FakeS3Client:
        meta = Mock()

        def head_bucket(self, **kwargs) -> None:
            calls.append(("head_bucket", kwargs))

        def create_bucket(self, **kwargs) -> None:
            calls.append(("create_bucket", kwargs))

    monkeypatch.setattr(
        "tamoss.adapters.object_storage.boto3.client",
        lambda *args, **kwargs: FakeS3Client(),
    )

    backend = _s3_backend()
    storage = ConfiguredObjectStorage(
        Settings(
            auth_required=False,
            storage_backend=_settings_backend(backend),
        )
    )

    storage.check_backend(backend)

    assert calls == [("head_bucket", {"Bucket": "tamoss-test"})]


def test_invalid_runtime_credentials_file_keeps_previous_valid_credentials(
    monkeypatch,
    tmp_path,
) -> None:
    created_clients: list[dict] = []

    class FakeS3Client:
        meta = Mock()

        def generate_presigned_url(self, *args, **kwargs) -> str:
            return "https://storage.example.test/presigned"

    def fake_boto3_client(*args, **kwargs):
        created_clients.append(kwargs)
        return FakeS3Client()

    monkeypatch.setattr(
        "tamoss.adapters.object_storage.boto3.client", fake_boto3_client
    )

    backend = _external_backend()
    credentials_file = tmp_path / "credentials.json"
    _write_credentials_file(
        credentials_file,
        backend.id,
        access_key="external-access",
        secret_key="external-secret",
    )

    storage = ConfiguredObjectStorage(
        Settings(
            auth_required=False,
            storage_backend=_settings_backend(_s3_backend()),
            storage_backend_credentials_file=str(credentials_file),
        )
    )

    storage.build_get_urls_batch(
        [ObjectGetUrlRequest(object_id="media/object.ts", backend=backend)]
    )
    credentials_file.write_text("{not-json", encoding="utf-8")
    os.utime(credentials_file, ns=(3_000_000_000, 3_000_000_000))
    storage.build_get_urls_batch(
        [ObjectGetUrlRequest(object_id="media/object-2.ts", backend=backend)]
    )

    assert len(created_clients) == 1
    assert created_clients[0]["aws_access_key_id"] == "external-access"


def test_missing_runtime_credentials_raises_clear_configuration_error() -> None:
    storage = ConfiguredObjectStorage(
        Settings(
            auth_required=False,
            storage_backend=_settings_backend(_s3_backend()),
        )
    )

    with pytest.raises(ConfigurationError, match="missing S3 endpoint or credentials"):
        storage.build_get_urls_batch(
            [
                ObjectGetUrlRequest(
                    object_id="media/object.ts", backend=_external_backend()
                )
            ]
        )


def _s3_backend() -> StorageBackend:
    return StorageBackend(
        id=UUID("55555555-5555-4555-8555-555555555555"),
        label="tamoss.storage.primary",
        provider="tamoss",
        region="us-east-1",
        store_product="s3",
        default_storage=True,
        bucket_name="tamoss-test",
        endpoint_url="https://storage.internal.example.test",
        public_endpoint_url="https://storage.public.example.test",
        access_key="access",
        secret_key="secret",
    )


def _external_backend() -> StorageBackend:
    return StorageBackend(
        id=UUID("66666666-6666-4666-8666-666666666666"),
        label="tamoss.external",
        provider="external-s3",
        region="eu-central-003",
        store_product="s3",
        bucket_name="tamoss-external",
        endpoint_url="https://s3.eu-central-003.backblazeb2.com",
        public_endpoint_url="https://s3.eu-central-003.backblazeb2.com",
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


def _write_credentials_file(
    path,
    storage_backend_id: UUID,
    *,
    access_key: str,
    secret_key: str,
) -> None:
    path.write_text(
        json.dumps(
            {
                "apiVersion": "tamoss.livewyer.io/v1",
                "kind": "StorageBackendCredentials",
                "credentials": [
                    {
                        "storageBackendId": str(storage_backend_id),
                        "accessKey": access_key,
                        "secretKey": secret_key,
                    }
                ],
            }
        ),
        encoding="utf-8",
    )


def test_presigned_get_urls_increment_media_metric(monkeypatch) -> None:
    class FakeS3Client:
        meta = Mock()

        def generate_presigned_url(self, *args, **kwargs) -> str:
            return f"https://storage.example.test/{kwargs['Params']['Key']}"

    monkeypatch.setattr(
        "tamoss.adapters.object_storage.boto3.client",
        lambda *args, **kwargs: FakeS3Client(),
    )
    backend = _s3_backend()
    storage = ConfiguredObjectStorage(
        Settings(auth_required=False, storage_backend=_settings_backend(backend))
    )
    metric = "tamoss_presigned_urls_generated_total"
    before = REGISTRY.get_sample_value(metric, {"operation": "get"}) or 0.0

    storage.build_get_urls_batch(
        [
            ObjectGetUrlRequest(object_id="media/a.ts", backend=backend),
            ObjectGetUrlRequest(object_id="media/b.ts", backend=backend),
        ]
    )

    after = REGISTRY.get_sample_value(metric, {"operation": "get"}) or 0.0
    assert after - before == 2


@pytest.fixture
def native_web_identity(monkeypatch, tmp_path):
    token_file = tmp_path / "identity-token"
    token_file.write_text("original-token")
    role = "arn:aws:iam::123456789012:role/tamoss"
    for name in (
        "AWS_ACCESS_KEY_ID",
        "AWS_SECRET_ACCESS_KEY",
        "AWS_SESSION_TOKEN",
        "AWS_PROFILE",
    ):
        monkeypatch.delenv(name, raising=False)
    for name, value in {
        "AWS_ROLE_ARN": role,
        "AWS_WEB_IDENTITY_TOKEN_FILE": str(token_file),
        "AWS_CONFIG_FILE": "/dev/null",
        "AWS_SHARED_CREDENTIALS_FILE": "/dev/null",
    }.items():
        monkeypatch.setenv(name, value)
    sdk = botocore.session.Session()
    sts = sdk.create_client(
        "sts",
        region_name="us-east-1",
        aws_access_key_id="test",
        aws_secret_access_key="test",
    )
    create_client = sdk.create_client
    monkeypatch.setattr(
        sdk,
        "create_client",
        lambda service_name, **kwargs: (
            sts if service_name == "sts" else create_client(service_name, **kwargs)
        ),
    )
    session = boto3.Session(botocore_session=sdk)
    monkeypatch.setattr("tamoss.adapters.object_storage.boto3.client", session.client)
    backend = replace(_s3_backend(), access_key=None, secret_key=None)
    storage = ConfiguredObjectStorage(
        Settings(storage_backend=_settings_backend(backend))
    )
    with Stubber(sts) as stubber:
        yield storage, backend, stubber, token_file, sdk, role
        stubber.assert_no_pending_responses()


@pytest.mark.parametrize(
    "error_code", ["InvalidIdentityToken", "AccessDenied", "ExpiredToken"]
)
def test_native_web_identity_errors_do_not_fall_back_to_static_credentials(
    native_web_identity, error_code
):
    storage, backend, stubber, _, _, role = native_web_identity
    stubber.add_client_error(
        "assume_role_with_web_identity",
        service_error_code=error_code,
        http_status_code=403,
        expected_params={
            "RoleArn": role,
            "RoleSessionName": ANY,
            "WebIdentityToken": "original-token",
        },
    )
    with pytest.raises(ClientError) as error:
        storage.build_put_request(
            object_id="object",
            content_type="video/mp2t",
            backend=backend,
            presigned=True,
        )
    assert error.value.response["Error"]["Code"] == error_code


def test_native_web_identity_refresh_reads_rotated_token(
    native_web_identity, monkeypatch
):
    storage, backend, stubber, token_file, sdk, role = native_web_identity
    initial = datetime.now(UTC)
    for index, token in enumerate(("original-token", "rotated-token")):
        stubber.add_response(
            "assume_role_with_web_identity",
            {
                "Credentials": {
                    "AccessKeyId": f"ASIAXXXXXXXXXXXXXXX{index}",
                    "SecretAccessKey": "s" * 40,
                    "SessionToken": f"session-{index}",
                    "Expiration": initial + timedelta(hours=index + 1),
                }
            },
            {"RoleArn": role, "RoleSessionName": ANY, "WebIdentityToken": token},
        )
    first = storage.build_put_request(
        object_id="object", content_type="video/mp2t", backend=backend, presigned=True
    )
    assert parse_qs(urlparse(first["url"]).query)["X-Amz-Security-Token"] == [
        "session-0"
    ]
    client = storage._s3_presign_client(backend)
    token_file.write_text("rotated-token")
    later = initial + timedelta(hours=1, seconds=1)
    monkeypatch.setattr(sdk.get_credentials(), "_time_fetcher", lambda: later)
    monkeypatch.setattr("botocore.credentials._local_now", lambda: later)
    second = storage.build_put_request(
        object_id="object", content_type="video/mp2t", backend=backend, presigned=True
    )
    assert parse_qs(urlparse(second["url"]).query)["X-Amz-Security-Token"] == [
        "session-1"
    ]
    assert storage._s3_presign_client(backend) is client
