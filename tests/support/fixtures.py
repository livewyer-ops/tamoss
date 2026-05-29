from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import yaml

from tests.support.paths import REPO_ROOT

FIXTURE_ROOT = REPO_ROOT / "tests" / "fixtures"


def load_json_fixture(path: str | Path) -> Any:
    return json.loads((FIXTURE_ROOT / path).read_text(encoding="utf-8"))


def load_yaml_fixture(path: str | Path) -> Any:
    return yaml.safe_load((FIXTURE_ROOT / path).read_text(encoding="utf-8"))
