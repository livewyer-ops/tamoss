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
            ]
        ),
        encoding="utf-8",
    )

    loaded = E2ETarget.from_file(target)

    assert loaded.auth_headers == {"Authorization": "Bearer command-token"}
    assert loaded.readiness_mode == "service"
    assert loaded.upload_checksum_header is False


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
