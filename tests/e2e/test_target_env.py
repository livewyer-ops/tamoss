from __future__ import annotations

from pathlib import Path

import pytest

from tests.e2e.target import _load_env_file


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
