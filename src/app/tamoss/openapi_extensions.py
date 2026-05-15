from __future__ import annotations

from typing import Any


def apply_tamoss_extensions(spec: dict[str, Any]) -> None:
    add_flow_list_timerange_extension(spec)


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
            "TAMOSS extension. Include each listed Flow's computed content "
            "timerange in the response."
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
