from __future__ import annotations

import ast
from pathlib import Path

import yaml

from tests.support.paths import REPO_ROOT

INVENTORY_PATH = REPO_ROOT / "tests/fixtures/tams/obligations.yaml"
REQUIRED_FIELDS = {"id", "source", "requirement", "tests"}


def test_tams_obligation_inventory_references_existing_tests() -> None:
    inventory = yaml.safe_load(INVENTORY_PATH.read_text(encoding="utf-8"))
    obligations = inventory["obligations"]
    collected_tests = _collected_test_ids(REPO_ROOT / "tests")
    seen_ids: set[str] = set()

    assert obligations
    for obligation in obligations:
        assert set(obligation) >= REQUIRED_FIELDS
        assert obligation["id"] not in seen_ids
        seen_ids.add(obligation["id"])
        assert obligation["tests"]
        missing = [
            test_id for test_id in obligation["tests"] if test_id not in collected_tests
        ]
        assert missing == []


def _collected_test_ids(tests_root: Path) -> set[str]:
    test_ids: set[str] = set()
    for path in tests_root.rglob("test_*.py"):
        module_path = path.relative_to(REPO_ROOT).as_posix()
        tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
        for node in tree.body:
            if isinstance(node, ast.FunctionDef) and node.name.startswith("test_"):
                test_ids.add(f"{module_path}::{node.name}")
            elif isinstance(node, ast.ClassDef):
                for child in node.body:
                    if isinstance(child, ast.FunctionDef) and child.name.startswith(
                        "test_"
                    ):
                        test_ids.add(f"{module_path}::{node.name}::{child.name}")
    return test_ids
