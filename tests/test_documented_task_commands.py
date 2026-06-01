from __future__ import annotations

import re
import shutil
import subprocess
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
DOCUMENTED_PATHS = [
    ROOT / "README.md",
    ROOT / "deploy" / "demo" / "README.md",
    *sorted((ROOT / "docs").rglob("*.md")),
]
TASK_REFERENCE = re.compile(
    r"(?:`task\s+([A-Za-z0-9:_-]+|-l)\b|^\s*task\s+([A-Za-z0-9:_-]+|-l)\b)",
    re.MULTILINE,
)
TASK_LISTING = re.compile(r"^\*\s+([^:\s]+(?::[^:\s]+)*):", re.MULTILINE)
ANSI_ESCAPE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")


def test_documented_task_commands_are_supported_entry_points() -> None:
    if shutil.which("task") is None:
        pytest.skip("task binary is unavailable")

    result = subprocess.run(
        ["task", "--list-all"],
        cwd=ROOT,
        capture_output=True,
        check=True,
        text=True,
    )
    available = set(TASK_LISTING.findall(ANSI_ESCAPE.sub("", result.stdout)))
    missing = sorted(
        {
            command
            for path in DOCUMENTED_PATHS
            for command in documented_task_commands(path)
            if command not in available
        }
    )

    assert missing == []


def documented_task_commands(path: Path) -> set[str]:
    commands = set()
    for match in TASK_REFERENCE.finditer(path.read_text(encoding="utf-8")):
        command = match.group(1) or match.group(2)
        if command == "-l":
            continue
        commands.add(command)
    return commands
