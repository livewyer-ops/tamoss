#!/usr/bin/env python3
"""Record the complete release, using build outputs rather than mutable tags."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
from pathlib import Path

IMAGES = {
    "api": "livewyer/tamoss-api",
    "ui": "livewyer/tamoss-ui",
    "console_api": "livewyer/tamoss-console-api",
    "operator": "livewyer/tamoss-operator",
}


def image_references(environment: dict[str, str]) -> dict[str, str]:
    images = {}
    for component, repository in IMAGES.items():
        digest = environment.get(f"{component.upper()}_DIGEST", "")
        if re.fullmatch(r"sha256:[0-9a-f]{64}", digest) is None:
            raise ValueError(f"Missing or invalid {component} image digest")
        images[component] = f"{repository}@{digest}"
    return images


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    tag = os.environ["GITHUB_REF_NAME"]
    metadata = subprocess.check_output(
        [sys.executable, ".github/scripts/release-metadata.py", tag.removeprefix("v")],
        text=True,
    )
    images = image_references(dict(os.environ))
    record = {
        "recordVersion": 1,
        "tag": tag,
        "sourceCommit": subprocess.check_output(
            ["git", "rev-parse", "HEAD"], text=True
        ).strip(),
        "bbcTamsCommit": subprocess.check_output(
            ["git", "rev-parse", "HEAD:src/vendor/bbc-tams"], text=True
        ).strip(),
        "compatibility": dict(line.split("=", 1) for line in metadata.splitlines()),
        "images": images,
        "workerImage": images["api"],
        "artifacts": {
            path.name: {"sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
            for path in (
                Path("dist/operator-release/install.yaml"),
                Path("operator/compatibility.yaml"),
            )
        },
        "validationRun": (
            f"{os.environ['GITHUB_SERVER_URL']}/{os.environ['GITHUB_REPOSITORY']}"
            f"/actions/runs/{os.environ['GITHUB_RUN_ID']}"
        ),
        "validationRunAttempt": os.environ["GITHUB_RUN_ATTEMPT"],
    }
    args.output.write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
