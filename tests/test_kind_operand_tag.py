from __future__ import annotations

import os
import re
import shutil
import subprocess
from pathlib import Path

import pytest
import yaml

ROOT = Path(__file__).resolve().parents[1]
KIND_LIB = ROOT / ".tasks/lib/kind.sh"
TAG_PATTERN = re.compile(r"^dev-[0-9a-f]{12}$")
# Binaries the dry run itself needs: docker and kind gate the image tasks,
# yq resolves the profile that names the Kind config; kubectl renders its overlay.
_PLAN_BINARIES = ("task", "docker", "kind", "yq", "kubectl")
_PLAN_BINARIES_MSG = ", ".join(_PLAN_BINARIES[:-1]) + f" and {_PLAN_BINARIES[-1]}"


def test_operand_tag_is_content_derived_and_directory_independent(
    tmp_path: Path,
) -> None:
    """The tag must not depend on where the helper is called from.

    Resolving src/ against the caller's directory matches nothing elsewhere and
    yields a constant tag, which is exactly the staleness the tag exists to
    prevent — and it fails silently.
    """
    if shutil.which("bash") is None or shutil.which("git") is None:
        pytest.skip("bash and git are required for operand tag checks")

    from_root = _operand_tag(ROOT)
    from_subdirectory = _operand_tag(ROOT / "src/app")

    assert TAG_PATTERN.match(from_root), from_root
    assert from_root == from_subdirectory
    assert from_root == _operand_tag(ROOT), "tag is not stable across calls"


def test_operand_tag_falls_back_outside_a_work_tree(tmp_path: Path) -> None:
    if shutil.which("bash") is None or shutil.which("git") is None:
        pytest.skip("bash and git are required for operand tag checks")

    assert _operand_tag(tmp_path) == "dev"


@pytest.mark.parametrize(
    "path",
    [
        "src/app/tamoss/app.py",
        "src/app/frontend/src/App.tsx",
        "operator/internal/consoleapi/server.go",
        "operator/cmd/console-api/main.go",
        "operator/api/v1alpha1/tamoss_types.go",
        "operator/go.mod",
        "operator/go.sum",
        "operator/Dockerfile.console-api",
        ".dockerignore",
    ],
)
def test_operand_tag_tracks_each_build_input(tmp_path: Path, path: str) -> None:
    _git(tmp_path, "init")
    source = tmp_path / "src/base.py"
    source.parent.mkdir()
    source.write_text("base\n", encoding="utf-8")
    _git(tmp_path, "add", "src/base.py")
    original = _operand_tag(tmp_path)
    changed = tmp_path / path
    changed.parent.mkdir(parents=True, exist_ok=True)
    changed.write_text("first\n", encoding="utf-8")
    added = _operand_tag(tmp_path)
    _git(tmp_path, "add", path)
    assert _operand_tag(tmp_path) == added
    changed.write_text("second\n", encoding="utf-8")
    assert _operand_tag(tmp_path) not in {original, added}
    changed.unlink()
    assert _operand_tag(tmp_path) == original


def test_operand_tag_ignores_private_working_material(tmp_path: Path) -> None:
    _git(tmp_path, "init")
    source = tmp_path / "src/app.py"
    source.parent.mkdir()
    source.write_text("app\n", encoding="utf-8")
    (tmp_path / ".gitignore").write_text(".local/\n", encoding="utf-8")
    _git(tmp_path, "add", "src/app.py", ".gitignore")
    original = _operand_tag(tmp_path)
    note = tmp_path / "operator/.local/review.md"
    note.parent.mkdir(parents=True)
    note.write_text("private\n", encoding="utf-8")
    assert _operand_tag(tmp_path) == original


def test_operand_tag_changes_when_a_submodule_pointer_changes(
    tmp_path: Path,
) -> None:
    if shutil.which("bash") is None or shutil.which("git") is None:
        pytest.skip("bash and git are required for operand tag checks")

    repository = tmp_path / "repository"
    source = repository / "src/app.py"
    source.parent.mkdir(parents=True)
    source.write_text("print('operand')\n", encoding="utf-8")
    _git(repository, "init")
    _git(repository, "add", "src/app.py")

    gitlink = "src/vendor/bbc-tams"
    _git(
        repository,
        "update-index",
        "--add",
        "--cacheinfo",
        f"160000,{'1' * 40},{gitlink}",
    )
    first = _operand_tag(repository)

    _git(
        repository,
        "update-index",
        "--cacheinfo",
        f"160000,{'2' * 40},{gitlink}",
    )
    second = _operand_tag(repository)

    assert TAG_PATTERN.match(first), first
    assert TAG_PATTERN.match(second), second
    assert first != second


@pytest.mark.parametrize(
    "profile", ["local-kind", "single-server", "multi-server", "edge"]
)
def test_kind_build_loads_and_renders_one_operand_tag(profile: str) -> None:
    """Every consumer of the operand tag must agree within a single build.

    The images are built and loaded under the operand tag, while the operator
    renders workloads from the tag compiled into it as OPERAND_VERSION. If those
    diverge the cluster references an image that was never loaded, so the
    agreement matters more than the tag's own value.
    """
    if any(shutil.which(binary) is None for binary in _PLAN_BINARIES):
        pytest.skip(f"{_PLAN_BINARIES_MSG} are required for kind wiring checks")

    plan = _kind_image_plan(profile)

    # The operator image is built through make rather than task_kind_build_image.
    built = set(re.findall(r'task_kind_build_image "[^"]+" "([^"]+)"', plan))
    built |= set(re.findall(r'IMG="([^"]+)"', plan))
    loaded = set(re.findall(r'task_kind_load_image "[^"]+" "[^"]+" "([^"]+)"', plan))
    operand_versions = set(re.findall(r'OPERAND_VERSION="([^"]+)"', plan))

    assert built, f"no image builds found in the kind image plan:\n{plan}"
    assert built == loaded, "kind builds images it does not load, or vice versa"

    operand_tags = {
        image.rsplit(":", 1)[1]
        for image in built
        if not image.startswith("livewyer/tamoss-operator:")
    }
    assert len(operand_tags) == 1, f"operand images disagree on a tag: {operand_tags}"
    assert operand_versions == operand_tags, (
        "the operator renders a different tag than the operands were built with"
    )
    registry = yaml.safe_load((ROOT / "deploy/profiles.yaml").read_text())
    environment = next(
        entry["kindEnvironmentDir"]
        for entry in registry["profiles"]
        if entry["id"] == profile
    )
    rendered = subprocess.run(
        ["kubectl", "kustomize", environment],
        cwd=ROOT,
        capture_output=True,
        check=True,
        text=True,
    ).stdout
    instance = next(
        item for item in yaml.safe_load_all(rendered) if item["kind"] == "Tamoss"
    )
    for component in ("api", "ui", "console"):
        tag = instance["spec"].get(component, {}).get("image", {}).get("tag")
        assert not tag or tag in operand_tags, (
            f"{profile} references unloaded {component} image tag: {tag}"
        )


def test_kind_operator_image_matches_the_pinned_overlay_tag() -> None:
    """The operator image is applied by Kustomize, not rendered by itself.

    Its tag therefore has to match the overlay pin rather than following the
    operand tag; otherwise kind:up loads an image the Deployment never requests.
    """
    if any(shutil.which(binary) is None for binary in _PLAN_BINARIES):
        pytest.skip(f"{_PLAN_BINARIES_MSG} are required for kind wiring checks")

    overlay = yaml.safe_load(
        (ROOT / "operator/config/local/kustomization.yaml").read_text(encoding="utf-8")
    )
    pinned = {
        f"{entry.get('newName', entry['name'])}:{entry['newTag']}"
        for entry in overlay["images"]
    }

    plan = _kind_image_plan()
    built = set(re.findall(r'task_kind_build_image "[^"]+" "([^"]+)"', plan))
    built |= set(re.findall(r'IMG="([^"]+)"', plan))

    operator_images = {
        image for image in built if image.startswith("livewyer/tamoss-operator:")
    }
    assert operator_images == pinned, (
        f"kind builds {operator_images} but the overlay pins {pinned}"
    )


@pytest.mark.parametrize(
    "entry_point",
    [
        "kind:create",
        "kind:up",
        "kind:e2e",
        "kind:operator:reload",
        "operator:image:build",
    ],
)
@pytest.mark.parametrize("source", ["default", "cli", "environment"])
def test_schema_versions_remain_consistent_across_entry_points(
    entry_point: str, source: str
) -> None:
    if any(
        shutil.which(binary) is None for binary in (*_PLAN_BINARIES, "helm", "helmfile")
    ):
        pytest.skip("Task, Docker and the Kubernetes deployment tools are required")
    environment = dict(os.environ)
    for key in ("SCHEMA_VERSION", "PREVIOUS_SCHEMA_VERSION"):
        environment.pop(key, None)
    overrides = []
    operator_build = entry_point == "operator:image:build"
    expected = "0.0.1" if operator_build else "dev-20260810-0007"
    previous = "" if operator_build else "0.0.1"
    if source != "default":
        expected, previous = "custom-schema", "previous-schema"
        values = {"SCHEMA_VERSION": expected, "PREVIOUS_SCHEMA_VERSION": previous}
        if source == "cli":
            overrides = [f"{key}={value}" for key, value in values.items()]
        else:
            environment.update(values)
    result = subprocess.run(
        ["task", "--verbose", "--dry", entry_point, "VERSION=0.0.1", *overrides],
        cwd=ROOT,
        env=environment,
        capture_output=True,
        check=True,
        text=True,
    )
    plan = result.stdout + result.stderr
    assert set(re.findall(r'(?<!PREVIOUS_)SCHEMA_VERSION="([^"]+)"', plan)) == {
        expected
    }
    assert set(re.findall(r'PREVIOUS_SCHEMA_VERSION="([^"]*)"', plan)) == {previous}


def _kind_image_plan(profile: str = "local-kind") -> str:
    """Dry-run the image subtree of kind:up rather than kind:up itself.

    Task evaluates the preconditions of every task it would call, including
    under --dry, and the tail of kind:up reaches env:platform:apply, which
    requires helm and helmfile. Those are not present on a stock CI runner, so
    dry-running kind:up there fails the precondition and exits 201 before
    printing any plan. kind:create is the subtree that builds and loads the
    images, so it carries the whole of what these tests assert on while needing
    only docker, kind and yq.
    """
    # Task writes its verbose command trace to stderr, so both streams are needed
    # to see the image references a run would use.
    result = subprocess.run(
        ["task", "--verbose", "--dry", "kind:create", f"PROFILE={profile}"],
        cwd=ROOT,
        capture_output=True,
        check=True,
        text=True,
    )
    return result.stdout + result.stderr


def _operand_tag(cwd: Path) -> str:
    return subprocess.run(
        ["bash", "-c", f'. "{KIND_LIB}"; task_kind_operand_tag'],
        cwd=cwd,
        capture_output=True,
        check=True,
        text=True,
    ).stdout


def _git(repository: Path, *args: str) -> None:
    subprocess.run(
        ["git", *args],
        cwd=repository,
        capture_output=True,
        check=True,
        text=True,
    )
