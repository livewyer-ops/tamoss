from __future__ import annotations

from types import ModuleType

import pytest
from fastapi import FastAPI

from tests.adapters.bbc.support import BBC_API_SPEC_PATH, REPO_ROOT
from tests.support.paths import load_python_module

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
    parameters = _operation_parameters(
        schema,
        "/flow-delete-requests/{request-id}",
        "get",
    )
    assert any(
        parameter["in"] == "path" and parameter["name"] == "request-id"
        for parameter in parameters
    )


def test_runtime_openapi_hides_tamoss_object_mutation_aliases(
    tamoss_app: FastAPI,
) -> None:
    schema = tamoss_app.openapi()

    assert "post" not in schema["paths"]["/objects/{objectId}"]
    assert "delete" not in schema["paths"]["/objects/{objectId}"]
    assert "post" in schema["paths"]["/objects/{objectId}/instances"]
    assert "delete" in schema["paths"]["/objects/{objectId}/instances"]


def test_runtime_openapi_documents_tag_filter_and_path_shapes(
    tamoss_app: FastAPI,
) -> None:
    schema = tamoss_app.openapi()

    for path in ("/flows", "/sources"):
        get_parameters = {
            parameter["name"]
            for parameter in _operation_parameters(schema, path, "get")
        }
        assert {"tag.{name}", "tag_exists.{name}"} <= get_parameters

    for path in ("/flows/{flowId}/tags/{name}", "/sources/{sourceId}/tags/{name}"):
        get_parameters = [
            parameter
            for parameter in _operation_parameters(schema, path, "get")
            if parameter["in"] == "path"
        ]
        assert {parameter["name"] for parameter in get_parameters} >= {"name"}


def test_runtime_openapi_distinguishes_core_and_compatibility_timerange_parameters(
    tamoss_app: FastAPI,
) -> None:
    schema = tamoss_app.openapi()
    flow_list_parameters = _operation_parameters(schema, "/flows", "get")
    list_include_timerange = next(
        parameter
        for parameter in flow_list_parameters
        if parameter["name"] == "include_timerange"
    )
    flow_detail_parameters = _operation_parameters(schema, "/flows/{flowId}", "get")
    detail_include_timerange = next(
        parameter
        for parameter in flow_detail_parameters
        if parameter["name"] == "include_timerange"
    )

    assert list_include_timerange["x-tamoss-extension"] is True
    assert (
        "Third-party compatibility extension" in list_include_timerange["description"]
    )
    assert "x-tamoss-extension" not in detail_include_timerange


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


def test_runtime_openapi_documents_tamoss_error_payload(tamoss_app: FastAPI) -> None:
    schema = tamoss_app.openapi()

    assert "ErrorPayload" in schema["components"]["schemas"]
    error_response = schema["paths"]["/flows/{flowId}"]["put"]["responses"]["400"]
    assert error_response["x-tamoss-error-payload"] == {
        "$ref": "#/components/schemas/ErrorPayload"
    }


def test_runtime_openapi_distinguishes_tamoss_and_tams_versions(
    tamoss_app: FastAPI,
) -> None:
    schema = tamoss_app.openapi()

    assert schema["info"]["version"] != "0.0.0"
    assert schema["info"]["version"] != "8.1"
    assert schema["info"]["x-tamoss-version"] == schema["info"]["version"]
    assert schema["info"]["x-bbc-tams-api-version"] == "8.1"


def _load_openapi_parity_module() -> ModuleType:
    return load_python_module(
        "tamoss_openapi_parity",
        REPO_ROOT / "src/scripts/check_tamoss_openapi_parity.py",
    )


def _operation_parameters(
    schema: dict,
    path: str,
    method: str,
) -> list[dict]:
    path_item = schema["paths"][path]
    return [
        parameter
        for parameter in [
            *path_item.get("parameters", []),
            *path_item[method].get("parameters", []),
        ]
        if "name" in parameter
    ]
