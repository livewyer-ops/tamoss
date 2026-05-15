from __future__ import annotations

from uuid import UUID

from tamoss.adapters.object_storage import ConfiguredObjectStorage
from tamoss.domain.model import StorageBackend
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
            s3_auto_create_bucket=False,
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
