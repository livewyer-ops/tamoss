from __future__ import annotations

import shutil
import subprocess
import textwrap
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]

INSTANCE_MANIFEST = """\
apiVersion: v1
kind: Namespace
metadata:
  name: {namespace}
---
apiVersion: tamoss.livewyer.io/v1alpha1
kind: Tamoss
metadata:
  name: {name}
  namespace: {namespace}
spec:
  profile: multi-server
  publicEndpoint:
    baseDomain: {name}.example.com
"""


@pytest.fixture(name="rendered")
def rendered_fixture(tmp_path: Path) -> Path:
    """Render a two-instance environment the same way the tasks do."""
    if shutil.which("kubectl") is None or shutil.which("yq") is None:
        pytest.skip("kubectl and yq are required for environment selection checks")

    environment = tmp_path / "env"
    environment.mkdir()
    for name, namespace in (("prod-a", "prod-a"), ("prod-b", "prod-b")):
        (environment / f"{name}.yaml").write_text(
            INSTANCE_MANIFEST.format(name=name, namespace=namespace),
            encoding="utf-8",
        )
    (environment / "kustomization.yaml").write_text(
        textwrap.dedent(
            """\
            apiVersion: kustomize.config.k8s.io/v1beta1
            kind: Kustomization

            resources:
              - prod-a.yaml
              - prod-b.yaml
            """
        ),
        encoding="utf-8",
    )

    target = tmp_path / "rendered.yaml"
    target.write_text(
        subprocess.run(
            ["kubectl", "kustomize", str(environment)],
            check=True,
            capture_output=True,
            text=True,
        ).stdout,
        encoding="utf-8",
    )
    return target


def run_env_helper(script: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["bash", "-c", f". .tasks/lib/env.sh\n{script}"],
        cwd=ROOT,
        capture_output=True,
        text=True,
        check=False,
    )


def test_every_instance_is_listed_in_composition_order(rendered: Path) -> None:
    result = run_env_helper(f'task_tamoss_names_from_rendered "{rendered}"')

    assert result.returncode == 0, result.stderr
    assert result.stdout.split() == ["prod-a", "prod-b"]


def test_unset_instance_selects_all_instances(rendered: Path) -> None:
    result = run_env_helper(f'task_tamoss_instances_for "{rendered}"')

    assert result.returncode == 0, result.stderr
    assert result.stdout.split() == ["prod-a", "prod-b"]


def test_named_instance_narrows_the_selection(rendered: Path) -> None:
    result = run_env_helper(f'task_tamoss_instances_for "{rendered}" prod-b')

    assert result.returncode == 0, result.stderr
    assert result.stdout.split() == ["prod-b"]


def test_unknown_instance_fails_and_lists_the_available_instances(
    rendered: Path,
) -> None:
    result = run_env_helper(f'task_tamoss_instances_for "{rendered}" prod-c')

    assert result.returncode != 0
    assert "prod-c was not found" in result.stderr
    assert "prod-a" in result.stderr
    assert "prod-b" in result.stderr


def test_fields_resolve_per_instance(rendered: Path) -> None:
    """Without this the tasks report the first instance for every instance."""
    first = run_env_helper(
        f'task_tamoss_field_from_rendered "{rendered}" namespace prod-a'
    )
    second = run_env_helper(
        f'task_tamoss_field_from_rendered "{rendered}" namespace prod-b'
    )

    assert first.stdout.strip() == "prod-a", first.stderr
    assert second.stdout.strip() == "prod-b", second.stderr


def test_instance_init_registers_the_manifest(tmp_path: Path) -> None:
    if shutil.which("yq") is None:
        pytest.skip("yq is required for environment instance creation")

    environment = tmp_path / "env"
    environment.mkdir()
    (environment / "kustomization.yaml").write_text(
        textwrap.dedent(
            """\
            apiVersion: kustomize.config.k8s.io/v1beta1
            kind: Kustomization

            resources: []
            """
        ),
        encoding="utf-8",
    )

    created = run_env_helper(
        f'task_init_env_instance "{environment}" prod-a multi-server '
        "prod-a.example.com prod-a"
    )
    assert created.returncode == 0, created.stderr

    manifest = environment / "prod-a.yaml"
    assert manifest.exists()
    assert "name: prod-a" in manifest.read_text(encoding="utf-8")
    assert "prod-a.yaml" in (environment / "kustomization.yaml").read_text(
        encoding="utf-8"
    )

    repeated = run_env_helper(
        f'task_init_env_instance "{environment}" prod-a multi-server '
        "prod-a.example.com prod-a"
    )
    assert repeated.returncode != 0
    assert "already exists" in repeated.stderr
