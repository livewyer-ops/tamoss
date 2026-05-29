#!/usr/bin/env python3
"""Compare the TAMOSS FastAPI OpenAPI surface with the BBC TAMS contract."""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections import Counter
from collections.abc import Mapping
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any

import yaml

HTTP_METHODS = frozenset(
    {"get", "put", "post", "delete", "options", "head", "patch", "trace"}
)
REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_BBC_SPEC = REPO_ROOT / "src/vendor/bbc-tams/api/TimeAddressableMediaStore.yaml"


@dataclass(frozen=True)
class OperationRef:
    method: str
    path: str
    normalized_path: str
    path_item: Mapping[str, Any]
    operation: Mapping[str, Any]

    @property
    def key(self) -> tuple[str, str]:
        return self.method, self.normalized_path


@dataclass(frozen=True)
class Finding:
    kind: str
    method: str | None
    path: str
    detail: str
    candidate_path: str | None = None


@dataclass(frozen=True)
class ParityReport:
    source_operations: int
    candidate_operations: int
    matched_operations: int
    findings: list[Finding]

    @property
    def ok(self) -> bool:
        return not self.findings


def load_yaml(path: Path) -> dict[str, Any]:
    return yaml.safe_load(path.read_text(encoding="utf-8"))


def load_candidate_openapi(path: Path | None) -> dict[str, Any]:
    if path is not None:
        return load_yaml(path)

    from tamoss.app import create_app
    from tamoss.application.use_cases import TamossUseCases
    from tamoss.settings import Settings, StorageBackendSettings

    settings = Settings(
        auth_required=False,
        storage_backend=StorageBackendSettings(
            label="tamoss.storage.primary",
            provider="tamoss",
            region="us-east-1",
            store_product="s3",
            bucket_name="tamoss-openapi",
            endpoint_url="https://objects.internal.example.test",
            public_endpoint_url="https://objects.example.test",
            access_key="access",
            secret_key="secret",
        ),
    )
    return create_app(
        settings,
        use_cases=TamossUseCases(
            repository=_OpenApiOnlyRepository(),
            object_storage=_OpenApiOnlyObjectStorage(),
            settings=settings,
        ),
    ).openapi()


class _OpenApiOnlyRepository:
    """Repository adapter used only while building the runtime OpenAPI schema."""

    @property
    def service_repository(self) -> _OpenApiOnlyRepository:
        return self

    @property
    def webhook_repository(self) -> _OpenApiOnlyRepository:
        return self

    @property
    def deletion_repository(self) -> _OpenApiOnlyRepository:
        return self

    @property
    def source_repository(self) -> _OpenApiOnlyRepository:
        return self

    @property
    def flow_repository(self) -> _OpenApiOnlyRepository:
        return self

    @property
    def storage_repository(self) -> _OpenApiOnlyRepository:
        return self

    @property
    def segment_repository(self) -> _OpenApiOnlyRepository:
        return self

    @property
    def object_repository(self) -> _OpenApiOnlyRepository:
        return self


class _OpenApiOnlyObjectStorage:
    """Object-storage adapter used only while building the runtime OpenAPI schema."""

    pass


def normalized_path(path: str) -> str:
    return re.sub(r"\{[^}]+\}", "{}", path)


def operation_map(spec: Mapping[str, Any]) -> dict[tuple[str, str], OperationRef]:
    operations: dict[tuple[str, str], OperationRef] = {}
    for path, path_item in sorted(spec.get("paths", {}).items()):
        if not isinstance(path_item, Mapping):
            continue
        for method, operation in sorted(path_item.items()):
            if method.lower() not in HTTP_METHODS:
                continue
            if not isinstance(operation, Mapping):
                continue
            operation_ref = OperationRef(
                method=method.upper(),
                path=path,
                normalized_path=normalized_path(path),
                path_item=path_item,
                operation=operation,
            )
            operations[operation_ref.key] = operation_ref
    return operations


def compare_specs(
    source_spec: Mapping[str, Any],
    candidate_spec: Mapping[str, Any],
) -> ParityReport:
    source_operations = operation_map(source_spec)
    candidate_operations = operation_map(candidate_spec)
    findings: list[Finding] = []

    source_keys = set(source_operations)
    candidate_keys = set(candidate_operations)

    for key in sorted(source_keys - candidate_keys):
        source = source_operations[key]
        findings.append(
            Finding(
                kind="missing_operation",
                method=source.method,
                path=source.path,
                detail=f"{source.method} {source.path} is in the BBC spec.",
            )
        )

    for key in sorted(candidate_keys - source_keys):
        candidate = candidate_operations[key]
        findings.append(
            Finding(
                kind="extra_operation",
                method=candidate.method,
                path=candidate.path,
                detail=f"{candidate.method} {candidate.path} is not in the BBC spec.",
            )
        )

    for key in sorted(source_keys & candidate_keys):
        source = source_operations[key]
        candidate = candidate_operations[key]
        findings.extend(
            compare_operation(source_spec, source, candidate_spec, candidate)
        )

    return ParityReport(
        source_operations=len(source_operations),
        candidate_operations=len(candidate_operations),
        matched_operations=len(source_keys & candidate_keys),
        findings=findings,
    )


def compare_operation(
    source_spec: Mapping[str, Any],
    source: OperationRef,
    candidate_spec: Mapping[str, Any],
    candidate: OperationRef,
) -> list[Finding]:
    findings: list[Finding] = []
    if source.path != candidate.path:
        findings.append(
            Finding(
                kind="path_template_mismatch",
                method=source.method,
                path=source.path,
                candidate_path=candidate.path,
                detail=(
                    "Path templates match structurally but not by BBC parameter "
                    f"name: candidate exposes {candidate.path}."
                ),
            )
        )

    findings.extend(compare_parameters(source_spec, source, candidate_spec, candidate))
    findings.extend(compare_request_body(source, candidate))
    findings.extend(compare_responses(source, candidate))
    return findings


def compare_parameters(
    source_spec: Mapping[str, Any],
    source: OperationRef,
    candidate_spec: Mapping[str, Any],
    candidate: OperationRef,
) -> list[Finding]:
    source_parameters = parameters_by_key(source_spec, source)
    candidate_parameters = parameters_by_key(candidate_spec, candidate)
    source_keys = set(source_parameters)
    candidate_keys = set(candidate_parameters)
    findings: list[Finding] = []

    for location, name in sorted(source_keys - candidate_keys):
        findings.append(
            Finding(
                kind="missing_parameter",
                method=source.method,
                path=source.path,
                candidate_path=candidate.path,
                detail=f"Missing {location} parameter {name!r}.",
            )
        )

    for location, name in sorted(candidate_keys - source_keys):
        candidate_parameter = candidate_parameters[(location, name)]
        if is_tamoss_extension(candidate_parameter):
            continue
        findings.append(
            Finding(
                kind="extra_parameter",
                method=source.method,
                path=source.path,
                candidate_path=candidate.path,
                detail=f"Extra {location} parameter {name!r}.",
            )
        )

    for key in sorted(source_keys & candidate_keys):
        source_parameter = source_parameters[key]
        candidate_parameter = candidate_parameters[key]
        source_required = bool(source_parameter.get("required", False))
        candidate_required = bool(candidate_parameter.get("required", False))
        if source_required != candidate_required:
            location, name = key
            findings.append(
                Finding(
                    kind="parameter_required_mismatch",
                    method=source.method,
                    path=source.path,
                    candidate_path=candidate.path,
                    detail=(
                        f"{location} parameter {name!r} required={candidate_required} "
                        f"but BBC requires {source_required}."
                    ),
                )
            )

    return findings


def is_tamoss_extension(value: Mapping[str, Any]) -> bool:
    schema = value.get("schema")
    return value.get("x-tamoss-extension") is True or (
        isinstance(schema, Mapping) and schema.get("x-tamoss-extension") is True
    )


def compare_request_body(
    source: OperationRef, candidate: OperationRef
) -> list[Finding]:
    source_content = content_types(source.operation.get("requestBody"))
    candidate_content = content_types(candidate.operation.get("requestBody"))
    findings: list[Finding] = []

    for content_type in sorted(source_content - candidate_content):
        findings.append(
            Finding(
                kind="missing_request_content_type",
                method=source.method,
                path=source.path,
                candidate_path=candidate.path,
                detail=f"Missing request content type {content_type!r}.",
            )
        )

    for content_type in sorted(candidate_content - source_content):
        findings.append(
            Finding(
                kind="extra_request_content_type",
                method=source.method,
                path=source.path,
                candidate_path=candidate.path,
                detail=f"Extra request content type {content_type!r}.",
            )
        )

    return findings


def compare_responses(source: OperationRef, candidate: OperationRef) -> list[Finding]:
    source_responses = source.operation.get("responses", {})
    candidate_responses = candidate.operation.get("responses", {})
    source_statuses = set(source_responses)
    candidate_statuses = set(candidate_responses)
    findings: list[Finding] = []

    for status_code in sorted(source_statuses - candidate_statuses):
        findings.append(
            Finding(
                kind="missing_response",
                method=source.method,
                path=source.path,
                candidate_path=candidate.path,
                detail=f"Missing response status {status_code}.",
            )
        )

    for status_code in sorted(candidate_statuses - source_statuses):
        findings.append(
            Finding(
                kind="extra_response",
                method=source.method,
                path=source.path,
                candidate_path=candidate.path,
                detail=f"Extra response status {status_code}.",
            )
        )

    for status_code in sorted(source_statuses & candidate_statuses):
        source_content = content_types(source_responses.get(status_code))
        candidate_content = content_types(candidate_responses.get(status_code))
        for content_type in sorted(source_content - candidate_content):
            findings.append(
                Finding(
                    kind="missing_response_content_type",
                    method=source.method,
                    path=source.path,
                    candidate_path=candidate.path,
                    detail=(
                        f"Response {status_code} is missing content type "
                        f"{content_type!r}."
                    ),
                )
            )
        for content_type in sorted(candidate_content - source_content):
            findings.append(
                Finding(
                    kind="extra_response_content_type",
                    method=source.method,
                    path=source.path,
                    candidate_path=candidate.path,
                    detail=(
                        f"Response {status_code} has extra content type "
                        f"{content_type!r}."
                    ),
                )
            )

    return findings


def parameters_by_key(
    spec: Mapping[str, Any],
    operation_ref: OperationRef,
) -> dict[tuple[str, str], Mapping[str, Any]]:
    parameters: dict[tuple[str, str], Mapping[str, Any]] = {}
    raw_parameters = [
        *operation_ref.path_item.get("parameters", []),
        *operation_ref.operation.get("parameters", []),
    ]
    for raw_parameter in raw_parameters:
        parameter = resolve_ref(spec, raw_parameter)
        location = parameter.get("in")
        name = parameter.get("name")
        if isinstance(location, str) and isinstance(name, str):
            parameters[(location, name)] = parameter
    return parameters


def resolve_ref(spec: Mapping[str, Any], value: Any) -> Mapping[str, Any]:
    if not isinstance(value, Mapping) or "$ref" not in value:
        return value if isinstance(value, Mapping) else {}
    ref = value["$ref"]
    if not isinstance(ref, str) or not ref.startswith("#/"):
        return value

    resolved: Any = spec
    for part in ref[2:].split("/"):
        part = part.replace("~1", "/").replace("~0", "~")
        if not isinstance(resolved, Mapping):
            return value
        resolved = resolved.get(part)
    return resolved if isinstance(resolved, Mapping) else value


def content_types(value: Any) -> set[str]:
    if not isinstance(value, Mapping):
        return set()
    content = value.get("content", {})
    if not isinstance(content, Mapping):
        return set()
    return {str(content_type) for content_type in content}


def render_markdown(report: ParityReport, *, max_findings: int | None = None) -> str:
    counts = Counter(finding.kind for finding in report.findings)
    lines = [
        "# TAMOSS OpenAPI Parity",
        "",
        f"- BBC operations: {report.source_operations}",
        f"- TAMOSS operations: {report.candidate_operations}",
        f"- Matched operations: {report.matched_operations}",
        f"- Findings: {len(report.findings)}",
        "",
        "## Finding Counts",
        "",
        "| Kind | Count |",
        "| --- | ---: |",
    ]
    for kind, count in sorted(counts.items()):
        lines.append(f"| `{kind}` | {count} |")

    findings = report.findings
    if max_findings is not None:
        findings = findings[:max_findings]

    lines.extend(["", "## Findings", ""])
    if not findings:
        lines.append("No OpenAPI surface gaps found.")
    else:
        for finding in findings:
            operation = (
                f"{finding.method} {finding.path}"
                if finding.method is not None
                else finding.path
            )
            suffix = (
                f" Candidate: `{finding.candidate_path}`."
                if finding.candidate_path and finding.candidate_path != finding.path
                else ""
            )
            lines.append(f"- `{finding.kind}` `{operation}`: {finding.detail}{suffix}")

    remaining = len(report.findings) - len(findings)
    if remaining > 0:
        lines.extend(["", f"... {remaining} more findings omitted."])
    return "\n".join(lines) + "\n"


def render_json(report: ParityReport) -> str:
    return json.dumps(
        {
            "source_operations": report.source_operations,
            "candidate_operations": report.candidate_operations,
            "matched_operations": report.matched_operations,
            "ok": report.ok,
            "finding_counts": Counter(finding.kind for finding in report.findings),
            "findings": [asdict(finding) for finding in report.findings],
        },
        indent=2,
        sort_keys=True,
    )


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Compare the BBC TAMS OpenAPI surface with tamoss.app:create_app()."
        )
    )
    parser.add_argument(
        "--source",
        type=Path,
        default=DEFAULT_BBC_SPEC,
        help="BBC OpenAPI YAML to use as the source of truth.",
    )
    parser.add_argument(
        "--candidate",
        type=Path,
        default=None,
        help="Candidate OpenAPI YAML/JSON. Defaults to tamoss.app:create_app().",
    )
    parser.add_argument(
        "--format",
        choices=("markdown", "json"),
        default="markdown",
        help="Output format.",
    )
    parser.add_argument(
        "--max-findings",
        type=int,
        default=None,
        help="Maximum findings to print in markdown output.",
    )
    parser.add_argument(
        "--allow-gaps",
        action="store_true",
        help="Exit zero even when parity gaps are found.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(sys.argv[1:] if argv is None else argv)
    report = compare_specs(
        load_yaml(args.source), load_candidate_openapi(args.candidate)
    )
    output = (
        render_json(report)
        if args.format == "json"
        else render_markdown(report, max_findings=args.max_findings)
    )
    print(output, end="" if output.endswith("\n") else "\n")
    if report.ok or args.allow_gaps:
        return 0
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
