#!/usr/bin/env python3
"""Emit release metadata from the operator compatibility manifest."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Any

RELEASE_CANDIDATE_PATTERN = re.compile(r"^(?P<base>.+)-rc(?P<number>0|[1-9][0-9]*)$")


def fail(message: str) -> None:
    print(message, file=sys.stderr)
    raise SystemExit(1)


def as_dict(value: Any, context: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        fail(f"{context} must be an object")
    return value


def load_releases(path: Path) -> list[dict[str, Any]]:
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        fail(f"{path} must remain JSON-compatible YAML: {exc}")

    releases = as_dict(data, str(path)).get("releases")
    if not isinstance(releases, list):
        fail(f"{path} must contain a releases list")
    if not releases:
        fail(f"{path} must contain at least one release")
    return [as_dict(item, "release") for item in releases]


def validate_releases(
    releases: list[dict[str, Any]],
    path: Path,
    *,
    verbose: bool = True,
) -> None:
    versions: set[str] = set()
    for release in releases:
        version = required_string(release, "version", path)
        required_string(release, "schemaRevision", path, version)
        required_string(release, "tamsAPI", path, version)
        if version in versions:
            fail(f"{path} contains duplicate release {version}")
        versions.add(version)

    for release in releases:
        version = str(release["version"])
        upgrade = as_dict(release.get("upgrade"), f"release {version} upgrade")
        required_string(upgrade, "class", path, version)
        upgrade_from = upgrade.get("from")
        if not isinstance(upgrade_from, list):
            fail(f"release {version} upgrade.from must be a list")
        for source in upgrade_from:
            if not isinstance(source, str) or not source:
                fail(
                    f"release {version} upgrade.from entries must be non-empty strings"
                )
            if source not in versions:
                fail(
                    f"release {version} upgrade.from references unknown "
                    f"release {source}"
                )

    if verbose:
        print(f"validated {len(releases)} releases in {path}")


def required_string(
    mapping: dict[str, Any],
    key: str,
    path: Path,
    version: str | None = None,
) -> str:
    value = mapping.get(key)
    context = f"release {version}" if version else str(path)
    if not isinstance(value, str) or not value:
        fail(f"{context} must contain non-empty {key}")
    return value


def release_metadata(
    version: str, releases: list[dict[str, Any]], path: Path
) -> dict[str, str]:
    validate_releases(releases, path, verbose=False)
    releases_by_version = {str(item.get("version")): item for item in releases}

    release = releases_by_version.get(version)
    release_candidate = RELEASE_CANDIDATE_PATTERN.fullmatch(version)
    if release is None and release_candidate is not None:
        release = releases_by_version.get(release_candidate.group("base"))
    if release is None:
        fail(f"release {version} not found in {path}")

    upgrade = as_dict(release.get("upgrade", {}), f"release {version} upgrade")
    upgrade_from = upgrade.get("from", [])
    if not isinstance(upgrade_from, list):
        fail(f"release {version} upgrade.from must be a list")

    previous_schema_revisions = {
        required_string(
            releases_by_version[str(item)], "schemaRevision", path, str(item)
        )
        for item in upgrade_from
    }
    if len(previous_schema_revisions) > 1:
        fail(
            f"release {version} upgrade.from references multiple schema revisions: "
            f"{', '.join(sorted(previous_schema_revisions))}"
        )

    metadata = {
        "version": version,
        "prerelease": "true" if release_candidate is not None else "false",
        "schema_revision": release.get("schemaRevision"),
        "previous_schema_revision": next(iter(previous_schema_revisions), ""),
        "tams_api": release.get("tamsAPI"),
        "upgrade_class": upgrade.get("class", ""),
        "upgrade_from": ",".join(str(item) for item in upgrade_from),
    }
    for key, value in metadata.items():
        if value is None:
            fail(f"release {version} is missing {key}")
    return {key: str(value) for key, value in metadata.items()}


def emit_release_metadata(
    version: str, releases: list[dict[str, Any]], path: Path
) -> None:
    for key, value in release_metadata(version, releases, path).items():
        print(f"{key}={value}")


def main() -> None:
    if len(sys.argv) < 2:
        fail(
            "usage: release-metadata.py <version> [compatibility-file] | "
            "release-metadata.py --validate [compatibility-file]"
        )

    if sys.argv[1] == "--validate":
        if len(sys.argv) > 3:
            fail("usage: release-metadata.py --validate [compatibility-file]")
        path = (
            Path(sys.argv[2])
            if len(sys.argv) == 3
            else Path("operator/compatibility.yaml")
        )
        validate_releases(load_releases(path), path)
        return

    if len(sys.argv) not in {2, 3}:
        fail("usage: release-metadata.py <version> [compatibility-file]")

    version = sys.argv[1]
    path = (
        Path(sys.argv[2]) if len(sys.argv) == 3 else Path("operator/compatibility.yaml")
    )
    emit_release_metadata(version, load_releases(path), path)


if __name__ == "__main__":
    main()
