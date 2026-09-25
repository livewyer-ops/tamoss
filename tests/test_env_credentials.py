from __future__ import annotations

import os
import shutil
import subprocess
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]


@pytest.mark.parametrize(
    ("profile", "expected_credential"),
    [("multi-server", False), ("local-kind", True)],
)
def test_summary_reads_credentials_only_for_local_kind(
    tmp_path: Path, profile: str, expected_credential: bool
) -> None:
    if shutil.which("yq") is None:
        pytest.skip("yq is required for environment summary checks")

    manifest = tmp_path / "rendered.yaml"
    manifest.write_text(
        f"""\
apiVersion: tamoss.livewyer.io/v1alpha1
kind: Tamoss
metadata:
  name: demo
  namespace: demo
spec:
  profile: {profile}
  publicEndpoint:
    baseDomain: example.test
""",
        encoding="utf-8",
    )
    secret_calls = tmp_path / "secret-calls"
    environment = os.environ | {
        "MOCK_RENDERED": str(manifest),
        "SECRET_CALLS": str(secret_calls),
    }
    result = subprocess.run(
        [
            "bash",
            "-c",
            """\
. .tasks/lib/env.sh
task_render_environment() { cp "$MOCK_RENDERED" "$2"; }
task_require_cluster_access() { :; }
task_k8s_resource_value() { :; }
kubectl() { :; }
task_k8s_secret_value() {
  printf '%s/%s\n' "$3" "$4" >> "$SECRET_CALLS"
  printf 'test-secret'
}
task_k8s_secret_first_value() { printf 'test-secret'; }
task_print_env_summary env kubeconfig '' '' auto
""",
        ],
        cwd=ROOT,
        env=environment,
        capture_output=True,
        text=True,
        check=False,
    )

    assert result.returncode == 0, result.stderr
    assert ("API Token:        test-secret" in result.stdout) is expected_credential
    assert ("task env:credentials" in result.stdout) is not expected_credential
    assert secret_calls.exists() is expected_credential
