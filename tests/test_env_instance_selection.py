from __future__ import annotations

import os
import shutil
import subprocess
import textwrap
from pathlib import Path

import pytest
import yaml

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
---
apiVersion: tamoss.livewyer.io/v1alpha1
kind: StorageBackend
metadata:
  name: {name}-storage
  namespace: {namespace}
spec:
  provider: external-s3
"""


@pytest.fixture(name="rendered")
def rendered_fixture(tmp_path: Path) -> Path:
    """Render a two-instance environment the same way the tasks do."""
    if shutil.which("kubectl") is None or shutil.which("yq") is None:
        pytest.skip("kubectl and yq are required for environment selection checks")

    environment = tmp_path / "env"
    environment.mkdir()
    for name, namespace in (("prod-a", "prod-a"), ("prod-b", "prod-b")):
        instance_dir = environment / "instances" / name
        instance_dir.mkdir(parents=True)
        (instance_dir / "resources.yaml").write_text(
            INSTANCE_MANIFEST.format(name=name, namespace=namespace),
            encoding="utf-8",
        )
        (instance_dir / "kustomization.yaml").write_text(
            "resources:\n  - resources.yaml\n", encoding="utf-8"
        )
    (environment / "kustomization.yaml").write_text(
        textwrap.dedent(
            """\
            apiVersion: kustomize.config.k8s.io/v1beta1
            kind: Kustomization

            resources:
              - instances/prod-a
              - instances/prod-b
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


def test_instance_init_registers_a_kustomize_directory(tmp_path: Path) -> None:
    for executable in ("kubectl", "yq"):
        if shutil.which(executable) is None:
            pytest.skip(f"{executable} is required for environment instance creation")

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
        "prod-a.example.com prod-a 8.2.0-oss2"
    )
    assert created.returncode == 0, created.stderr

    instance_dir = environment / "instances" / "prod-a"
    assert (instance_dir / "namespace.yaml").exists()
    assert 'version: "8.2.0-oss2"' in (instance_dir / "tamoss.yaml").read_text(
        encoding="utf-8"
    )
    assert "instances/prod-a" in (environment / "kustomization.yaml").read_text(
        encoding="utf-8"
    )
    rendered = subprocess.check_output(
        ["kubectl", "kustomize", str(environment)], text=True
    )
    objects = list(yaml.safe_load_all(rendered))
    assert any(item["kind"] == "Namespace" for item in objects)
    assert any(item["kind"] == "Tamoss" for item in objects)

    repeated = run_env_helper(
        f'task_init_env_instance "{environment}" prod-a multi-server '
        "prod-a.example.com prod-a 8.2.0-oss2"
    )
    assert repeated.returncode != 0
    assert "already exists" in repeated.stderr


def test_instance_apply_uses_native_kustomize_for_generated_instances(
    tmp_path: Path,
) -> None:
    instance_dir = tmp_path / "env" / "instances" / "prod-a"
    instance_dir.mkdir(parents=True)
    (instance_dir / "kustomization.yaml").write_text("resources: []\n")
    called = tmp_path / "kubectl-called"
    env = os.environ.copy()
    env["KUBECTL_CALLED"] = str(called)
    env["ENVIRONMENT"] = str(instance_dir.parents[1])
    result = subprocess.run(
        [
            "bash",
            "-c",
            ". .tasks/lib/env.sh\n"
            'task_step() { shift; "$@"; }\n'
            'kubectl() { printf "%s\\n" "$*" > "$KUBECTL_CALLED"; }\n'
            'task_apply_env_instance "$ENVIRONMENT" kubeconfig prod-a',
        ],
        cwd=ROOT,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0, result.stderr
    assert result.stdout == ""
    assert (
        called.read_text(encoding="utf-8").strip().endswith(f"apply -k {instance_dir}")
    )


@pytest.mark.parametrize("defaults_revision", ["", "sha256:expected"])
def test_wait_checks_requested_release_and_generation_before_ready(
    defaults_revision: str,
) -> None:
    result = run_env_helper(
        """
task_step() { shift; "$@"; }
kubectl() {
  case "$*" in
    *jsonpath='{.spec.version}') printf 'selected-release' ;;
    *jsonpath='{.metadata.generation}') printf '7' ;;
    *) printf '%s\\n' "$*" ;;
  esac
}
"""
        + "task_wait_tamoss_instance kubeconfig media instance 1m "
        + f"'{defaults_revision}'\n"
    )
    assert result.returncode == 0, result.stderr
    lines = result.stdout.splitlines()
    assert "observedGeneration}=7" in lines[0]
    assert "currentVersion}=selected-release" in lines[1]
    if defaults_revision:
        assert f"appliedDefaultsRevision}}={defaults_revision}" in lines[2]
    assert "--for=condition=Ready" in lines[-1]


def test_generated_environment_inherits_hashed_operator_defaults(
    tmp_path: Path,
) -> None:
    for executable in ("task", "kubectl", "yq"):
        if shutil.which(executable) is None:
            pytest.skip(f"{executable} is required for environment generation")
    # Exercise the public Task entry point, including variables from other Taskfiles.
    env = os.environ.copy()
    env.pop("PROFILE", None)
    env.pop("DOMAIN", None)
    name = f"defaults-test-{os.getpid()}"
    environment = ROOT / "deploy" / "environments" / name
    try:
        subprocess.run(
            [
                "task",
                "env:init",
                f"NAME={name}",
                "PROFILE=single-server",
                "DOMAIN=example.com",
                "TAMOSS_VERSION=dev",
            ],
            cwd=ROOT,
            env=env,
            check=True,
            capture_output=True,
            text=True,
        )
        subprocess.run(
            [
                "task",
                "env:instance:init",
                f"ENV={name}",
                "INSTANCE=second",
                "TAMOSS_VERSION=dev",
            ],
            cwd=ROOT,
            env=env,
            check=True,
            capture_output=True,
            text=True,
        )
        rendered = subprocess.check_output(
            ["kubectl", "kustomize", str(environment)],
            text=True,
        )
        instances = [
            item for item in yaml.safe_load_all(rendered) if item["kind"] == "Tamoss"
        ]
        assert len(instances) == 2
        assert all(item["spec"] == {"version": "dev"} for item in instances)
        assert (environment / "instances" / "tamoss-single-server").is_dir()
        assert (environment / "instances" / "second").is_dir()
        operator = environment / "operator"
        # The published install is flattened and already contains hashed ConfigMaps.
        shutil.copyfile(
            ROOT / "deploy/operator/install.yaml", operator / "install.yaml"
        )
        kustomization = operator / "kustomization.yaml"
        composition = yaml.safe_load(kustomization.read_text())
        composition["resources"] = ["install.yaml"]
        kustomization.write_text(yaml.safe_dump(composition))

        def defaults_mount() -> str:
            objects = list(
                yaml.safe_load_all(
                    subprocess.check_output(
                        ["kubectl", "kustomize", str(operator)],
                        text=True,
                    )
                )
            )
            deployment = next(item for item in objects if item["kind"] == "Deployment")
            volumes = deployment["spec"]["template"]["spec"]["volumes"]
            config_name = next(
                item["configMap"]["name"]
                for item in volumes
                if item["name"] == "instance-defaults"
            )
            config = next(
                item
                for item in objects
                if item["kind"] == "ConfigMap"
                and item["metadata"]["name"] == config_name
            )
            assert config["immutable"] is True
            assert (
                config["data"]["defaults.yaml"]
                == (operator / "defaults.yaml").read_text()
            )
            return config_name

        original = defaults_mount()
        with (operator / "defaults.yaml").open("a") as config:
            config.write("ingressClassName: site-ingress\n")
        assert defaults_mount() != original
    finally:
        shutil.rmtree(environment, ignore_errors=True)
