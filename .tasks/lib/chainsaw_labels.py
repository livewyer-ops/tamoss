#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
import re
import sys
from pathlib import Path

AXES = {
    "target": {"kind", "deployed", "external"},
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
        "test.tamoss.io/target=kind,test.tamoss.io/tier in (smoke,standard,extended)"
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

        failures.extend(_validate_native_operations(path))
        failures.extend(_validate_file_references(path))

    failures.extend(_validate_no_shell_resources(root))

    if failures:
        print("\n".join(failures), file=sys.stderr)
        return 1
    print(f"chainsaw suite policy ok ({len(files)} tests)")
    return 0


def _validate_native_operations(path: Path) -> list[str]:
    lines = path.read_text(encoding="utf-8").splitlines()
    failures: list[str] = []
    for line_number, line in enumerate(lines, start=1):
        if re.match(r"^\s*-\s+script:\s*$", line):
            failures.append(
                f"{path}:{line_number}: use a native operation, proxy, or checked command instead of script"
            )

    for line_number, entrypoint, args, checked in _command_blocks(lines):
        executable = Path(entrypoint).name
        if executable in {"bash", "sh"}:
            failures.append(
                f"{path}:{line_number}: shell command entrypoints are not allowed"
            )
        if not checked:
            failures.append(
                f"{path}:{line_number}: external-boundary commands require an explicit check"
            )
        if executable == "kubectl" and not _allowed_kubectl_command(args):
            failures.append(
                f"{path}:{line_number}: kubectl is limited to exec, logs, and operator environment configuration"
            )
    return failures


def _validate_no_shell_resources(root: Path) -> list[str]:
    shell_item = re.compile(r"^\s*-\s+['\"]?(?:/bin/)?(?:ba)?sh['\"]?\s*$")
    failures: list[str] = []
    for path in sorted(root.rglob("*.yaml")):
        for line_number, line in enumerate(
            path.read_text(encoding="utf-8").splitlines(), start=1
        ):
            if shell_item.match(line):
                failures.append(
                    f"{path}:{line_number}: shell entrypoints are not allowed in Chainsaw resources"
                )
    return failures


def _validate_file_references(path: Path) -> list[str]:
    file_reference = re.compile(r"^\s+file:\s+(.+?)\s*$")
    failures: list[str] = []
    for line_number, line in enumerate(
        path.read_text(encoding="utf-8").splitlines(), start=1
    ):
        match = file_reference.match(line)
        if not match:
            continue
        reference = match.group(1).strip("'\"")
        if any(marker in reference for marker in ("$", "(", "*", "?", "[")):
            continue
        if not (path.parent / reference).is_file():
            failures.append(
                f"{path}:{line_number}: referenced file does not exist: {reference}"
            )
    return failures


def _command_blocks(lines: list[str]) -> list[tuple[int, str, list[str], bool]]:
    blocks: list[tuple[int, str, list[str], bool]] = []
    command_start = re.compile(r"^(\s*)-\s+command:\s*$")
    for index, line in enumerate(lines):
        match = command_start.match(line)
        if not match:
            continue
        indent = len(match.group(1))
        block: list[str] = []
        for candidate in lines[index + 1 :]:
            if re.match(rf"^\s{{{indent}}}-\s+\w", candidate):
                break
            block.append(candidate)

        entrypoint = ""
        args: list[str] = []
        in_args = False
        checked = False
        for candidate in block:
            stripped = candidate.strip()
            if stripped.startswith("entrypoint:"):
                entrypoint = stripped.split(":", 1)[1].strip().strip("'\"")
                in_args = False
            elif stripped == "args:":
                in_args = True
            elif stripped == "check:":
                checked = True
                in_args = False
            elif in_args and stripped.startswith("- "):
                args.append(stripped[2:].strip().strip("'\""))
            elif (
                stripped
                and not stripped.startswith("#")
                and not stripped.startswith("-")
            ):
                in_args = False
        blocks.append((index + 1, entrypoint, args, checked))
    return blocks


def _allowed_kubectl_command(args: list[str]) -> bool:
    if not args:
        return False
    if args[:2] == ["set", "env"]:
        return True
    if "logs" in args:
        return True
    if "exec" not in args or "--" not in args:
        return False
    remote = args[args.index("--") + 1 :]
    if not remote:
        return False
    executable = Path(remote[0]).name
    if executable in {"bash", "sh"}:
        return False
    return not (executable in {"python", "python3"} and remote[1:2] == ["-c"])


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
