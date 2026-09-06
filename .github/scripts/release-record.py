#!/usr/bin/env python3
"""Record the complete release, using build outputs rather than mutable tags."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import runpy
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


def commit_sha(name: str) -> str:
    value = os.environ.get(name, "")
    if re.fullmatch(r"[0-9a-f]{40}", value) is None:
        raise ValueError(f"Missing or invalid {name}")
    return value


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    tag = os.environ["GITHUB_REF_NAME"]
    metadata_module = runpy.run_path(
        str(Path(__file__).with_name("release-metadata.py"))
    )
    compatibility_path = Path("operator/compatibility.yaml")
    metadata = metadata_module["release_metadata"](
        tag.removeprefix("v"),
        metadata_module["load_releases"](compatibility_path),
        compatibility_path,
    )
    images = image_references(dict(os.environ))
    record = {
        "recordVersion": 1,
        "tag": tag,
        "sourceCommit": commit_sha("SOURCE_COMMIT"),
        "bbcTamsCommit": commit_sha("BBC_TAMS_COMMIT"),
        "compatibility": metadata,
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
