from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW_DIR = ROOT / ".github" / "workflows"
UBUNTU_RUNNER = re.compile(r"^\s*runs-on:\s*(ubuntu-[^\s#]+)", re.MULTILINE)


def test_github_hosted_ubuntu_runners_are_version_pinned() -> None:
    runners = [
        runner
        for workflow in sorted(WORKFLOW_DIR.glob("*.yaml"))
        for runner in UBUNTU_RUNNER.findall(workflow.read_text(encoding="utf-8"))
    ]

    assert runners
    assert set(runners) == {"ubuntu-24.04"}
