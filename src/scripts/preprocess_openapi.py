#!/usr/bin/env python3
"""Prepare the BBC TAMS OpenAPI document for the TAMOSS public contract."""

from __future__ import annotations

import argparse
import sys
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

from tamoss.openapi_extensions import apply_tamoss_extensions

PUBLIC_CONTRACT_TEXT_REPLACEMENTS = {
    "comma-seperated": "comma-separated",
    "representes": "represents",
    "content is were uploaded": "content was uploaded",
    "absolutely necesary": "absolutely necessary",
    (
        "List deletion requests currently being worked on, "
        "for monitoring in development."
    ): ("List ongoing flow deletion requests."),
    "description: Webhook identifier": (
        "description: A Universally Unique Identifier (UUID) as defined in RFC9562"
    ),
}


def preprocess_openapi(input_path: Path, output_path: Path) -> None:
    print(f"Preprocessing OpenAPI spec: {input_path}")
    print(f"Output will be written to: {output_path}\n")

    spec = yaml.safe_load(input_path.read_text(encoding="utf-8"))
    if not isinstance(spec, dict):
        raise ValueError(f"OpenAPI document must be a mapping: {input_path}")

    remove_url_token_auth(spec)
    apply_tamoss_extensions(spec)

    raw = yaml.dump(
        spec,
        default_flow_style=False,
        sort_keys=False,
        allow_unicode=True,
    )
    raw = rewrite_external_refs_for_public_contract(raw)
    raw = apply_public_contract_text_corrections(raw)
    output_path.write_text(raw, encoding="utf-8")
    print("Preprocessing complete\n")


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


def rewrite_external_refs_for_public_contract(raw: str) -> str:
    replacements = {
        "$ref: examples/": "$ref: vendor/bbc-tams/api/examples/",
        "$ref: 'examples/": "$ref: 'vendor/bbc-tams/api/examples/",
        '$ref: "examples/': '$ref: "vendor/bbc-tams/api/examples/',
        "$ref: schemas/": "$ref: vendor/bbc-tams/api/schemas/",
        "$ref: 'schemas/": "$ref: 'vendor/bbc-tams/api/schemas/",
        '$ref: "schemas/': '$ref: "vendor/bbc-tams/api/schemas/',
        "externalValue: examples/": "externalValue: vendor/bbc-tams/api/examples/",
        "externalValue: 'examples/": "externalValue: 'vendor/bbc-tams/api/examples/",
        'externalValue: "examples/': 'externalValue: "vendor/bbc-tams/api/examples/',
    }
    for old, new in replacements.items():
        raw = raw.replace(old, new)
    return raw


def apply_public_contract_text_corrections(raw: str) -> str:
    for old, new in PUBLIC_CONTRACT_TEXT_REPLACEMENTS.items():
        raw = raw.replace(old, new)

    raw = raw.replace(
        "        id:\n"
        "          description: ID of the Flow to which the deletion request relates\n",
        "        id:\n          description: ID of the deletion request\n",
    )
    return raw


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
