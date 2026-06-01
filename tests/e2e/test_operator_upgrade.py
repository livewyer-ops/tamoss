from __future__ import annotations

import json
import os
import subprocess
import time
from pathlib import Path
from string import Template

import pytest

from tests.e2e.commands import run_command as run
from tests.e2e.kubernetes import kubectl
from tests.support.paths import REPO_ROOT

pytestmark = [
    pytest.mark.e2e,
    pytest.mark.operator_upgrade,
    pytest.mark.slow,
]


def test_operator_upgrade_preserves_workload_pods_and_observes_generation(
    tmp_path: Path,
) -> None:
    kubeconfig_value = os.getenv("KUBECONFIG")
    if not kubeconfig_value:
        pytest.skip("set KUBECONFIG to the Kind cluster under test")
    kubeconfig = Path(kubeconfig_value)
    current_image = os.getenv(
        "TAMOSS_OPERATOR_CURRENT_IMAGE", "livewyer/tamoss-operator:dev"
    )
    current_config_dir = repo_path(
        os.getenv("TAMOSS_OPERATOR_CURRENT_CONFIG_DIR", "operator/config/default")
    )
    namespace = os.getenv("TAMOSS_NAMESPACE", "tams")
    name = os.getenv("TAMOSS_NAME", "tamoss-kind")

    wait_tamoss_ready(kubeconfig, namespace, name)

    pods_before = workload_pod_uids(kubeconfig, namespace, name)
    generation_before = tamoss_generation(kubeconfig, namespace, name)
    observed_before = tamoss_observed_generation(kubeconfig, namespace, name)
    assert observed_before == generation_before

    install_operator(tmp_path, kubeconfig, current_image, current_config_dir)
    wait_tamoss_ready(kubeconfig, namespace, name)
    assert workload_pod_uids(kubeconfig, namespace, name) == pods_before

    patch_tamoss_pause(kubeconfig, namespace, name, paused=True)
    wait_observed_generation(kubeconfig, namespace, name)
    assert workload_pod_uids(kubeconfig, namespace, name) == pods_before

    patch_tamoss_pause(kubeconfig, namespace, name, paused=False)
    wait_tamoss_ready(kubeconfig, namespace, name)
    wait_observed_generation(kubeconfig, namespace, name)
    assert workload_pod_uids(kubeconfig, namespace, name) == pods_before


def install_operator(
    tmp_path: Path, kubeconfig: Path, image: str, default_config_dir: Path
) -> None:
    repository, tag = split_image(image)
    overlay = tmp_path / "operator-kustomize"
    overlay.mkdir(exist_ok=True)
    default_config_ref = os.path.relpath(default_config_dir, overlay)
    (overlay / "kustomization.yaml").write_text(
        Template(
            (
                REPO_ROOT / "tests/fixtures/k8s/operator-upgrade-kustomization.yaml.tpl"
            ).read_text(encoding="utf-8")
        ).substitute(
            operator_default_config=default_config_ref,
            repository=repository,
            tag=tag,
        ),
        encoding="utf-8",
    )
    rendered = run(
        [
            "kubectl",
            "kustomize",
            "--load-restrictor=LoadRestrictionsNone",
            str(overlay),
        ],
        capture=True,
    )
    run_kubectl(
        kubeconfig,
        "apply",
        "--server-side",
        "-f",
        "-",
        input_text=rendered,
    )
    run_kubectl(
        kubeconfig,
        "-n",
        "tamoss-system",
        "rollout",
        "status",
        "deployment/operator-controller-manager",
        "--timeout=5m",
    )


def wait_tamoss_ready(kubeconfig: Path, namespace: str, name: str) -> None:
    run_kubectl(
        kubeconfig,
        "-n",
        namespace,
        "wait",
        "--for=condition=Ready",
        f"tamoss/{name}",
        "--timeout=15m",
    )


def wait_observed_generation(
    kubeconfig: Path, namespace: str, name: str, *, timeout_seconds: int = 180
) -> None:
    deadline = timeout_seconds
    while deadline > 0:
        if tamoss_observed_generation(kubeconfig, namespace, name) == tamoss_generation(
            kubeconfig, namespace, name
        ):
            return
        deadline -= 2
        time.sleep(2)
    raise AssertionError("Tamoss observedGeneration did not catch up")


def workload_pod_uids(kubeconfig: Path, namespace: str, name: str) -> dict[str, str]:
    output = run_kubectl(
        kubeconfig,
        "-n",
        namespace,
        "get",
        "pods",
        "-l",
        f"app.kubernetes.io/instance={name},app.kubernetes.io/name=tamoss",
        "--output=json",
    )
    data = json.loads(output)
    pods = {
        item["metadata"]["name"]: item["metadata"]["uid"]
        for item in data.get("items", [])
    }
    assert pods
    return pods


def tamoss_generation(kubeconfig: Path, namespace: str, name: str) -> int:
    return int(tamoss_jsonpath(kubeconfig, namespace, name, "{.metadata.generation}"))


def tamoss_observed_generation(kubeconfig: Path, namespace: str, name: str) -> int:
    value = tamoss_jsonpath(kubeconfig, namespace, name, "{.status.observedGeneration}")
    return int(value or "0")


def tamoss_jsonpath(kubeconfig: Path, namespace: str, name: str, jsonpath: str) -> str:
    return run_kubectl(
        kubeconfig,
        "-n",
        namespace,
        "get",
        "tamoss",
        name,
        "-o",
        f"jsonpath={jsonpath}",
    ).strip()


def patch_tamoss_pause(
    kubeconfig: Path, namespace: str, name: str, *, paused: bool
) -> None:
    value = "true" if paused else "false"
    run_kubectl(
        kubeconfig,
        "-n",
        namespace,
        "patch",
        "tamoss",
        name,
        "--type=merge",
        "-p",
        f'{{"spec":{{"paused":{value}}}}}',
    )


def run_kubectl(kubeconfig: Path, *args: str, input_text: str | None = None) -> str:
    try:
        return kubectl(
            kubeconfig=str(kubeconfig), args=list(args), input_text=input_text
        ).stdout
    except subprocess.CalledProcessError as exc:
        command = " ".join(str(part) for part in exc.cmd)
        output = "\n".join(
            part.strip()
            for part in (exc.stdout or "", exc.stderr or "")
            if part.strip()
        )
        raise AssertionError(f"{command} exited {exc.returncode}\n{output}") from exc


def repo_path(value: str) -> Path:
    path = Path(value)
    if path.is_absolute():
        return path
    return REPO_ROOT / path


def split_image(image: str) -> tuple[str, str]:
    if "@" in image:
        raise ValueError("operator upgrade e2e expects repository:tag images")
    repository, separator, tag = image.rpartition(":")
    if not separator or "/" in tag:
        raise ValueError(f"operator image must include an explicit tag: {image}")
    return repository, tag
