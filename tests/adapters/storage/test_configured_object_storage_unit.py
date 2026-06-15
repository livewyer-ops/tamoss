from __future__ import annotations

import io
import json
import os
from dataclasses import replace
from uuid import UUID

import pytest
from botocore.exceptions import ClientError
from tamoss.adapters.object_storage import ConfiguredObjectStorage
from tamoss.domain.model import ObjectGetUrlRequest, StorageBackend
from tamoss.errors import ConfigurationError
from tamoss.settings import Settings, StorageBackendSettings


def test_s3_clients_are_reused_and_configured_with_connection_pool(
    monkeypatch,
) -> None:
    created_clients: list[dict] = []

    class FakeS3Client:
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
            flow_container="video/mp2t",
            backend=backend,
        )
        storage.write("media/object.ts", b"body", backend=backend)

    assert len(created_clients) == 2
    assert {kwargs["config"].max_pool_connections for kwargs in created_clients} == {17}


def test_runtime_credentials_file_supplies_non_default_backend_credentials(
    monkeypatch,
    tmp_path,
) -> None:
    created_clients: list[dict] = []

    class FakeS3Client:
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
        flow_container="video/mp2t",
        backend=backend,
    )

    assert request["url"] == "https://storage.example.test/presigned"
    assert created_clients[0]["aws_access_key_id"] == "external-access"
    assert created_clients[0]["aws_secret_access_key"] == "external-secret"


def test_build_put_request_uses_flow_container_as_content_type(monkeypatch) -> None:
    presign_params: list[dict] = []

    class FakeS3Client:
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
        flow_container="application/ttml+xml",
        backend=backend,
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


def test_presigned_put_urls_do_not_outlive_allocated_object_timeout(
    monkeypatch,
) -> None:
    presign_calls: list[tuple[str, int]] = []

    class FakeS3Client:
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
        flow_container="video/mp2t",
        backend=backend,
    )
    storage.build_get_urls(object_id="media/object.ts", backend=backend)

    assert presign_calls == [("put_object", 300), ("get_object", 3600)]


def test_runtime_credentials_file_takes_precedence_over_persisted_credentials(
    monkeypatch,
    tmp_path,
) -> None:
    created_clients: list[dict] = []

    class FakeS3Client:
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

    storage.build_get_urls(object_id="media/object.ts", backend=backend)

    assert created_clients[0]["aws_access_key_id"] == "runtime-access"
    assert created_clients[0]["aws_secret_access_key"] == "runtime-secret"


def test_build_get_urls_batch_deduplicates_presigned_requests(monkeypatch) -> None:
    calls: list[tuple[str, str, int]] = []

    class FakeS3Client:
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


def test_delete_batch_deduplicates_and_chunks_s3_requests(monkeypatch) -> None:
    calls: list[dict] = []

    class FakeS3Client:
        def delete_objects(self, **kwargs) -> None:
            calls.append(kwargs)

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
    object_ids = [f"media/{index}.ts" for index in range(1001)]

    storage.delete_batch([*object_ids, object_ids[0]], backend=backend)

    assert [call["Bucket"] for call in calls] == ["tamoss-test", "tamoss-test"]
    assert len(calls[0]["Delete"]["Objects"]) == 1000
    assert calls[0]["Delete"]["Objects"][0] == {"Key": "media/0.ts"}
    assert calls[1]["Delete"]["Objects"] == [{"Key": "media/1000.ts"}]


def test_copy_uses_server_side_copy_for_backends_on_same_endpoint(monkeypatch) -> None:
    calls: list[dict] = []

    class FakeS3Client:
        def copy_object(self, **kwargs) -> None:
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
            "MetadataDirective": "COPY",
        }
    ]


def test_copy_raises_same_endpoint_copy_errors(monkeypatch) -> None:
    get_object_called = False

    class FakeS3Client:
        def copy_object(self, **kwargs) -> None:
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


def test_copy_streams_between_different_s3_endpoints(monkeypatch) -> None:
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
        def get_object(self, **kwargs):
            assert kwargs == {"Bucket": "tamoss-test", "Key": "media/object.ts"}
            return {
                "Body": io.BytesIO(b"copied-bytes"),
                "ContentType": "video/mp2t",
                "Metadata": {"origin": "primary"},
            }

    class DestinationS3Client:
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
            },
        )
    ]


def test_runtime_credentials_file_reloads_on_mtime_change(
    monkeypatch,
    tmp_path,
) -> None:
    created_clients: list[dict] = []
    closed_clients: list[str] = []

    class FakeS3Client:
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

    backend = _external_backend()
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
            storage_backend=_settings_backend(_s3_backend()),
            storage_backend_credentials_file=str(credentials_file),
        )
    )

    storage.build_get_urls(object_id="media/object.ts", backend=backend)
    _write_credentials_file(
        credentials_file,
        backend.id,
        access_key="external-access-2",
        secret_key="external-secret-2",
    )
    os.utime(credentials_file, ns=(2_000_000_000, 2_000_000_000))
    storage.build_get_urls(object_id="media/object.ts", backend=backend)

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

    storage.build_get_urls(object_id="media/object.ts", backend=backend)
    credentials_file.write_text("{not-json", encoding="utf-8")
    os.utime(credentials_file, ns=(3_000_000_000, 3_000_000_000))
    storage.build_get_urls(object_id="media/object-2.ts", backend=backend)

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
        storage.build_get_urls(
            object_id="media/object.ts",
            backend=_external_backend(),
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
    from prometheus_client import REGISTRY

    class FakeS3Client:
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
