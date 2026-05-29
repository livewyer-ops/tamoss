#!/usr/bin/env python3
"""Collect a redacted TAMOSS Kubernetes support bundle."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

REDACTED = "<redacted>"
LAST_APPLIED = "kubectl.kubernetes.io/last-applied-configuration"
SENSITIVE_ENV_NAMES = {
    "AUTHORIZATION",
    "AWS_ACCESS_KEY_ID",
    "AWS_SECRET_ACCESS_KEY",
    "API_KEY",
    "S3_ACCESS_KEY",
    "S3_SECRET_KEY",
    "AUTHENTIK_TOKEN",
    "API_KEY_VALUE",
    "TAMOSS_API_TOKEN",
    "POSTGRES_PASSWORD",
}
SENSITIVE_DOCUMENT_KEYS = {
    "api-key-value",
    "api_key_value",
    "apikeyvalue",
    "client-secret",
    "client_secret",
    "clientsecret",
    "database-url",
    "database_url",
    "databaseurl",
    "password",
    "passwd",
    "token",
}
SENSITIVE_KEY = re.compile(
    r"(password|passwd|token|secret|credential|api[_-]?key[_-]?value|access[_-]?key|secret[_-]?key|database[_-]?url|client[_-]?secret)",
    re.IGNORECASE,
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--kubeconfig", default="", help="Kubeconfig path")
    parser.add_argument("--namespace", default="tams", help="TAMOSS namespace")
    parser.add_argument("--name", default="", help="Optional Tamoss resource name")
    parser.add_argument(
        "--operator-namespace", default="tamoss-system", help="Operator namespace"
    )
    parser.add_argument(
        "--output-root", default=".local/support-bundles", help="Bundle root directory"
    )
    parser.add_argument("--tail", default="500", help="Log lines per container")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    timestamp = datetime.now(UTC).strftime("%Y%m%dT%H%M%SZ")
    target = args.name or "all"
    bundle_dir = Path(args.output_root) / f"{args.namespace}-{target}-{timestamp}"
    bundle_dir.mkdir(parents=True, exist_ok=False)

    collector = Collector(args.kubeconfig, bundle_dir)
    if not collector.check_namespace(args.namespace):
        return 2
    if args.operator_namespace != args.namespace and not collector.check_namespace(
        args.operator_namespace
    ):
        return 2
    tamoss_args = ["tamoss"]
    if args.name:
        tamoss_args.append(args.name)
    tamoss_document = collector.collect_json(
        args.namespace, tamoss_args, "namespace/tamoss.json"
    )
    storage_backend_document = collector.collect_json(
        args.namespace, ["storagebackends"], "namespace/storagebackends.json"
    )
    collector.write_json(
        "bundle.json",
        {
            "generatedAt": timestamp,
            "namespace": args.namespace,
            "tamossName": args.name or None,
            "operatorNamespace": args.operator_namespace,
            "versions": tamoss_version_summary(tamoss_document),
            "firstStart": first_start_summary(
                tamoss_document, storage_backend_document
            ),
            "redaction": {
                "secretValues": REDACTED,
                "lastAppliedAnnotation": REDACTED,
                "sensitiveConfigMapValues": REDACTED,
                "sensitivePodEnvValues": REDACTED,
            },
        },
    )
    for resource, filename in [
        ("clusters.postgresql.cnpg.io", "namespace/cnpg-clusters.json"),
        ("scheduledbackups.postgresql.cnpg.io", "namespace/cnpg-scheduledbackups.json"),
        ("pods", "namespace/pods.json"),
        ("deployments", "namespace/deployments.json"),
        ("statefulsets", "namespace/statefulsets.json"),
        ("replicasets", "namespace/replicasets.json"),
        ("services", "namespace/services.json"),
        ("configmaps", "namespace/configmaps.json"),
        ("secrets", "namespace/secrets.json"),
        ("ingress", "namespace/ingress.json"),
        ("httproutes.gateway.networking.k8s.io", "namespace/httproutes.json"),
        ("events", "namespace/events.json"),
    ]:
        collector.collect_json(args.namespace, [resource], filename)

    for resource, filename in [
        ("pods", "operator/pods.json"),
        ("deployments", "operator/deployments.json"),
        ("services", "operator/services.json"),
        ("configmaps", "operator/configmaps.json"),
        ("secrets", "operator/secrets.json"),
        ("events", "operator/events.json"),
    ]:
        collector.collect_json(args.operator_namespace, [resource], filename)

    for resource, filename in [
        (
            "customresourcedefinitions.apiextensions.k8s.io/tamosses.tamoss.livewyer.io",
            "cluster/crd-tamosses.json",
        ),
        (
            "customresourcedefinitions.apiextensions.k8s.io/storagebackends.tamoss.livewyer.io",
            "cluster/crd-storagebackends.json",
        ),
        (
            "validatingwebhookconfigurations",
            "cluster/validatingwebhookconfigurations.json",
        ),
    ]:
        collector.collect_json(None, [resource], filename)

    collector.collect_logs(args.namespace, "namespace", args.tail)
    collector.collect_logs(args.operator_namespace, "operator", args.tail)
    print(bundle_dir)
    return 0


class Collector:
    def __init__(self, kubeconfig: str, bundle_dir: Path) -> None:
        self.kubeconfig = kubeconfig
        self.bundle_dir = bundle_dir

    def kubectl(self, args: list[str]) -> subprocess.CompletedProcess[str]:
        command = ["kubectl"]
        if self.kubeconfig:
            command.extend(["--kubeconfig", self.kubeconfig])
        command.extend(args)
        return subprocess.run(command, text=True, capture_output=True, check=False)

    def collect_json(
        self, namespace: str | None, get_args: list[str], output: str
    ) -> Any | None:
        args = []
        if namespace:
            args.extend(["-n", namespace])
        args.extend(["get", *get_args, "-o", "json"])
        result = self.kubectl(args)
        if result.returncode != 0:
            self.write_error(output, result)
            return None
        try:
            document = json.loads(result.stdout)
        except json.JSONDecodeError:
            self.write_error(output, result, "kubectl did not return JSON")
            return None
        self.write_json(output, redact_document(document))
        return document

    def check_namespace(self, namespace: str) -> bool:
        result = self.kubectl(["get", "namespace", namespace, "-o", "json"])
        if result.returncode == 0:
            return True
        self.write_error(f"namespace/{namespace}.json", result)
        print(
            f"unable to access namespace {namespace}; see bundle errors",
            file=sys.stderr,
        )
        return False

    def collect_logs(self, namespace: str, scope: str, tail: str) -> None:
        result = self.kubectl(["-n", namespace, "get", "pods", "-o", "json"])
        if result.returncode != 0:
            self.write_error(f"logs/{scope}/pods.json", result)
            return
        try:
            pods = json.loads(result.stdout).get("items", [])
        except json.JSONDecodeError:
            self.write_error(
                f"logs/{scope}/pods.json", result, "kubectl did not return JSON"
            )
            return
        for pod in pods:
            pod_name = pod.get("metadata", {}).get("name", "")
            spec = pod.get("spec", {})
            containers = spec.get("initContainers", []) + spec.get("containers", [])
            for container in containers:
                container_name = container.get("name", "")
                if not pod_name or not container_name:
                    continue
                safe_name = safe_filename(f"{pod_name}_{container_name}.log")
                current = self.kubectl(
                    [
                        "-n",
                        namespace,
                        "logs",
                        pod_name,
                        "-c",
                        container_name,
                        "--tail",
                        tail,
                    ]
                )
                if current.returncode == 0:
                    self.write_text(f"logs/{scope}/current/{safe_name}", current.stdout)
                else:
                    self.write_error(f"logs/{scope}/current/{safe_name}", current)
                previous = self.kubectl(
                    [
                        "-n",
                        namespace,
                        "logs",
                        pod_name,
                        "-c",
                        container_name,
                        "--previous",
                        "--tail",
                        tail,
                    ]
                )
                if previous.returncode == 0:
                    self.write_text(
                        f"logs/{scope}/previous/{safe_name}", previous.stdout
                    )

    def write_json(self, output: str, document: Any) -> None:
        path = self.bundle_dir / output
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            json.dumps(document, indent=2, sort_keys=True) + "\n", encoding="utf-8"
        )

    def write_text(self, output: str, content: str) -> None:
        path = self.bundle_dir / output
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(redact_text(content), encoding="utf-8")

    def write_error(
        self,
        output: str,
        result: subprocess.CompletedProcess[str],
        message: str | None = None,
    ) -> None:
        error_path = self.bundle_dir / "errors" / safe_filename(output + ".txt")
        error_path.parent.mkdir(parents=True, exist_ok=True)
        body = []
        if message:
            body.append(message)
        body.append(f"command exited {result.returncode}")
        if result.stderr.strip():
            body.append(result.stderr.strip())
        error_path.write_text("\n".join(body) + "\n", encoding="utf-8")


def redact_document(document: Any) -> Any:
    redacted = json.loads(json.dumps(document))
    redact_value(redacted)
    return redacted


def tamoss_version_summary(document: Any | None) -> list[dict[str, Any]]:
    if not isinstance(document, dict):
        return []
    items = document.get("items")
    if not isinstance(items, list):
        items = [document]
    summary = []
    for item in items:
        if not isinstance(item, dict):
            continue
        metadata = item.get("metadata", {})
        status = item.get("status", {})
        if not isinstance(metadata, dict) or not isinstance(status, dict):
            continue
        resolved = status.get("resolved", {})
        versions = resolved.get("versions", {}) if isinstance(resolved, dict) else {}
        summary.append(
            {
                "namespace": metadata.get("namespace"),
                "name": metadata.get("name"),
                "schemaVersion": status.get("schemaVersion"),
                "resolvedVersions": versions if isinstance(versions, dict) else {},
            }
        )
    return summary


def first_start_summary(
    tamoss_document: Any | None, storage_backend_document: Any | None
) -> list[dict[str, Any]]:
    storage_backends = storage_backends_by_tamoss(storage_backend_document)
    summary = []
    for item in kubernetes_items(tamoss_document):
        metadata = item.get("metadata", {})
        status = item.get("status", {})
        if not isinstance(metadata, dict) or not isinstance(status, dict):
            continue
        name = metadata.get("name")
        namespace = metadata.get("namespace")
        phases = [
            phase(
                "dependenciesAndBackends",
                "Tamoss/BackendsReady",
                status_condition(status, "BackendsReady"),
            ),
            phase(
                "schemaMigration",
                "Tamoss/SchemaMigrated",
                status_condition(status, "SchemaMigrated"),
            ),
            phase(
                "identity",
                "Tamoss/IdentityReady",
                status_condition(status, "IdentityReady"),
            ),
            phase("workloads", "Tamoss/status.replicas", replica_phase(status)),
            phase(
                "routes",
                "Tamoss/RoutingReady",
                status_condition(status, "RoutingReady"),
            ),
        ]
        storage_backend = default_storage_backend(
            storage_backends.get((namespace, name), [])
        )
        if storage_backend is None:
            phases.insert(
                1,
                phase(
                    "defaultStorageBucket",
                    "StorageBackend/BucketReady",
                    skipped("StorageBackendNotManaged"),
                ),
            )
            phases.insert(
                2,
                phase(
                    "storageDatabaseRegistration",
                    "StorageBackend/DatabaseReady",
                    skipped("StorageBackendNotManaged"),
                ),
            )
        else:
            storage_status = storage_backend.get("status", {})
            phases.insert(
                1,
                phase(
                    "defaultStorageBucket",
                    "StorageBackend/BucketReady",
                    status_condition(storage_status, "BucketReady"),
                ),
            )
            phases.insert(
                2,
                phase(
                    "storageDatabaseRegistration",
                    "StorageBackend/DatabaseReady",
                    status_condition(storage_status, "DatabaseReady"),
                ),
            )
        summary.append(
            {
                "namespace": namespace,
                "name": name,
                "phases": phases,
            }
        )
    return summary


def kubernetes_items(document: Any | None) -> list[dict[str, Any]]:
    if not isinstance(document, dict):
        return []
    items = document.get("items")
    if not isinstance(items, list):
        items = [document]
    return [item for item in items if isinstance(item, dict)]


def storage_backends_by_tamoss(
    document: Any | None,
) -> dict[tuple[Any, Any], list[dict[str, Any]]]:
    grouped: dict[tuple[Any, Any], list[dict[str, Any]]] = {}
    for item in kubernetes_items(document):
        metadata = item.get("metadata", {})
        spec = item.get("spec", {})
        if not isinstance(metadata, dict) or not isinstance(spec, dict):
            continue
        tamoss_ref = spec.get("tamossRef", {})
        if not isinstance(tamoss_ref, dict):
            continue
        key = (metadata.get("namespace"), tamoss_ref.get("name"))
        grouped.setdefault(key, []).append(item)
    return grouped


def default_storage_backend(items: list[dict[str, Any]]) -> dict[str, Any] | None:
    for item in items:
        spec = item.get("spec", {})
        if isinstance(spec, dict) and spec.get("defaultStorage") is True:
            return item
    return items[0] if items else None


def phase(name: str, source: str, state: dict[str, Any]) -> dict[str, Any]:
    return {"name": name, "source": source, **state}


def status_condition(status: Any, condition_type: str) -> dict[str, Any]:
    if not isinstance(status, dict):
        return skipped("StatusUnavailable")
    conditions = status.get("conditions")
    if not isinstance(conditions, list):
        return skipped("ConditionNotObserved")
    for condition in conditions:
        if not isinstance(condition, dict) or condition.get("type") != condition_type:
            continue
        return {
            "status": condition.get("status", "Unknown"),
            "reason": condition.get("reason", ""),
            "message": condition.get("message", ""),
        }
    return skipped("ConditionNotObserved")


def replica_phase(status: Any) -> dict[str, Any]:
    if not isinstance(status, dict):
        return skipped("StatusUnavailable")
    replicas = status.get("replicas")
    if not isinstance(replicas, dict):
        return skipped("ReplicasNotObserved")
    components = {}
    ready = True
    observed = False
    for component in ("api", "ui", "worker"):
        component_status = replicas.get(component)
        if not isinstance(component_status, dict):
            components[component] = {
                "status": "Skipped",
                "reason": "ComponentNotEnabled",
            }
            continue
        desired = int(component_status.get("desired") or 0)
        available = int(component_status.get("available") or 0)
        if desired == 0:
            components[component] = {
                "status": "Skipped",
                "reason": "ComponentNotEnabled",
            }
            continue
        observed = True
        component_ready = available >= desired
        ready = ready and component_ready
        components[component] = {
            "status": "True" if component_ready else "False",
            "available": available,
            "desired": desired,
        }
    return {
        "status": "True"
        if ready and observed
        else "Skipped"
        if not observed
        else "False",
        "reason": "WorkloadsAvailable"
        if ready and observed
        else "WorkloadsUnavailable",
        "components": components,
    }


def skipped(reason: str) -> dict[str, Any]:
    return {"status": "Skipped", "reason": reason, "message": ""}


def redact_text(content: str) -> str:
    redacted = re.sub(
        r"(\bauthorization\b\s*[:=]\s*)(bearer|basic)\s+\S+",
        rf"\1\2 {REDACTED}",
        content,
        flags=re.IGNORECASE,
    )
    for name in SENSITIVE_ENV_NAMES - {"AUTHORIZATION"}:
        redacted = re.sub(
            rf"(\b{name}\b\s*[:=]\s*)(\"[^\"]*\"|'[^']*'|\S+)",
            rf"\1{REDACTED}",
            redacted,
            flags=re.IGNORECASE,
        )
    return re.sub(
        r"((?:postgres(?:ql)?|https?)://)[^:@/\s]+:[^@/\s]+@",
        rf"\1{REDACTED}@",
        redacted,
        flags=re.IGNORECASE,
    )


def redact_value(value: Any) -> None:
    if isinstance(value, dict):
        redact_metadata(value)
        if value.get("kind") == "Secret":
            redact_mapping(value.get("data"))
            redact_mapping(value.get("stringData"))
        if value.get("kind") == "ConfigMap":
            name = value.get("metadata", {}).get("name", "")
            redact_config_map(value, name)
        redact_sensitive_fields(value)
        redact_pod_template(value)
        redact_webhook_credentials(value)
        for child in value.values():
            redact_value(child)
    elif isinstance(value, list):
        for index, child in enumerate(value):
            if isinstance(child, str):
                value[index] = redact_text(child)
            else:
                redact_value(child)


def redact_metadata(value: dict[str, Any]) -> None:
    metadata = value.get("metadata")
    if not isinstance(metadata, dict):
        return
    annotations = metadata.get("annotations")
    if isinstance(annotations, dict) and LAST_APPLIED in annotations:
        annotations[LAST_APPLIED] = REDACTED


def redact_config_map(value: dict[str, Any], name: str) -> None:
    sensitive_name = "oauth2-credentials" in name
    for field in ("data", "binaryData"):
        mapping = value.get(field)
        if not isinstance(mapping, dict):
            continue
        for key in list(mapping.keys()):
            if sensitive_name or is_sensitive_name(key):
                mapping[key] = REDACTED


def redact_pod_template(value: dict[str, Any]) -> None:
    spec = None
    if value.get("kind") == "Pod":
        spec = value.get("spec")
    elif isinstance(value.get("spec"), dict):
        spec = value["spec"].get("template", {}).get("spec")
    if not isinstance(spec, dict):
        return
    for container_field in ("initContainers", "containers"):
        for container in spec.get(container_field, []) or []:
            for env in container.get("env", []) or []:
                if (
                    isinstance(env, dict)
                    and is_sensitive_name(env.get("name", ""))
                    and "value" in env
                ):
                    env["value"] = REDACTED


def redact_webhook_credentials(value: dict[str, Any]) -> None:
    for key in ("api_key_value", "api-key-value"):
        if key in value:
            value[key] = REDACTED


def redact_sensitive_fields(value: dict[str, Any]) -> None:
    for key, child in list(value.items()):
        if isinstance(child, str):
            if is_sensitive_document_key(key):
                value[key] = REDACTED
            else:
                value[key] = redact_text(child)


def redact_mapping(mapping: Any) -> None:
    if not isinstance(mapping, dict):
        return
    for key in list(mapping.keys()):
        mapping[key] = REDACTED


def is_sensitive_name(name: str) -> bool:
    return name in SENSITIVE_ENV_NAMES or bool(SENSITIVE_KEY.search(name))


def is_sensitive_document_key(name: str) -> bool:
    normalized = re.sub(r"[^a-z0-9]+", "", name.lower())
    return (
        name.lower() in SENSITIVE_DOCUMENT_KEYS or normalized in SENSITIVE_DOCUMENT_KEYS
    )


def safe_filename(value: str) -> str:
    return re.sub(r"[^A-Za-z0-9_.-]+", "_", value).strip("_") or "output"


if __name__ == "__main__":
    sys.exit(main())
