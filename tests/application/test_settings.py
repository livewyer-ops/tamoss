from __future__ import annotations

import pytest
from tamoss.settings import Settings, read_secret_file


@pytest.mark.parametrize(
    ("name", "value", "message"),
    [
        ("TAMOSS_WORKER_MAX_REQUESTS", "many", "positive integer"),
        ("TAMOSS_WORKER_LEASE_SECONDS", "0", "positive integer"),
        (
            "TAMOSS_WORKER_HEALTH_STALE_AFTER_SECONDS",
            "29",
            "TAMOSS_WORKER_HEALTH_STALE_AFTER_SECONDS",
        ),
        ("TAMOSS_WEBHOOK_TIMEOUT_SECONDS", "-1", "positive number"),
        ("TAMOSS_WORKER_ENABLE_DELETE", "sometimes", "boolean"),
        ("TAMOSS_STORAGE_BACKEND_REGISTRATION_ENABLED", "sometimes", "boolean"),
        ("TAMOSS_OAUTH2_ISSUER", "auth.example.test", "TAMOSS_OAUTH2_ISSUER"),
        (
            "TAMOSS_CORS_ALLOWED_ORIGINS",
            "https://tool.example.test/path",
            "TAMOSS_CORS_ALLOWED_ORIGINS",
        ),
        ("TAMOSS_S3_ENDPOINT", "objects.internal", "TAMOSS_S3_ENDPOINT"),
        ("TAMOSS_MIN_OBJECT_TIMEOUT", "299:0", "min_object_timeout"),
        (
            "TAMOSS_MIN_PRESIGNED_URL_TIMEOUT",
            "29:0",
            "min_presigned_url_timeout",
        ),
        ("TAMOSS_S3_PRESIGN_TTL", "29", "TAMOSS_S3_PRESIGN_TTL"),
    ],
)
def test_settings_reject_invalid_runtime_env(
    monkeypatch: pytest.MonkeyPatch, name: str, value: str, message: str
) -> None:
    monkeypatch.setenv(name, value)

    with pytest.raises(ValueError, match=message):
        Settings()


def test_operator_rendered_runtime_env_values_parse(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    operator_env = {
        "TAMOSS_S3_CONNECT_TIMEOUT_SECONDS": "1",
        "TAMOSS_S3_READ_TIMEOUT_SECONDS": "2",
        "TAMOSS_WEBHOOK_ALLOWED_HOSTS": ".svc.cluster.local",
        "TAMOSS_WORKER_POLL_INTERVAL_SECONDS": "1",
        "TAMOSS_WORKER_MAX_REQUESTS": "25",
        "TAMOSS_WORKER_HEALTH_STALE_AFTER_SECONDS": "900",
        "TAMOSS_STORAGE_BACKEND_REGISTRATION_ENABLED": "false",
        "TAMOSS_OAUTH2_ENABLED": "true",
        "TAMOSS_OAUTH2_ISSUER": "https://auth.example.test/application/o/tamoss/",
        "TAMOSS_OAUTH2_JWKS_URI": "http://authentik.auth.svc:9000/application/o/tamoss/jwks/",
        "TAMOSS_OAUTH2_ALGORITHMS": "RS256,PS256",
        "TAMOSS_CORS_ALLOWED_ORIGINS": "https://cuttingroom.github.io, https://app.example.test/",
    }
    for name, value in operator_env.items():
        monkeypatch.setenv(name, value)

    settings = Settings()

    assert settings.s3_connect_timeout_seconds == 1
    assert settings.s3_read_timeout_seconds == 2
    assert settings.webhook_allowed_hosts == [".svc.cluster.local"]
    assert settings.worker_poll_interval_seconds == 1
    assert settings.worker_max_requests == 25
    assert settings.worker_health_stale_after_seconds == 900
    assert settings.storage_backend_registration_enabled is False
    assert settings.oauth2_enabled is True
    assert settings.oauth2_algorithms == ["RS256", "PS256"]
    assert settings.cors_allowed_origins == [
        "https://cuttingroom.github.io",
        "https://app.example.test",
    ]


def test_local_kind_runtime_env_values_parse(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    local_kind_env = {
        "POSTGRES_HOST": "tamoss-kind-rw.tams.svc.cluster.local",
        "POSTGRES_PORT": "5432",
        "POSTGRES_DB": "tamoss",
        "POSTGRES_USER": "tamoss",
        "POSTGRES_PASSWORD": "tamoss-db-password",
        "TAMOSS_S3_ENDPOINT": "http://tamoss-kind-rustfs.tams.svc:9000",
        "TAMOSS_S3_PUBLIC_ENDPOINT": "https://s3.tamoss.localtest.me",
        "TAMOSS_S3_ACCESS_KEY": "rustfs-access",
        "TAMOSS_S3_SECRET_KEY": "rustfs-secret",
        "TAMOSS_S3_BUCKET": "tamoss",
        "TAMOSS_STORAGE_LABEL": "tamoss.local-kind:s3:tamoss",
        "TAMOSS_AUTH_REQUIRED": "true",
        "TAMOSS_BASIC_AUTH_USERNAME": "tamoss",
        "TAMOSS_BASIC_AUTH_PASSWORD": "tamoss-pass",
        "TAMOSS_API_TOKEN": "api-token",
        "TAMOSS_WORKER_ID": "tamoss-worker-0",
        "TAMOSS_WORKER_ENABLE_DELETE": "true",
        "TAMOSS_WORKER_ENABLE_WEBHOOK": "true",
        "TAMOSS_WEBHOOK_ALLOWED_HOSTS": ".tamoss.localtest.me,.svc.cluster.local",
    }
    for name, value in local_kind_env.items():
        monkeypatch.setenv(name, value)

    settings = Settings()
    storage_backend = settings.storage_backend_record()

    assert settings.database_url_value() == (
        "postgresql://tamoss:tamoss-db-password@"
        "tamoss-kind-rw.tams.svc.cluster.local:5432/tamoss"
    )
    assert settings.auth_required is True
    assert settings.basic_auth_password_value() == "tamoss-pass"
    assert settings.api_token_value() == "api-token"
    assert settings.worker_id == "tamoss-worker-0"
    assert settings.webhook_allowed_hosts == [
        ".tamoss.localtest.me",
        ".svc.cluster.local",
    ]
    assert storage_backend is not None
    assert storage_backend.endpoint_url == "http://tamoss-kind-rustfs.tams.svc:9000"
    assert storage_backend.public_endpoint_url == "https://s3.tamoss.localtest.me"
    assert storage_backend.access_key == "rustfs-access"
    assert storage_backend.secret_key == "rustfs-secret"


def test_disabled_oauth_accepts_empty_operator_url_env(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("TAMOSS_OAUTH2_ENABLED", "false")
    monkeypatch.setenv("TAMOSS_OAUTH2_ISSUER", "")
    monkeypatch.setenv("TAMOSS_OAUTH2_JWKS_URI", "")
    monkeypatch.setenv("TAMOSS_OAUTH2_AUDIENCE", "")
    monkeypatch.setenv("TAMOSS_OAUTH2_ALGORITHMS", "")

    settings = Settings()

    assert settings.oauth2_enabled is False
    assert settings.oauth2_issuer is None
    assert settings.oauth2_jwks_uri is None
    assert settings.oauth2_allow_unscoped_full_access is True


def test_storage_backend_registration_is_disabled_by_default() -> None:
    assert Settings().storage_backend_registration_enabled is False


@pytest.mark.parametrize(
    ("kwargs", "message"),
    [
        (
            {"min_presigned_url_timeout": "301:0"},
            "min_presigned_url_timeout must be less than or equal to",
        ),
        (
            {
                "min_object_timeout": "300:500000000",
            },
            "min_object_timeout must be a whole-second",
        ),
    ],
)
def test_settings_reject_timeout_contracts(
    kwargs: dict[str, object], message: str
) -> None:
    with pytest.raises(ValueError, match=message):
        Settings(**kwargs)


def test_settings_caps_presigned_put_ttl_at_object_timeout() -> None:
    settings = Settings(
        min_object_timeout="300:0",
        min_presigned_url_timeout="30:0",
        s3_presign_ttl_seconds=3600,
    )

    assert settings.min_object_timeout_seconds() == 300
    assert settings.presigned_put_ttl_seconds() == 300


def test_default_versions_are_not_placeholders() -> None:
    settings = Settings()

    assert settings.tamoss_version != "0.0.0"
    assert settings.service_version != "0.0.0"
    assert settings.api_version == "8.1"


def test_runtime_secret_files_are_reloaded(tmp_path) -> None:
    token_file = tmp_path / "api-token"
    token_file.write_text("first-token", encoding="utf-8")

    settings = Settings(
        api_token_file=str(token_file),
    )

    assert settings.api_token_value() == "first-token"

    token_file.write_text("second-token", encoding="utf-8")

    assert settings.api_token_value() == "second-token"


def test_read_secret_file_is_the_shared_secret_loader(tmp_path) -> None:
    secret_file = tmp_path / "secret"
    secret_file.write_text(" shared-secret \n", encoding="utf-8")

    assert read_secret_file(str(secret_file)) == "shared-secret"
    assert read_secret_file(str(tmp_path / "missing")) is None


def test_runtime_secret_file_accessors_fail_closed(tmp_path) -> None:
    settings = Settings(api_token_file=str(tmp_path / "missing-token"))

    with pytest.raises(ValueError, match="TAMOSS_API_TOKEN_FILE"):
        settings.api_token_value()


def test_database_url_parts_are_percent_encoded(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("POSTGRES_HOST", "postgres")
    monkeypatch.setenv("POSTGRES_PORT", "5432")
    monkeypatch.setenv("POSTGRES_USER", "tamoss user")
    monkeypatch.setenv("POSTGRES_PASSWORD", "p@ss/word")
    monkeypatch.setenv("POSTGRES_DB", "tams/main")

    settings = Settings()

    expected = "postgresql://tamoss%20user:p%40ss%2Fword@postgres:5432/tams%2Fmain"
    assert settings.database_url_value() == expected


def test_oauth2_algorithms_reject_symmetric_or_none_algorithms() -> None:
    with pytest.raises(ValueError, match="unsupported OAuth2 JWT algorithm"):
        Settings(oauth2_algorithms=["RS256", "HS256"])


def test_basic_auth_password_is_explicit() -> None:
    settings = Settings(api_token="shared-token", basic_auth_password=None)

    assert settings.api_token_value() == "shared-token"
    assert settings.basic_auth_password_value() is None


def test_forward_auth_secret_file_is_reloaded(tmp_path) -> None:
    proof_file = tmp_path / "forward-auth-proof"
    proof_file.write_text("proof-1", encoding="utf-8")

    settings = Settings(
        trust_forward_auth_headers=True,
        forward_auth_shared_secret_file=str(proof_file),
    )

    assert settings.forward_auth_shared_secret_value() == "proof-1"

    proof_file.write_text("proof-2", encoding="utf-8")

    assert settings.forward_auth_shared_secret_value() == "proof-2"
