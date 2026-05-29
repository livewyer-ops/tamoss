from __future__ import annotations

from tamoss.settings import Settings, StorageBackendSettings

from tests.adapters.bbc.support import PRIMARY_BACKEND_ID, PRIMARY_BACKEND_LABEL


def bbc_parity_settings(**overrides: object) -> Settings:
    values: dict[str, object] = {
        "auth_required": False,
        "public_base_url": "http://testserver",
        "service_name": "TAMOSS BBC parity",
        "service_description": "BBC API parity test instance",
        "service_version": "tamoss-bbc-parity",
        "storage_backend": StorageBackendSettings(
            id=PRIMARY_BACKEND_ID,
            label=PRIMARY_BACKEND_LABEL,
            provider="tamoss",
            region="us-east-1",
            store_product="s3",
            default_storage=True,
            bucket_name="tamoss-primary",
            endpoint_url="https://objects.internal.example.test",
            public_endpoint_url="https://objects.example.test",
            access_key="access",
            secret_key="secret",
        ),
    }
    values.update(overrides)
    return Settings(**values)
