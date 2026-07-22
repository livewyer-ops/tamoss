from __future__ import annotations

import os
import shutil
import subprocess
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]


def test_missing_optional_kubernetes_secret_is_an_empty_success(
    tmp_path: Path,
) -> None:
    if shutil.which("bash") is None:
        pytest.skip("bash is required for environment helper checks")

    _write_fake_kubectl(tmp_path, exit_code=1)
    result = _run_bash(
        'value="$(task_k8s_secret_value missing.kubeconfig tams absent key)"; '
        'test -z "$value"',
        tmp_path,
    )

    assert result.returncode == 0, result.stderr


def test_cluster_access_failure_suggests_matching_environment_kubeconfig(
    tmp_path: Path,
) -> None:
    if shutil.which("bash") is None:
        pytest.skip("bash is required for environment helper checks")

    _write_fake_kubectl(tmp_path, exit_code=1)
    (tmp_path / "dev-tamoss-1.kubeconfig").touch()
    result = _run_bash(
        "task_require_cluster_access tams.kubeconfig deploy/environments/dev-tamoss-1",
        tmp_path,
    )

    assert result.returncode == 1
    assert (
        "Unable to reach Kubernetes using kubeconfig tams.kubeconfig." in result.stderr
    )
    assert "Try KUBECONFIG=dev-tamoss-1.kubeconfig" in result.stderr


def test_rustfs_access_is_only_printed_for_managed_rustfs(tmp_path: Path) -> None:
    if shutil.which("bash") is None:
        pytest.skip("bash is required for environment helper checks")

    external = _run_bash(
        "task_print_rustfs_access external https://s3.example.com user password",
        tmp_path,
    )
    managed = _run_bash(
        "task_print_rustfs_access rustfs-operator https://s3.example.com user password",
        tmp_path,
    )

    assert external.returncode == 0
    assert external.stdout == ""
    assert managed.returncode == 0
    assert "RustFS Admin URL: https://s3.example.com/rustfs/console/" in managed.stdout
    assert "RustFS Username:  user" in managed.stdout


def _run_bash(command: str, cwd: Path) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env["PATH"] = f"{cwd}:{env['PATH']}"
    return subprocess.run(
        [
            "bash",
            "-c",
            f'. "{ROOT / ".tasks/lib/env.sh"}"; {command}',
        ],
        cwd=cwd,
        env=env,
        capture_output=True,
        check=False,
        text=True,
    )


def _write_fake_kubectl(directory: Path, exit_code: int) -> None:
    executable = directory / "kubectl"
    executable.write_text(f"#!/usr/bin/env sh\nexit {exit_code}\n", encoding="utf-8")
    executable.chmod(0o755)
