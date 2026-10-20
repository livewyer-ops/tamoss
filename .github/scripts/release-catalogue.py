#!/usr/bin/env python3
"""Build the operator's instance release catalogue from reviewed release data."""

from __future__ import annotations

import argparse
import json
import os
import runpy
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def runtime_release(version: str, images: dict[str, str]) -> dict:
    metadata = runpy.run_path(str(Path(__file__).with_name("release-metadata.py")))
    path = ROOT / "operator/compatibility.yaml"
    entries = metadata["load_releases"](path)
    resolved = metadata["release_metadata"](version, entries, path)
    base = version.rsplit("-rc", 1)[0]
    source = next(entry for entry in entries if entry["version"] == base)
    return {
        "version": version,
        "sourceCommit": os.environ.get("SOURCE_COMMIT", ""),
        "upgradeFrom": list(
            dict.fromkeys(source["upgrade"]["from"] + source.get("adoptionFrom", []))
        ),
        "schema": {
            "version": resolved["schema_revision"],
            "previousVersion": resolved["previous_schema_revision"],
            "databaseRevision": source["databaseRevision"],
            "tamsAPI": resolved["tams_api"],
        },
        "images": {
            **source["runtimeImages"],
            "api": images["api"],
            "ui": images["ui"],
            "console": images["console_api"],
        },
    }


def catalogue(current: dict) -> list[dict]:
    # Retain published schemas even when a later RC changes them.
    entries = []
    for version in current["upgradeFrom"]:
        entry = json.loads((ROOT / "operator/releases" / f"{version}.json").read_text())
        if entry["version"] != version:
            raise ValueError(f"Historical release does not match {version}")
        entries.append(entry)
    if any(entry["version"] == current["version"] for entry in entries):
        raise ValueError("Current release duplicates a historical entry")
    return [*entries, current]


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--development", action="store_true")
    parser.add_argument("--operand-tag", default="dev")
    parser.add_argument("--schema-version", default="dev")
    parser.add_argument("--previous-schema-version")
    args = parser.parse_args()
    if args.development:
        releases = json.loads((ROOT / "operator/compatibility.yaml").read_text())[
            "releases"
        ]
        version = releases[-1]["version"]
        images = {
            key: f"livewyer/tamoss-{repo}:{args.operand_tag}"
            for key, repo in (
                ("api", "api"),
                ("ui", "ui"),
                ("console_api", "console-api"),
            )
        }
    else:
        version = os.environ["GITHUB_REF_NAME"].removeprefix("v")
        module = runpy.run_path(str(Path(__file__).with_name("release-record.py")))
        images = module["image_references"](dict(os.environ))
    current = runtime_release(version, images)
    if args.development:
        current["version"] = "dev"
        current["schema"]["version"] = args.schema_version
        if args.previous_schema_version is not None:
            current["schema"]["previousVersion"] = args.previous_schema_version
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(catalogue(current), indent=2) + "\n")


if __name__ == "__main__":
    main()
