from __future__ import annotations

from typing import Any

ERROR_RESPONSE_CODES = frozenset({"400", "401", "403", "404", "409", "500", "503"})


def apply_tamoss_contract_extensions(spec: dict[str, Any]) -> None:
    add_error_payload_contract(spec)
    add_tag_filter_parameters(spec)
    add_flow_list_timerange_extension(spec)


def add_error_payload_contract(spec: dict[str, Any]) -> None:
    schemas = spec.setdefault("components", {}).setdefault("schemas", {})
    schemas.setdefault(
        "ErrorPayload",
        {
            "additionalProperties": True,
            "properties": {
                "type": {"type": "string"},
                "summary": {"type": "string"},
                "traceback": {
                    "items": {"type": "string"},
                    "type": "array",
                },
                "time": {"format": "date-time", "type": "string"},
                "incident_id": {"type": "string"},
            },
            "required": ["type", "summary", "time"],
            "type": "object",
        },
    )

    paths = spec.get("paths")
    if not isinstance(paths, dict):
        return
    for path_item in paths.values():
        if not isinstance(path_item, dict):
            continue
        for operation in path_item.values():
            if not isinstance(operation, dict):
                continue
            responses = operation.get("responses")
            if not isinstance(responses, dict):
                continue
            for status_code, response in responses.items():
                if status_code not in ERROR_RESPONSE_CODES or not isinstance(
                    response, dict
                ):
                    continue
                if "$ref" in response:
                    continue
                response.setdefault(
                    "x-tamoss-error-payload",
                    {"$ref": "#/components/schemas/ErrorPayload"},
                )


TAG_FILTER_PARAMETER_PATHS = {
    "/flows": ("tag.{name}", "tag_exists.{name}"),
    "/sources": ("tag.{name}", "tag_exists.{name}"),
    "/service/webhooks": ("tag.{name}", "tag_exists.{name}"),
    "/objects/{objectId}": ("flow_tag.{name}", "flow_tag_exists.{name}"),
}


def add_tag_filter_parameters(spec: dict[str, Any]) -> None:
    paths = spec.get("paths")
    if not isinstance(paths, dict):
        return

    for path, parameter_names in TAG_FILTER_PARAMETER_PATHS.items():
        path_item = paths.get(path)
        if not isinstance(path_item, dict):
            continue
        for method in ("head", "get"):
            operation = path_item.get(method)
            if not isinstance(operation, dict):
                continue
            parameters = operation.get("parameters")
            if not isinstance(parameters, list):
                parameters = []
                operation["parameters"] = parameters
            for parameter_name in parameter_names:
                _upsert_query_parameter(
                    parameters,
                    _tag_filter_parameter(parameter_name),
                )


def _tag_filter_parameter(name: str) -> dict[str, Any]:
    is_existence_filter = name in {"tag_exists.{name}", "flow_tag_exists.{name}"}
    return {
        "name": name,
        "in": "query",
        "required": False,
        "allowReserved": True,
        "description": (
            "BBC TAMS-compatible tag filter placeholder. Replace {name} with "
            "the tag name to filter on."
        ),
        "schema": {"type": "boolean"} if is_existence_filter else {"type": "string"},
    }


def _upsert_query_parameter(parameters: list[Any], replacement: dict[str, Any]) -> None:
    for index, parameter in enumerate(parameters):
        if (
            isinstance(parameter, dict)
            and parameter.get("name") == replacement["name"]
            and parameter.get("in") == "query"
        ):
            parameters[index] = {
                **parameter,
                "allowReserved": True,
                "required": parameter.get("required", False),
            }
            return
    parameters.append(replacement)


def add_flow_list_timerange_extension(spec: dict[str, Any]) -> None:
    paths = spec.get("paths")
    if not isinstance(paths, dict):
        return
    flows_path = paths.get("/flows")
    if not isinstance(flows_path, dict):
        return

    for method in ("head", "get"):
        operation = flows_path.get(method)
        if not isinstance(operation, dict):
            continue
        parameters = operation.get("parameters")
        if not isinstance(parameters, list):
            continue

        extension = _flow_list_timerange_extension()
        for index, parameter in enumerate(parameters):
            if (
                isinstance(parameter, dict)
                and parameter.get("name") == "include_timerange"
                and parameter.get("in") == "query"
            ):
                parameters[index] = extension
                break
        else:
            insert_at = _query_parameter_insert_index(
                parameters, after_name="timerange"
            )
            parameters.insert(insert_at, extension)


def _flow_list_timerange_extension() -> dict[str, Any]:
    return {
        "name": "include_timerange",
        "in": "query",
        "x-tamoss-extension": True,
        "description": (
            "Third-party compatibility extension. Include each "
            "listed Flow's computed content timerange in the response."
        ),
        "schema": {
            "default": False,
            "type": "boolean",
        },
    }


def _query_parameter_insert_index(parameters: list[Any], *, after_name: str) -> int:
    for index, parameter in enumerate(parameters):
        if (
            isinstance(parameter, dict)
            and parameter.get("name") == after_name
            and parameter.get("in") == "query"
        ):
            return index + 1
    return len(parameters)
