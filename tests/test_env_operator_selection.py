from __future__ import annotations

import os
import shutil
import subprocess
from pathlib import Path

import pytest

from tests.support.paths import REPO_ROOT


@pytest.mark.parametrize("operation", ["apply", "diff"])
@pytest.mark.parametrize("overlay", ["missing", "invalid", "valid"])
def test_operator_overlay_is_validated_before_platform_commands(
    tmp_path: Path, operation: str, overlay: str
) -> None:
    if shutil.which("kubectl") is None:
        pytest.skip("kubectl is required to render operator overlays")
    operator = tmp_path / "operator"
    if overlay != "missing":
        operator.mkdir()
        (operator / "kustomization.yaml").write_text(
            "resources: []\n" if overlay == "valid" else "resources: [absent.yaml]\n"
        )
    (tmp_path / "platform-values.yaml").touch()
    helmfile = tmp_path / "helmfile.yaml"
    helmfile.touch()
    command = (
        'task_apply_env "$TEST_ENV" kubeconfig "$HELMFILE" "$OPERATOR" 15m'
        if operation == "apply"
        else ('task_diff_env "$TEST_ENV" kubeconfig "$HELMFILE" 15m "$OPERATOR"')
    )
    result = subprocess.run(
        [
            "bash",
            "-c",
            ". .tasks/lib/env.sh\n"
            "task_render_environment() { :; }\n"
            "task_tamoss_instances_for() { printf 'instance\\n'; }\n"
            "task_apply_env_platform() { printf 'platform reached\\n'; return 42; }\n"
            "task_diff_platform_helmfile() { task_apply_env_platform; }\n" + command,
        ],
        cwd=REPO_ROOT,
        env={
            **os.environ,
            "TEST_ENV": str(tmp_path),
            "OPERATOR": str(operator),
            "HELMFILE": str(helmfile),
        },
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode != 0
    assert ("platform reached" in result.stdout) == (overlay == "valid")
    if overlay == "missing":
        assert f"Operator overlay {operator} was not found" in result.stderr
        assert "upgrades.md#environment-layout-migration" in result.stderr
    elif overlay == "invalid":
        assert f"Cannot render operator overlay {operator}" in result.stderr


@pytest.mark.parametrize("operation", ["apply", "diff"])
@pytest.mark.parametrize("override", ["none", "environment", "cli"])
def test_public_environment_tasks_select_only_explicit_operator_sources(
    tmp_path: Path, operation: str, override: str
) -> None:
    if shutil.which("task") is None or shutil.which("yq") is None:
        pytest.skip("task and yq are required to check public entry points")
    env = os.environ.copy()
    env.pop("OPERATOR_KUSTOMIZE_DIR", None)
    binaries = tmp_path / "bin"
    binaries.mkdir()
    for name in ("helm", "helmfile", "kubectl"):
        executable = binaries / name
        executable.write_text("#!/bin/sh\nexit 0\n")
        executable.chmod(0o755)
    env["PATH"] = f"{binaries}:{env['PATH']}"
    args = ["task", f"env:{operation}", f"ENV_DIR={tmp_path}", "KUBECONFIG=unused"]
    selected = tmp_path / "operator"
    if override != "none":
        selected = tmp_path / "explicit-operator"
        if override == "environment":
            env["OPERATOR_KUSTOMIZE_DIR"] = str(selected)
        else:
            env["OPERATOR_KUSTOMIZE_DIR"] = str(tmp_path / "overridden")
            args.append(f"OPERATOR_KUSTOMIZE_DIR={selected}")
    result = subprocess.run(
        args, cwd=REPO_ROOT, env=env, capture_output=True, text=True, check=False
    )
    assert result.returncode != 0
    output = result.stdout + result.stderr
    assert f"Operator overlay {selected} was not found" in output
