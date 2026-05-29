from __future__ import annotations

import subprocess
from pathlib import Path

from tests.support.paths import REPO_ROOT


def run_command(
    command: list[str],
    *,
    cwd: Path = REPO_ROOT,
    capture: bool = False,
    check: bool = True,
    timeout: int | None = None,
) -> str:
    completed = subprocess.run(
        command,
        cwd=cwd,
        text=True,
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.STDOUT if capture else None,
        timeout=timeout,
        check=False,
    )
    if check and completed.returncode != 0:
        rendered = " ".join(command)
        output = f"\n{completed.stdout}" if capture and completed.stdout else ""
        raise AssertionError(f"{rendered} exited {completed.returncode}{output}")
    return completed.stdout or ""
