from __future__ import annotations

import ast
import re
import subprocess
from fnmatch import fnmatch
from itertools import pairwise
from pathlib import Path
from typing import Any

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[3]
UPSTREAM_REPO = REPO_ROOT / "src/vendor/bbc-tams"
TRACEABILITY_ROOT = Path(__file__).resolve().parent
REQUIREMENTS_PATH = TRACEABILITY_ROOT / "requirements.yaml"
RELEASES_ROOT = TRACEABILITY_ROOT / "releases"
CONFORMANCE_ROOT = REPO_ROOT / "tests/tams/conformance"
HTTP_METHODS = {"delete", "get", "head", "options", "patch", "post", "put", "trace"}
REQUIREMENT_ID = re.compile(r"^(?:TAMS|TAMOSS)(?:-[A-Z]+)+-[0-9]{3}$")

pytestmark = pytest.mark.architecture


def _load_yaml(path: Path) -> dict[str, Any]:
    document = yaml.safe_load(path.read_text())
    assert isinstance(document, dict), f"{path}: expected a mapping"
    return document


def _requirements() -> dict[str, dict[str, Any]]:
    document = _load_yaml(REQUIREMENTS_PATH)
    assert document["schemaVersion"] == 1
    requirements = document["requirements"]
    by_id = {item["id"]: item for item in requirements}
    assert len(by_id) == len(requirements), "duplicate requirement ID"
    return by_id


def _releases() -> list[tuple[Path, dict[str, Any]]]:
    paths = sorted(
        RELEASES_ROOT.glob("*.yaml"), key=lambda path: _version_key(path.stem)
    )
    assert paths, "no release traceability manifests found"
    return [(path, _load_yaml(path)) for path in paths]


def _version_key(version: str) -> tuple[int, ...]:
    parts = version.split(".")
    assert all(part.isdigit() for part in parts), f"unsupported release name: {version}"
    return tuple(int(part) for part in parts)


def _git(*args: str) -> str:
    return subprocess.check_output(
        ["git", "-C", str(UPSTREAM_REPO), *args], text=True
    ).strip()


def _spec(tag: str) -> dict[str, Any]:
    raw = _git("show", f"{tag}:api/TimeAddressableMediaStore.yaml")
    document = yaml.safe_load(raw)
    assert isinstance(document, dict)
    return document


def _reference_path(reference: str) -> Path:
    return REPO_ROOT / reference.split("::", maxsplit=1)[0]


def _module_pytestmark_names(path: Path) -> set[str]:
    tree = ast.parse(path.read_text())
    markers: set[str] = set()
    for node in tree.body:
        if not isinstance(node, (ast.Assign, ast.AnnAssign)):
            continue
        targets = node.targets if isinstance(node, ast.Assign) else [node.target]
        if not any(
            isinstance(target, ast.Name) and target.id == "pytestmark"
            for target in targets
        ):
            continue
        value = node.value
        if value is None:
            continue
        for child in ast.walk(value):
            if (
                isinstance(child, ast.Attribute)
                and isinstance(child.value, ast.Attribute)
                and child.value.attr == "mark"
                and isinstance(child.value.value, ast.Name)
                and child.value.value.id == "pytest"
            ):
                markers.add(child.attr)
    return markers


def _assert_reference_exists(reference: str, *, requirement_id: str) -> None:
    path = _reference_path(reference)
    assert path.exists(), f"{requirement_id}: missing {reference}"
    if "::" not in reference:
        return

    node_id = reference.split("::", maxsplit=1)[1]
    if path.suffix == ".py":
        tree = ast.parse(path.read_text())
        executable_nodes: set[str] = set()
        for node in tree.body:
            if isinstance(node, (ast.AsyncFunctionDef, ast.FunctionDef)):
                executable_nodes.add(node.name)
            elif isinstance(node, ast.ClassDef):
                executable_nodes.update(
                    f"{node.name}::{child.name}"
                    for child in node.body
                    if isinstance(child, (ast.AsyncFunctionDef, ast.FunctionDef))
                )
        assert node_id in executable_nodes, f"{requirement_id}: missing {reference}"
        return

    if path.suffix == ".go":
        test_functions = set(
            re.findall(
                r"^func\s+(Test[A-Za-z0-9_]+)\s*\(",
                path.read_text(),
                re.MULTILINE,
            )
        )
        assert node_id in test_functions, f"{requirement_id}: missing {reference}"
        return

    assert node_id in path.read_text(), f"{requirement_id}: missing {reference}"


def _assert_requirement_ids(
    requirement_ids: list[str],
    *,
    requirements: dict[str, dict[str, Any]],
    source: str,
    implemented: bool = False,
) -> None:
    assert requirement_ids, f"{source}: no mapped requirements"
    assert len(requirement_ids) == len(set(requirement_ids)), (
        f"{source}: duplicate requirement mapping"
    )
    unknown = set(requirement_ids) - requirements.keys()
    assert not unknown, f"{source}: unknown requirements {sorted(unknown)}"
    if implemented:
        unresolved = [
            requirement_id
            for requirement_id in requirement_ids
            if requirements[requirement_id]["disposition"] != "implemented"
        ]
        assert not unresolved, f"{source}: requirements not implemented {unresolved}"


def _changed_operations(before: dict[str, Any], after: dict[str, Any]) -> set[str]:
    changed: set[str] = set()
    before_paths = before.get("paths", {})
    after_paths = after.get("paths", {})
    for path in set(before_paths) | set(after_paths):
        before_item = before_paths.get(path, {})
        after_item = after_paths.get(path, {})
        for method in HTTP_METHODS:
            if before_item.get(method) != after_item.get(method):
                changed.add(f"{method.upper()} {path}")
    return changed


def _changed_upstream_files(pattern: str, *, from_tag: str, to_tag: str) -> set[str]:
    output = _git("diff", "--name-only", f"{from_tag}..{to_tag}", "--", pattern)
    return set(output.splitlines()) if output else set()


def _changed_webhooks(before: dict[str, Any], after: dict[str, Any]) -> set[str]:
    before_webhooks = before.get("webhooks", {})
    after_webhooks = after.get("webhooks", {})
    return {
        name
        for name in set(before_webhooks) | set(after_webhooks)
        if before_webhooks.get(name) != after_webhooks.get(name)
    }


def _mapped_file_requirements(
    path: str, file_selectors: dict[str, list[str]]
) -> set[str]:
    return {
        requirement_id
        for requirement_id, patterns in file_selectors.items()
        if any(fnmatch(path, pattern) for pattern in patterns)
    }


def test_requirement_registry_is_complete_and_resolved() -> None:
    requirements = _requirements()
    release_versions = {path.stem for path, _ in _releases()}

    for requirement_id, item in requirements.items():
        assert REQUIREMENT_ID.fullmatch(requirement_id), requirement_id
        assert item["summary"], requirement_id
        assert item["introducedIn"] in release_versions, requirement_id
        assert item["disposition"] in {"implemented", "not-applicable"}, requirement_id
        assert "status" not in item, requirement_id
        assert "upstream" not in item, requirement_id
        if item["normative"]:
            assert item["disposition"] == "implemented", requirement_id
        if item["disposition"] == "implemented":
            assert item["implementation"], requirement_id
            assert item["tests"], requirement_id
        else:
            assert item.get("rationale"), requirement_id
            assert not item["implementation"], requirement_id
            assert not item["tests"], requirement_id

        for reference in item["implementation"]:
            _assert_reference_exists(reference, requirement_id=requirement_id)
        for reference in item["tests"]:
            path = _reference_path(reference)
            if path.suffix == ".py" and "tests/" in path.as_posix():
                assert "::" in reference, (
                    f"{requirement_id}: Python evidence must use an exact test node: "
                    f"{reference}"
                )
                assert reference.split("::")[-1].startswith("test_"), reference
            if path.suffix == ".go" and path.name.endswith("_test.go"):
                assert "::" in reference, (
                    f"{requirement_id}: Go test evidence must use an exact node ID: "
                    f"{reference}"
                )
                assert reference.split("::")[-1].startswith("Test"), reference
            _assert_reference_exists(reference, requirement_id=requirement_id)


def test_conformance_modules_belong_to_exactly_one_capability_gate() -> None:
    for path in sorted(CONFORMANCE_ROOT.glob("test_*.py")):
        markers = _module_pytestmark_names(path)
        assert "tams_conformance" in markers, f"{path}: missing tams_conformance"
        gates = markers & {"tams_contract", "tams_semantics"}
        assert len(gates) == 1, (
            f"{path}: expected exactly one capability gate, got {gates}"
        )


def test_release_deltas_match_pinned_upstream_history() -> None:
    requirements = _requirements()
    releases = _releases()

    for (_, previous), (_, current) in pairwise(releases):
        assert previous["release"]["toTag"] == current["release"]["fromTag"]
        assert previous["release"]["toCommit"] == current["release"]["fromCommit"]

    for path, document in releases:
        assert document["schemaVersion"] == 2
        release = document["release"]
        assert path.stem == release["toTag"]
        assert (
            _git("rev-parse", f"{release['fromTag']}^{{commit}}")
            == release["fromCommit"]
        )
        assert (
            _git("rev-parse", f"{release['toTag']}^{{commit}}") == release["toCommit"]
        )

        commits = _git(
            "rev-list", f"{release['fromTag']}..{release['toTag']}"
        ).splitlines()
        changed_files = _git(
            "diff", "--name-only", f"{release['fromTag']}..{release['toTag']}"
        ).splitlines()
        assert len(commits) == release["commitCount"]
        assert len(changed_files) == release["changedFileCount"]
        historical_files = set(changed_files)
        for commit in commits:
            historical_files.update(
                _git(
                    "diff-tree", "--no-commit-id", "--name-only", "-r", commit
                ).splitlines()
            )

        for section in ("operations", "schemas", "webhooks"):
            for source, requirement_ids in document[section].items():
                _assert_requirement_ids(
                    requirement_ids,
                    requirements=requirements,
                    source=f"{path.name}:{section}:{source}",
                )

        file_selectors = document["fileSelectors"]
        assert file_selectors
        mapped_file_ids: set[str] = set()
        for requirement_id, patterns in file_selectors.items():
            _assert_requirement_ids(
                [requirement_id],
                requirements=requirements,
                source=f"{path.name}:fileSelectors:{requirement_id}",
            )
            assert patterns, f"{path.name}:fileSelectors:{requirement_id}"
            assert len(patterns) == len(set(patterns)), requirement_id
            for pattern in patterns:
                assert any(
                    fnmatch(changed_file, pattern) for changed_file in historical_files
                ), f"{path.name}: unused selector {requirement_id}:{pattern}"
            mapped_file_ids.add(requirement_id)

        for record in document["records"]:
            _git("cat-file", "-e", f"{release['toCommit']}:{record}")
            assert record in changed_files, record
            assert _mapped_file_requirements(record, file_selectors), record

        mapped_ids = {
            requirement_id
            for section in ("operations", "schemas", "webhooks")
            for requirement_ids in document[section].values()
            for requirement_id in requirement_ids
        } | mapped_file_ids
        introduced_ids = {
            requirement_id
            for requirement_id, item in requirements.items()
            if item["introducedIn"] == release["toTag"]
        }
        assert introduced_ids <= mapped_ids
        future_ids = {
            requirement_id
            for requirement_id in mapped_ids
            if _version_key(requirements[requirement_id]["introducedIn"])
            > _version_key(release["toTag"])
        }
        assert not future_ids, f"{path.name}: future requirement mappings {future_ids}"

    latest_release = releases[-1][1]["release"]
    assert _git("rev-parse", "HEAD") == latest_release["toCommit"]


def test_release_deltas_map_every_changed_file_and_commit() -> None:
    requirements = _requirements()
    for path, document in _releases():
        release = document["release"]
        file_selectors = document["fileSelectors"]
        changed_files = _git(
            "diff", "--name-only", f"{release['fromTag']}..{release['toTag']}"
        ).splitlines()

        for changed_file in changed_files:
            requirement_ids = sorted(
                _mapped_file_requirements(changed_file, file_selectors)
            )
            _assert_requirement_ids(
                requirement_ids,
                requirements=requirements,
                source=f"{path.name}:file:{changed_file}",
            )

        commits = _git(
            "rev-list", f"{release['fromTag']}..{release['toTag']}"
        ).splitlines()
        for commit in commits:
            changed_in_commit = _git(
                "diff-tree", "--no-commit-id", "--name-only", "-r", commit
            ).splitlines()
            assert changed_in_commit, (
                f"{path.name}: commit {commit} has no changed files"
            )
            for changed_file in changed_in_commit:
                assert _mapped_file_requirements(changed_file, file_selectors), (
                    f"{path.name}: commit {commit} has unmapped file {changed_file}"
                )


def test_release_deltas_map_every_changed_openapi_operation() -> None:
    requirements = _requirements()
    for path, document in _releases():
        release = document["release"]
        changed = _changed_operations(
            _spec(release["fromTag"]), _spec(release["toTag"])
        )
        assert len(changed) == release["changedOperationCount"]
        assert changed == set(document["operations"])
        for operation, requirement_ids in document["operations"].items():
            _assert_requirement_ids(
                requirement_ids,
                requirements=requirements,
                source=f"{path.name}:operations:{operation}",
                implemented=True,
            )


def test_release_deltas_map_every_changed_openapi_schema() -> None:
    requirements = _requirements()
    for path, document in _releases():
        release = document["release"]
        changed = _changed_upstream_files(
            "api/schemas/*.json",
            from_tag=release["fromTag"],
            to_tag=release["toTag"],
        )
        assert len(changed) == release["changedSchemaCount"]
        assert changed == set(document["schemas"])
        for schema, requirement_ids in document["schemas"].items():
            _assert_requirement_ids(
                requirement_ids,
                requirements=requirements,
                source=f"{path.name}:schemas:{schema}",
                implemented=True,
            )


def test_release_deltas_map_every_changed_webhook() -> None:
    requirements = _requirements()
    for path, document in _releases():
        release = document["release"]
        changed = _changed_webhooks(_spec(release["fromTag"]), _spec(release["toTag"]))
        assert len(changed) == release["changedWebhookCount"]
        assert changed == set(document["webhooks"])
        for webhook, requirement_ids in document["webhooks"].items():
            _assert_requirement_ids(
                requirement_ids,
                requirements=requirements,
                source=f"{path.name}:webhooks:{webhook}",
                implemented=True,
            )
