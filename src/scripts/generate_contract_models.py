#!/usr/bin/env python3
"""Generate TAMOSS Python API contract models from the processed OpenAPI spec."""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path

import yaml
from preprocess_openapi import build_model_contract_spec


def generate_contract_models(
    *,
    processed_openapi: Path,
    contract_openapi: Path,
    models_output: Path,
    schema_root: Path,
    public_openapi_json: Path | None = None,
) -> None:
    spec = yaml.safe_load(processed_openapi.read_text(encoding="utf-8"))
    if not isinstance(spec, dict):
        raise ValueError(f"OpenAPI document must be a mapping: {processed_openapi}")

    build_model_contract_spec(spec, schema_root=schema_root)
    contract_openapi.parent.mkdir(parents=True, exist_ok=True)
    contract_openapi.write_text(
        yaml.dump(
            spec,
            default_flow_style=False,
            sort_keys=False,
            allow_unicode=True,
        ),
        encoding="utf-8",
    )
    if public_openapi_json is not None:
        public_openapi_json.parent.mkdir(parents=True, exist_ok=True)
        public_openapi_json.write_text(
            json.dumps(spec, indent=2, ensure_ascii=True) + "\n",
            encoding="utf-8",
        )

    models_output.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(
        [
            sys.executable,
            "-m",
            "datamodel_code_generator",
            "--input",
            str(contract_openapi),
            "--input-file-type",
            "openapi",
            "--output",
            str(models_output),
            "--output-model-type",
            "pydantic_v2.BaseModel",
            "--target-python-version",
            "3.11",
            "--use-annotated",
            "--field-constraints",
            "--use-union-operator",
            "--disable-timestamp",
            "--formatters",
            "ruff-format",
            "ruff-check",
            "--extra-fields",
            "allow",
        ],
        check=True,
    )


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate TAMOSS Python API contract models."
    )
    parser.add_argument("processed_openapi", type=Path)
    parser.add_argument("contract_openapi", type=Path)
    parser.add_argument("models_output", type=Path)
    parser.add_argument(
        "--schema-root",
        type=Path,
        default=Path("src/vendor/bbc-tams/api/schemas"),
        help="BBC schema directory used by processed OpenAPI schema refs.",
    )
    parser.add_argument(
        "--public-openapi-json",
        type=Path,
        default=None,
        help="Optional packaged public OpenAPI JSON output for runtime serving.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(sys.argv[1:] if argv is None else argv)
    try:
        generate_contract_models(
            processed_openapi=args.processed_openapi,
            contract_openapi=args.contract_openapi,
            models_output=args.models_output,
            schema_root=args.schema_root,
            public_openapi_json=args.public_openapi_json,
        )
    except Exception as exc:
        print(f"ERROR: contract model generation failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
