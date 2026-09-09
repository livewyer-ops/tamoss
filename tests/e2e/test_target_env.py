from __future__ import annotations

import shlex
import sys
from pathlib import Path

import pytest

from tests.e2e.target import E2ETarget, _load_env_file


def test_target_env_sources_common_defaults(tmp_path: Path) -> None:
    common = tmp_path / "common.env"
    common.write_text(
        "\n".join(
            [
                "TEST_TAMOSS_API=https://api.example.test",
                "TEST_TAMOSS_UI_EXPECT_STATUS=302",
            ]
        ),
        encoding="utf-8",
    )
    target = tmp_path / "target.env"
    target.write_text(
        "\n".join(
            [
                ". common.env",
                "TEST_TARGET=custom",
                "TEST_TAMOSS_UI_EXPECT_STATUS=200",
            ]
        ),
        encoding="utf-8",
    )

    values = _load_env_file(target)

    assert values["TEST_TAMOSS_API"] == "https://api.example.test"
    assert values["TEST_TARGET"] == "custom"
    assert values["TEST_TAMOSS_UI_EXPECT_STATUS"] == "200"


def test_target_env_rejects_source_cycles(tmp_path: Path) -> None:
    first = tmp_path / "first.env"
    second = tmp_path / "second.env"
    first.write_text(". second.env\n", encoding="utf-8")
    second.write_text(". first.env\n", encoding="utf-8")

    with pytest.raises(pytest.UsageError, match="source cycle"):
        _load_env_file(first)


def test_target_env_loads_token_command_and_service_readiness(
    tmp_path: Path,
) -> None:
    token_command = shlex.quote(
        f"{sys.executable} -c 'print(\"Bearer command-token\")'"
    )
    target = tmp_path / "target.env"
    target.write_text(
        "\n".join(
            [
                "TEST_TAMOSS_API=https://api.example.test",
                "TEST_TAMOSS_UI=https://ui.example.test",
                "TEST_TAMOSS_AUTH_PASSWORD=unused",
                f"TEST_TAMOSS_TOKEN_COMMAND={token_command}",
                "TEST_TAMOSS_READINESS_MODE=service",
                "TEST_TAMOSS_UPLOAD_CHECKSUM_HEADER=false",
                "TEST_TAMOSS_MEMORY_BUDGET_MIB=3500",
                "TEST_TAMOSS_BROWSER_API_AVAILABLE=false",
            ]
        ),
        encoding="utf-8",
    )

    loaded = E2ETarget.from_file(target)

    assert loaded.auth_headers == {"Authorization": "Bearer command-token"}
    assert loaded.readiness_mode == "service"
    assert loaded.upload_checksum_header is False
    assert loaded.memory_budget_mib == 3500
    assert loaded.browser_api_available is False
    target_repr = repr(loaded)
    assert "command-token" not in target_repr
    assert "unused" not in target_repr


def test_target_env_media_origin_matches_whatever_path_storage_serves(
    tmp_path: Path,
) -> None:
    target = _minimal_target(
        tmp_path, "TEST_TAMOSS_S3=https://s3.example.test/", "TEST_TAMOSS_TOKEN=token"
    )

    loaded = E2ETarget.from_file(target)

    assert loaded.s3_url == "https://s3.example.test"
    assert loaded.is_media_origin("https://s3.example.test/bucket/object?signed=1")
    assert loaded.is_media_origin("https://S3.EXAMPLE.TEST/object")
    assert not loaded.is_media_origin("https://cdn.example.test/object")
    assert not loaded.is_media_origin("https://s3.example.test.evil/object")


def test_target_env_without_media_origin_matches_nothing(tmp_path: Path) -> None:
    target = _minimal_target(tmp_path, "TEST_TAMOSS_TOKEN=token")

    loaded = E2ETarget.from_file(target)

    assert loaded.s3_url is None
    assert loaded.browser_api_available is True
    assert not loaded.is_media_origin("https://s3.example.test/object")


def test_target_env_rejects_media_origin_without_a_scheme(tmp_path: Path) -> None:
    target = _minimal_target(
        tmp_path, "TEST_TAMOSS_S3=s3.example.test", "TEST_TAMOSS_TOKEN=token"
    )

    with pytest.raises(pytest.UsageError, match="TEST_TAMOSS_S3"):
        E2ETarget.from_file(target)


def _minimal_target(tmp_path: Path, *extra_lines: str) -> Path:
    target = tmp_path / "target.env"
    target.write_text(
        "\n".join(
            [
                "TEST_TAMOSS_API=https://api.example.test",
                "TEST_TAMOSS_UI=https://ui.example.test",
                "TEST_TAMOSS_AUTH_PASSWORD=unused",
                *extra_lines,
            ]
        ),
        encoding="utf-8",
    )
    return target


def test_target_env_rejects_non_positive_memory_budget(tmp_path: Path) -> None:
    target = tmp_path / "target.env"
    target.write_text(
        "\n".join(
            [
                "TEST_TAMOSS_API=https://api.example.test",
                "TEST_TAMOSS_UI=https://ui.example.test",
                "TEST_TAMOSS_AUTH_PASSWORD=unused",
                "TEST_TAMOSS_TOKEN=token",
                "TEST_TAMOSS_MEMORY_BUDGET_MIB=0",
            ]
        ),
        encoding="utf-8",
    )

    with pytest.raises(pytest.UsageError, match="MEMORY_BUDGET_MIB"):
        E2ETarget.from_file(target)


def test_target_env_rejects_unknown_readiness_mode(tmp_path: Path) -> None:
    target = tmp_path / "target.env"
    target.write_text(
        "\n".join(
            [
                "TEST_TAMOSS_API=https://api.example.test",
                "TEST_TAMOSS_UI=https://ui.example.test",
                "TEST_TAMOSS_AUTH_PASSWORD=unused",
                "TEST_TAMOSS_TOKEN=token",
                "TEST_TAMOSS_READINESS_MODE=unknown",
            ]
        ),
        encoding="utf-8",
    )

    with pytest.raises(pytest.UsageError, match="READINESS_MODE"):
        E2ETarget.from_file(target)
