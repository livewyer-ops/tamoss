#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path

AXES = {
    "target": {"render", "kind", "deployed", "external"},
    "tier": {"smoke", "standard", "extended", "release"},
    "domain": {
        "install",
        "instance",
        "storage",
        "db",
        "auth",
        "routing",
        "schema",
        "profile",
        "observability",
        "operations",
    },
    "lifecycle": {"read-only", "ephemeral", "destructive"},
    "provider": {"none", "cnpg", "rustfs", "authentik", "external", "mixed"},
}

SELECTORS = {
    "smoke": "test.tamoss.io/target=kind,test.tamoss.io/tier=smoke",
    "ci": (
        "test.tamoss.io/target=kind,"
        "test.tamoss.io/tier in (smoke,standard),"
        "test.tamoss.io/provider notin (external)"
    ),
    "nightly": (
        "test.tamoss.io/target=kind,"
        "test.tamoss.io/tier in (smoke,standard,extended)"
    ),
    "release": "test.tamoss.io/tier=release",
    "deployed": "test.tamoss.io/target=deployed,test.tamoss.io/lifecycle=read-only",
}


def main() -> int:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)
    selector = subparsers.add_parser("selector")
    selector.add_argument("profile", choices=sorted(SELECTORS))
    validate = subparsers.add_parser("validate")
    validate.add_argument("root", type=Path)
    args = parser.parse_args()

    if args.command == "selector":
        print(SELECTORS[args.profile])
        return 0
    return validate_labels(args.root)


def validate_labels(root: Path) -> int:
    allowed = {axis: set(values) for axis, values in AXES.items()}
    profiles = os.environ.get("CHAINSAW_ALLOWED_PROFILES", "").splitlines()
    allowed["profile"] = {"none", *filter(None, profiles)}
    required = [f"test.tamoss.io/{axis}" for axis in allowed]
    failures: list[str] = []
    files = sorted(root.glob("**/chainsaw-test.yaml"))

    for path in files:
        labels = _header_labels(path)
        for key in required:
            if key not in labels:
                failures.append(f"{path}: missing {key}")
                continue
            axis = key.rsplit("/", 1)[1]
            if labels[key] not in allowed[axis]:
                failures.append(f"{path}: unsupported {key}={labels[key]}")

        if (
            labels.get("test.tamoss.io/target") == "deployed"
            and labels.get("test.tamoss.io/lifecycle") != "read-only"
        ):
            failures.append(f"{path}: deployed tests must be read-only")

    if failures:
        print("\n".join(failures), file=sys.stderr)
        return 1
    print(f"chainsaw labels ok ({len(files)} tests)")
    return 0


def _header_labels(path: Path) -> dict[str, str]:
    labels: dict[str, str] = {}
    in_labels = False
    for line in path.read_text(encoding="utf-8").splitlines():
        if line == "spec:":
            break
        if line == "  labels:":
            in_labels = True
            continue
        if in_labels:
            if line.startswith("    ") and ":" in line:
                key, value = line.strip().split(":", 1)
                labels[key] = value.strip().strip("'\"")
            elif line.strip():
                in_labels = False
    return labels


if __name__ == "__main__":
    raise SystemExit(main())
