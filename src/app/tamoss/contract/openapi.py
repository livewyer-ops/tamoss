from __future__ import annotations

import json
from copy import deepcopy
from importlib.resources import files
from typing import Any

from tamoss.settings import Settings


def load_public_openapi(settings: Settings) -> dict[str, Any]:
    schema = deepcopy(_load_packaged_openapi())
    info = schema.setdefault("info", {})
    info["version"] = settings.tamoss_version
    info["x-tamoss-version"] = settings.tamoss_version
    info["x-bbc-tams-api-version"] = settings.api_version
    return schema


def _load_packaged_openapi() -> dict[str, Any]:
    payload = (
        files("tamoss.contract").joinpath("openapi.json").read_text(encoding="utf-8")
    )
    schema = json.loads(payload)
    if not isinstance(schema, dict):
        raise ValueError("Packaged OpenAPI contract must be a JSON object.")
    return schema
