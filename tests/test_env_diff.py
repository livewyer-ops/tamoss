from __future__ import annotations

import os
import subprocess
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]


def run_env_helper(script: str, **variables: str) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env.update(variables)
    return subprocess.run(
        ["bash", "-c", f". .tasks/lib/env.sh\n{script}"],
        cwd=ROOT,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )


@pytest.mark.parametrize(
    ("diff_status", "expected_status"), [(0, 0), (1, 0), (2, 2), (7, 7)]
)
def test_kubectl_diff_only_normalises_difference_status(
    diff_status: int, expected_status: int
) -> None:
    result = run_env_helper(
        'kubectl() { return "$DIFF_STATUS"; }\n'
        "status=0; task_kubectl_diff kubeconfig -k operator || status=$?; "
        "printf '%s\\n' \"$status\"",
        DIFF_STATUS=str(diff_status),
    )

    assert result.returncode == 0, result.stderr
    assert result.stdout.strip() == str(expected_status)


def test_platform_diff_propagates_template_failure_without_calling_kubectl(
    tmp_path: Path,
) -> None:
    called = tmp_path / "kubectl-called"
    result = run_env_helper(
        "task_platform_helmfile() { return 42; }\n"
        'kubectl() { touch "$KUBECTL_CALLED"; return 0; }\n'
        "status=0; task_diff_platform_helmfile kubeconfig helmfile values 15m "
        "|| status=$?; "
        "printf '%s\\n' \"$status\"",
        KUBECTL_CALLED=str(called),
    )

    assert result.returncode == 0, result.stderr
    assert result.stdout.strip() == "42"
    assert not called.exists()


def test_instance_diff_uses_selected_kustomize_directory(tmp_path: Path) -> None:
    environment = tmp_path / "environment"
    instance_dir = environment / "instances" / "prod-a"
    instance_dir.mkdir(parents=True)
    (environment / "platform-values.yaml").touch()
    (instance_dir / "kustomization.yaml").write_text("resources: []\n")
    operator_dir = tmp_path / "operator"
    operator_dir.mkdir()
    helmfile = tmp_path / "helmfile.yaml"
    helmfile.touch()
    capture = tmp_path / "diff-args"
    result = run_env_helper(
        "task_diff_platform_helmfile() { :; }\n"
        'task_kubectl_diff() { printf "%s\\n" "$*" >> "$CAPTURE"; }\n'
        'task_diff_env "$ENVIRONMENT" kubeconfig "$HELMFILE" 15m "$OPERATOR" prod-a',
        ENVIRONMENT=str(environment),
        HELMFILE=str(helmfile),
        OPERATOR=str(operator_dir),
        CAPTURE=str(capture),
    )

    assert result.returncode == 0, result.stderr
    assert (
        capture.read_text(encoding="utf-8")
        .splitlines()[-1]
        .endswith(f"kubeconfig -k {instance_dir}")
    )


@pytest.mark.parametrize(("diff_status", "expected_status"), [(1, 0), (2, 2)])
def test_platform_diff_propagates_kubernetes_errors(
    diff_status: int, expected_status: int
) -> None:
    result = run_env_helper(
        "task_platform_helmfile() { printf 'rendered manifest\\n'; }\n"
        'kubectl() { return "$DIFF_STATUS"; }\n'
        "status=0; task_diff_platform_helmfile kubeconfig helmfile values 15m "
        "|| status=$?; "
        "printf '%s\\n' \"$status\"",
        DIFF_STATUS=str(diff_status),
    )

    assert result.returncode == 0, result.stderr
    assert result.stdout.strip() == str(expected_status)
