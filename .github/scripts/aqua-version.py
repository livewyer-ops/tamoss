#!/usr/bin/env python3
"""Print a package version pinned in aqua.yml."""

from __future__ import annotations

import re
import sys
from pathlib import Path


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: aqua-version.py <package-name>", file=sys.stderr)
        return 2

    package = sys.argv[1]
    text = Path("aqua.yml").read_text(encoding="utf-8")
    pattern = re.compile(
        rf"^\s*-\s+name:\s+{re.escape(package)}@v?([^\s#]+)",
        re.MULTILINE,
    )
    match = pattern.search(text)
    if not match:
        print(f"{package}: not found in aqua.yml", file=sys.stderr)
        return 1

    print(match.group(1))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
