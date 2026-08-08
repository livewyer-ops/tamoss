from __future__ import annotations

import os
import shutil
import subprocess
from pathlib import Path
from typing import Any

import pytest
import yaml

ROOT = Path(__file__).resolve().parents[1]


def test_profile_registry_selects_kind_configurations() -> None:
    registry = _load_yaml(ROOT / "deploy/profiles.yaml")
    profiles = {item["id"]: item for item in registry["profiles"]}

    assert profiles["local-kind"]["kindConfig"] == "deploy/kind.yaml"
    assert profiles["single-server"]["kindConfig"] == "deploy/kind.yaml"
    assert profiles["multi-server"]["kindConfig"] == "deploy/kind-multi-server.yaml"
    assert profiles["edge"]["kindConfig"] == "deploy/kind.yaml"
    assert (
        profiles["local-kind"]["kindEnvironmentDir"] == "deploy/environments/local-kind"
    )
    assert (
        profiles["single-server"]["kindEnvironmentDir"]
        == "deploy/environments/single-server"
    )
    assert (
        profiles["multi-server"]["kindEnvironmentDir"]
        == "deploy/environments/multi-server"
    )
    assert profiles["edge"]["kindEnvironmentDir"] == "deploy/environments/edge"
    assert profiles["local-kind"]["instanceKustomizeDir"] == (
        "deploy/instances/local-kind"
    )
    assert profiles["single-server"]["instanceKustomizeDir"] == (
        "deploy/instances/single-server"
    )
    assert profiles["multi-server"]["instanceKustomizeDir"] == (
        "deploy/instances/multi-server"
    )
    assert profiles["edge"]["instanceKustomizeDir"] == "deploy/instances/edge"
    assert profiles["local-kind"]["targetEnv"] == "tests/targets/local-kind.env"
    assert profiles["single-server"]["targetEnv"] == "tests/targets/single-server.env"
    assert profiles["multi-server"]["targetEnv"] == "tests/targets/multi-server.env"
    assert profiles["edge"]["targetEnv"] == "tests/targets/edge.env"
    for profile in profiles.values():
        assert "platformKustomizeDir" not in profile
        assert "remotePlatformKustomizeDir" not in profile
        assert (ROOT / profile["kindConfig"]).is_file()
        assert (ROOT / profile["kindEnvironmentDir"]).is_dir()
        assert (ROOT / profile["instanceKustomizeDir"]).is_dir()
        assert (ROOT / profile["kindEnvironmentDir"] / "platform-values.yaml").is_file()
        assert (ROOT / profile["targetEnv"]).is_file()


def test_multi_server_kind_config_is_multi_node_with_single_https_mapping() -> None:
    config = _load_yaml(ROOT / "deploy/kind-multi-server.yaml")
    nodes = config["nodes"]

    assert [node["role"] for node in nodes] == [
        "control-plane",
        "worker",
        "worker",
        "worker",
    ]
    https_mappings = [
        mapping
        for node in nodes
        for mapping in node.get("extraPortMappings", [])
        if mapping.get("hostPort") == 443
    ]
    assert https_mappings == [
        {
            "containerPort": 30443,
            "hostPort": 443,
            "listenAddress": "0.0.0.0",
            "protocol": "TCP",
        }
    ]


def test_profile_shell_helper_resolves_kind_config() -> None:
    if shutil.which("bash") is None or shutil.which("yq") is None:
        pytest.skip("bash and yq are required for task profile helper checks")

    result = subprocess.run(
        [
            "bash",
            "-c",
            ". .tasks/lib/profile.sh; task_profile_kind_config multi-server",
        ],
        cwd=ROOT,
        capture_output=True,
        check=True,
        text=True,
    )

    assert result.stdout.strip() == "deploy/kind-multi-server.yaml"


def test_profile_shell_validation_checks_supported_profile_paths() -> None:
    if shutil.which("bash") is None or shutil.which("yq") is None:
        pytest.skip("bash and yq are required for task profile helper checks")

    for profile in ["local-kind", "single-server", "multi-server", "edge"]:
        subprocess.run(
            [
                "bash",
                "-c",
                f". .tasks/lib/profile.sh; task_validate_profile {profile}",
            ],
            cwd=ROOT,
            capture_output=True,
            check=True,
            text=True,
        )


def test_kind_environments_allow_cluster_webhook_receiver() -> None:
    if shutil.which("kubectl") is None:
        pytest.skip("kubectl is required for Kustomize overlay checks")

    for environment in ["single-server", "multi-server", "edge"]:
        tamoss = _render_tamoss(ROOT / "deploy/environments" / environment)

        assert (
            tamoss["spec"]["api"]["env"]["TAMOSS_WEBHOOK_ALLOWED_HOSTS"]
            == ".svc.cluster.local"
        )
        assert (
            tamoss["spec"]["worker"]["env"]["TAMOSS_WEBHOOK_ALLOWED_HOSTS"]
            == ".svc.cluster.local"
        )


def test_canonical_profiles_do_not_default_to_localtest_domains() -> None:
    if shutil.which("kubectl") is None:
        pytest.skip("kubectl is required for Kustomize overlay checks")

    for overlay in ["single-server", "multi-server", "edge"]:
        rendered = subprocess.run(
            ["kubectl", "kustomize", str(ROOT / "deploy/instances" / overlay)],
            cwd=ROOT,
            capture_output=True,
            check=True,
            text=True,
        ).stdout

        assert "tamoss.localtest.me" not in rendered


def test_kind_environments_keep_localtest_domains() -> None:
    if shutil.which("kubectl") is None:
        pytest.skip("kubectl is required for Kustomize overlay checks")

    for environment in ["single-server", "multi-server", "edge"]:
        tamoss = _render_tamoss(ROOT / "deploy/environments" / environment)

        assert tamoss["spec"]["publicEndpoint"]["baseDomain"] == "tamoss.localtest.me"


def test_kind_multi_server_platform_values_render_nodeport() -> None:
    if shutil.which("helm") is None:
        pytest.skip("helm is required for platform Helmfile checks")
    if shutil.which("helmfile") is None:
        pytest.skip("helmfile is required for platform Helmfile checks")

    rendered = _render_platform(
        ROOT / "deploy/environments/multi-server/platform-values.yaml"
    )
    service = _resource(rendered, "Service", "traefik", "traefik")
    websecure = next(
        port for port in service["spec"]["ports"] if port["name"] == "websecure"
    )

    assert service["spec"]["type"] == "NodePort"
    assert websecure["nodePort"] == 30443
    assert (
        _resource(rendered, "Secret", "authentik", "auth")["metadata"]["name"]
        == "authentik"
    )

    worker = _resource(rendered, "Deployment", "authentik-worker", "auth")
    container = next(
        item
        for item in worker["spec"]["template"]["spec"]["containers"]
        if item["name"] == "worker"
    )
    for probe_name in ["startupProbe", "livenessProbe", "readinessProbe"]:
        assert container[probe_name]["timeoutSeconds"] == 30


def test_profile_platform_values_enable_authentik_by_default() -> None:
    registry = _load_yaml(ROOT / "deploy/profiles.yaml")
    for profile in registry["profiles"]:
        values = _load_yaml(
            ROOT / profile["kindEnvironmentDir"] / "platform-values.yaml"
        )
        if profile["id"] == "edge":
            assert values["authentik"]["enabled"] is False
        else:
            assert values["authentik"]["enabled"] is True

    for values_file in [
        ROOT / "deploy/platform/values/local-kind.yaml",
        ROOT / "deploy/platform/values/single-server-reference.yaml",
        ROOT / "deploy/platform/values/multi-server-reference.yaml",
    ]:
        values = _load_yaml(values_file)
        assert values["authentik"]["enabled"] is True

    values = _load_yaml(ROOT / "deploy/platform/values/edge-reference.yaml")
    assert values["authentik"]["enabled"] is False


def test_default_authentik_worker_probe_timeouts_allow_slow_startup() -> None:
    values = _load_yaml(ROOT / "deploy/platform/values/defaults.yaml")
    worker = values["authentikChart"]["worker"]

    for probe_name in ["startupProbe", "livenessProbe", "readinessProbe"]:
        assert worker[probe_name]["timeoutSeconds"] == 30


def test_reference_platform_values_render_profile_authentik_host() -> None:
    if shutil.which("helm") is None:
        pytest.skip("helm is required for platform Helmfile checks")
    if shutil.which("helmfile") is None:
        pytest.skip("helmfile is required for platform Helmfile checks")

    for values_file in [
        ROOT / "deploy/platform/values/single-server-reference.yaml",
        ROOT / "deploy/platform/values/multi-server-reference.yaml",
    ]:
        rendered = _render_platform(values_file)
        auth_ingresses = [
            item
            for item in rendered
            if item.get("kind") == "Ingress"
            and item.get("metadata", {}).get("namespace") == "auth"
        ]

        assert len(auth_ingresses) == 1
        assert (
            auth_ingresses[0]["spec"]["rules"][0]["host"] == "auth.tamoss.example.com"
        )
        assert "localtest" not in yaml.safe_dump(auth_ingresses[0])


def test_edge_platform_values_do_not_render_authentik() -> None:
    if shutil.which("helm") is None:
        pytest.skip("helm is required for platform Helmfile checks")
    if shutil.which("helmfile") is None:
        pytest.skip("helmfile is required for platform Helmfile checks")

    rendered = _render_platform(ROOT / "deploy/platform/values/edge-reference.yaml")
    assert not [
        item for item in rendered if item.get("metadata", {}).get("namespace") == "auth"
    ]


def test_multi_server_topology_guard_requires_two_ready_nodes(tmp_path: Path) -> None:
    if shutil.which("bash") is None:
        pytest.skip("bash is required for task shell helper checks")

    _write_kubectl_stub(
        tmp_path,
        "tams-control-plane Ready control-plane 1m v1.34.0\n"
        "tams-worker Ready <none> 1m v1.34.0\n",
    )
    assert _run_topology_guard(tmp_path).returncode == 0

    _write_kubectl_stub(
        tmp_path,
        "tams-control-plane Ready control-plane 1m v1.34.0\n",
    )
    result = _run_topology_guard(tmp_path)

    assert result.returncode == 1
    assert "Expected at least 2 Ready Kind nodes, found 1." in result.stderr


def _load_yaml(path: Path) -> Any:
    return yaml.safe_load(path.read_text(encoding="utf-8"))


def _render_tamoss(path: Path) -> dict[str, Any]:
    result = subprocess.run(
        ["kubectl", "kustomize", str(path)],
        cwd=ROOT,
        capture_output=True,
        check=True,
        text=True,
    )
    for item in yaml.safe_load_all(result.stdout):
        if item.get("kind") == "Tamoss":
            return item
    raise AssertionError(f"No Tamoss resource rendered from {path}")


def _render_platform(values_path: Path) -> list[dict[str, Any]]:
    result = subprocess.run(
        [
            "helmfile",
            "--file",
            "helmfile.yaml.gotmpl",
            "--state-values-file",
            "values/defaults.yaml",
            "--state-values-file",
            str(values_path),
            "template",
            "--include-crds",
        ],
        cwd=ROOT / "deploy/platform",
        capture_output=True,
        check=True,
        text=True,
    )
    return [
        item
        for item in yaml.safe_load_all(result.stdout)
        if isinstance(item, dict) and item.get("kind")
    ]


def _resource(
    resources: list[dict[str, Any]], kind: str, name: str, namespace: str | None = None
) -> dict[str, Any]:
    for item in resources:
        metadata = item.get("metadata", {})
        if (
            item.get("kind") == kind
            and metadata.get("name") == name
            and (namespace is None or metadata.get("namespace") == namespace)
        ):
            return item
    raise AssertionError(f"No {kind}/{name} found")


def _write_kubectl_stub(tmp_path: Path, output: str) -> None:
    kubectl = tmp_path / "kubectl"
    kubectl.write_text(
        f"#!/usr/bin/env bash\ncat <<'EOF'\n{output}EOF\n", encoding="utf-8"
    )
    kubectl.chmod(0o755)


def _run_topology_guard(tmp_path: Path) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env["PATH"] = f"{tmp_path}:{env['PATH']}"
    return subprocess.run(
        [
            "bash",
            "-c",
            ". .tasks/lib/kind.sh; "
            "task_kind_validate_profile_topology multi-server dummy",
        ],
        cwd=ROOT,
        capture_output=True,
        check=False,
        text=True,
        env=env,
    )
