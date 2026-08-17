from __future__ import annotations

import re
import shutil
import subprocess
from pathlib import Path

import pytest
import yaml

ROOT = Path(__file__).resolve().parents[1]
KIND_LIB = ROOT / ".tasks/lib/kind.sh"
TAG_PATTERN = re.compile(r"^dev-[0-9a-f]{12}$")


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


def test_kind_up_builds_loads_and_renders_one_operand_tag() -> None:
    """Every consumer of the operand tag must agree within a single kind:up.

    The images are built and loaded under the operand tag, while the operator
    renders workloads from the tag compiled into it as OPERAND_VERSION. If those
    diverge the cluster references an image that was never loaded, so the
    agreement matters more than the tag's own value.
    """
    if any(shutil.which(binary) is None for binary in ("task", "docker", "kind")):
        pytest.skip("task, docker and kind are required for kind wiring checks")

    plan = _kind_up_plan()

    # The operator image is built through make rather than task_kind_build_image.
    built = set(re.findall(r'task_kind_build_image "[^"]+" "([^"]+)"', plan))
    built |= set(re.findall(r'IMG="([^"]+)"', plan))
    loaded = set(re.findall(r'task_kind_load_image "[^"]+" "[^"]+" "([^"]+)"', plan))
    operand_versions = set(re.findall(r'OPERAND_VERSION="([^"]+)"', plan))

    assert built, f"no image builds found in kind:up plan:\n{plan}"
    assert built == loaded, "kind:up loads images it did not build"

    operand_tags = {
        image.rsplit(":", 1)[1]
        for image in built
        if not image.startswith("livewyer/tamoss-operator:")
    }
    assert len(operand_tags) == 1, f"operand images disagree on a tag: {operand_tags}"
    assert operand_versions == operand_tags, (
        "the operator renders a different tag than the operands were built with"
    )


def test_kind_up_operator_image_matches_the_pinned_overlay_tag() -> None:
    """The operator image is applied by Kustomize, not rendered by itself.

    Its tag therefore has to match the overlay pin rather than following the
    operand tag; otherwise kind:up loads an image the Deployment never requests.
    """
    if any(shutil.which(binary) is None for binary in ("task", "docker", "kind")):
        pytest.skip("task, docker and kind are required for kind wiring checks")

    overlay = yaml.safe_load(
        (ROOT / "operator/config/local/kustomization.yaml").read_text(encoding="utf-8")
    )
    pinned = {
        f"{entry.get('newName', entry['name'])}:{entry['newTag']}"
        for entry in overlay["images"]
    }

    plan = _kind_up_plan()
    built = set(re.findall(r'task_kind_build_image "[^"]+" "([^"]+)"', plan))
    built |= set(re.findall(r'IMG="([^"]+)"', plan))

    operator_images = {
        image for image in built if image.startswith("livewyer/tamoss-operator:")
    }
    assert operator_images == pinned, (
        f"kind:up builds {operator_images} but the overlay pins {pinned}"
    )


def _kind_up_plan() -> str:
    # Task writes its verbose command trace to stderr, so both streams are needed
    # to see the image references a run would use.
    result = subprocess.run(
        ["task", "--verbose", "--dry", "kind:up"],
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
