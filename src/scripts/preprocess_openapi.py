#!/usr/bin/env python3
"""Prepare the BBC TAMS OpenAPI document for the TAMOSS public contract."""

from __future__ import annotations

import argparse
import importlib.util
import json
import re
import sys
from collections.abc import Iterator
from pathlib import Path
from typing import Any

try:
    import yaml
except ImportError:
    print(
        "ERROR: PyYAML is required. Install with: uv add --dev pyyaml",
        file=sys.stderr,
    )
    sys.exit(1)

try:
    from openapi_extensions import apply_tamoss_contract_extensions
except ModuleNotFoundError:
    extension_path = Path(__file__).with_name("openapi_extensions.py")
    spec = importlib.util.spec_from_file_location("openapi_extensions", extension_path)
    if spec is None or spec.loader is None:
        raise
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    apply_tamoss_contract_extensions = module.apply_tamoss_contract_extensions

BBC_SCHEMA_REF_PREFIX = "vendor/bbc-tams/api/schemas/"
PUBLIC_CONTRACT_TEXT_REPLACEMENTS = {
    "comma-seperated": "comma-separated",
    "representes": "represents",
    "content is were uploaded": "content was uploaded",
    "absolutely necesary": "absolutely necessary",
    (
        "List deletion requests currently being worked on, "
        "for monitoring in development."
    ): ("List ongoing flow deletion requests."),
}
DESCRIPTION_REPLACEMENTS = {
    "Webhook identifier": (
        "A Universally Unique Identifier (UUID) as defined in RFC9562"
    ),
    "ID of the Flow to which the deletion request relates": (
        "ID of the deletion request"
    ),
}


def preprocess_openapi(input_path: Path, output_path: Path) -> None:
    print(f"Preprocessing OpenAPI spec: {input_path}")
    print(f"Output will be written to: {output_path}\n")

    spec = yaml.safe_load(input_path.read_text(encoding="utf-8"))
    if not isinstance(spec, dict):
        raise ValueError(f"OpenAPI document must be a mapping: {input_path}")

    remove_url_token_auth(spec)
    apply_tamoss_contract_extensions(spec)
    rewrite_external_refs_for_public_contract(spec)
    apply_public_contract_text_corrections(spec)

    raw = yaml.dump(
        spec,
        default_flow_style=False,
        sort_keys=False,
        allow_unicode=True,
    )
    output_path.write_text(raw, encoding="utf-8")
    print("Preprocessing complete\n")


def build_model_contract_spec(
    processed_spec: dict[str, Any], *, schema_root: Path
) -> dict[str, Any]:
    """Embed external BBC schemas for Pydantic contract generation."""
    embed_external_schema_refs_for_contract(processed_spec, schema_root=schema_root)
    return processed_spec


def remove_url_token_auth(spec: dict[str, Any]) -> None:
    security = spec.get("security")
    if isinstance(security, list):
        spec["security"] = [
            item
            for item in security
            if not (isinstance(item, dict) and "url_token_auth" in item)
        ]

    components = spec.get("components")
    if isinstance(components, dict):
        schemes = components.get("securitySchemes")
        if isinstance(schemes, dict):
            schemes.pop("url_token_auth", None)


def rewrite_external_refs_for_public_contract(spec: dict[str, Any]) -> None:
    for parent, key, value in _walk_openapi_values(spec):
        if not isinstance(value, str):
            continue
        if key == "$ref":
            parent[key] = _rewrite_ref_value(value)
        elif key == "externalValue":
            parent[key] = _rewrite_external_value(value)


def embed_external_schema_refs_for_contract(
    spec: dict[str, Any], *, schema_root: Path
) -> None:
    components = spec.setdefault("components", {}).setdefault("schemas", {})
    loading: set[str] = set()

    def rewrite_schema_refs(node: Any) -> None:
        if isinstance(node, dict):
            ref = node.get("$ref")
            if isinstance(ref, str):
                node["$ref"] = load_schema_ref(ref)
            for value in node.values():
                rewrite_schema_refs(value)
            return
        if isinstance(node, list):
            for item in node:
                rewrite_schema_refs(item)

    def load_schema_ref(ref: str) -> str:
        schema_file = _schema_file_from_ref(ref)
        if schema_file is None:
            return ref
        component_name = _schema_component_name(schema_file)
        if component_name not in components and component_name not in loading:
            schema_path = schema_root / schema_file
            if not schema_path.exists():
                raise ValueError(f"Referenced schema file not found: {schema_path}")
            loading.add(component_name)
            schema = json.loads(schema_path.read_text(encoding="utf-8"))
            rewrite_schema_refs(schema)
            components[component_name] = schema
            loading.remove(component_name)
        return f"#/components/schemas/{component_name}"

    rewrite_schema_refs(spec)


def apply_public_contract_text_corrections(spec: dict[str, Any]) -> None:
    for parent, key, value in _walk_openapi_values(spec):
        if not isinstance(value, str):
            continue
        if key == "description" and value in DESCRIPTION_REPLACEMENTS:
            parent[key] = DESCRIPTION_REPLACEMENTS[value]
            continue
        corrected = value
        for old, new in PUBLIC_CONTRACT_TEXT_REPLACEMENTS.items():
            corrected = corrected.replace(old, new)
        parent[key] = corrected


def _rewrite_ref_value(value: str) -> str:
    if value.startswith("examples/"):
        return f"vendor/bbc-tams/api/{value}"
    if value.startswith("schemas/"):
        return f"vendor/bbc-tams/api/{value}"
    return value


def _rewrite_external_value(value: str) -> str:
    if value.startswith("examples/"):
        return f"vendor/bbc-tams/api/{value}"
    return value


def _schema_file_from_ref(ref: str) -> str | None:
    if ref.startswith(BBC_SCHEMA_REF_PREFIX):
        return Path(ref).name
    if re.fullmatch(r"[^/#]+\.json", ref):
        return ref
    return None


def _schema_component_name(schema_file: str) -> str:
    stem = Path(schema_file).stem
    return "".join(
        part.capitalize() for part in re.split(r"[^A-Za-z0-9]+", stem) if part
    )


def _walk_openapi_values(
    node: Any,
) -> Iterator[tuple[dict[str, Any] | list[Any], str | int, Any]]:
    if isinstance(node, dict):
        for key, value in list(node.items()):
            yield node, key, value
            yield from _walk_openapi_values(value)
    elif isinstance(node, list):
        for index, value in enumerate(list(node)):
            yield node, index, value
            yield from _walk_openapi_values(value)


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Prepare the BBC TAMS OpenAPI document for TAMOSS."
    )
    parser.add_argument("input_path", type=Path, help="BBC OpenAPI document")
    parser.add_argument("output_path", type=Path, help="Where to write output YAML")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(sys.argv[1:] if argv is None else argv)
    if not args.input_path.exists():
        print(f"ERROR: Input file not found: {args.input_path}", file=sys.stderr)
        return 1

    try:
        preprocess_openapi(args.input_path, args.output_path)
    except Exception as exc:
        print(f"\nERROR: Preprocessing failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
