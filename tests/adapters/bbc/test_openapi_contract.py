from __future__ import annotations

import importlib.util
import sys
from types import ModuleType

import pytest
from fastapi import FastAPI

from tests.adapters.bbc.support import BBC_API_SPEC_PATH, REPO_ROOT

pytestmark = pytest.mark.bbc


def test_runtime_openapi_has_bbc_operation_parity(tamoss_app: FastAPI) -> None:
    """bbc-id: semantic.spec.version_alignment"""
    parity = _load_openapi_parity_module()
    report = parity.compare_specs(
        parity.load_yaml(BBC_API_SPEC_PATH),
        tamoss_app.openapi(),
    )

    assert report.ok, "\n".join(
        f"{item.kind}: {item.detail}" for item in report.findings
    )


def test_runtime_openapi_keeps_bbc_delete_request_path_shape(
    tamoss_app: FastAPI,
) -> None:
    """bbc-id: semantic.runtime.flow_delete_requests_routed"""
    schema = tamoss_app.openapi()

    assert "/flow-delete-requests/{request-id}" in schema["paths"]
    assert "/flow-delete-requests/{request_id}" not in schema["paths"]
    parameters = schema["paths"]["/flow-delete-requests/{request-id}"]["get"][
        "parameters"
    ]
    assert any(
        parameter["in"] == "path" and parameter["name"] == "request-id"
        for parameter in parameters
    )


def test_runtime_openapi_uses_bbc_error_response_codes(tamoss_app: FastAPI) -> None:
    schema = tamoss_app.openapi()

    for path_item in schema["paths"].values():
        for operation in path_item.values():
            if isinstance(operation, dict):
                assert "422" not in operation.get("responses", {})

    for path, method, status_code in [
        ("/flows/{flowId}", "put", "400"),
        ("/flows/{flowId}/segments", "post", "400"),
        ("/service/webhooks/{webhookId}", "put", "400"),
    ]:
        assert status_code in schema["paths"][path][method]["responses"]


def _load_openapi_parity_module() -> ModuleType:
    module_path = REPO_ROOT / "src/scripts/check_tamoss_openapi_parity.py"
    spec = importlib.util.spec_from_file_location("tamoss_openapi_parity", module_path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"Cannot load {module_path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module
