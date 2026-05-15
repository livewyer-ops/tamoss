from __future__ import annotations

import ast
from pathlib import Path

ROUTE_DIR = Path(__file__).parents[2] / "src/app/tamoss/api/routes"
OFFLOAD_CALLS = {
    "anyio.to_thread.run_sync",
    "to_thread.run_sync",
    "run_in_threadpool",
}


def test_async_routes_do_not_call_sync_use_cases_directly() -> None:
    violations: list[str] = []

    for route_file in sorted(ROUTE_DIR.glob("*.py")):
        source = route_file.read_text(encoding="utf-8")
        tree = ast.parse(source, filename=str(route_file))
        parents = _parent_map(tree)
        lines = source.splitlines()

        for node in ast.walk(tree):
            if not isinstance(node, ast.AsyncFunctionDef):
                continue
            if not _is_route_handler(node):
                continue

            has_await = any(isinstance(child, ast.Await) for child in ast.walk(node))
            has_marker = _has_async_required_marker(node, lines)
            if not has_await and not has_marker:
                violations.append(
                    f"{route_file.relative_to(ROUTE_DIR.parent.parent.parent)}:"
                    f"{node.lineno} {node.name} is async without await"
                )

            for call in _use_case_calls(node):
                if not _is_inside_offload(call, parents):
                    violations.append(
                        f"{route_file.relative_to(ROUTE_DIR.parent.parent.parent)}:"
                        f"{call.lineno} {node.name} calls use_cases synchronously "
                        "from an async route"
                    )

    assert violations == []


def _is_route_handler(node: ast.AsyncFunctionDef) -> bool:
    return any(_is_router_decorator(decorator) for decorator in node.decorator_list)


def _is_router_decorator(decorator: ast.expr) -> bool:
    call = decorator if isinstance(decorator, ast.Call) else None
    target = call.func if call is not None else decorator
    return (
        isinstance(target, ast.Attribute)
        and isinstance(target.value, ast.Name)
        and target.value.id == "router"
    )


def _has_async_required_marker(node: ast.AsyncFunctionDef, lines: list[str]) -> bool:
    end_lineno = node.end_lineno or node.lineno
    body_lines = lines[node.lineno - 1 : end_lineno]
    return any("# async required:" in line for line in body_lines)


def _parent_map(tree: ast.AST) -> dict[ast.AST, ast.AST]:
    parents: dict[ast.AST, ast.AST] = {}
    for parent in ast.walk(tree):
        for child in ast.iter_child_nodes(parent):
            parents[child] = parent
    return parents


def _use_case_calls(node: ast.AsyncFunctionDef) -> list[ast.Call]:
    return [
        child
        for child in ast.walk(node)
        if isinstance(child, ast.Call) and _is_use_case_method(child.func)
    ]


def _is_use_case_method(node: ast.expr) -> bool:
    return (
        isinstance(node, ast.Attribute)
        and isinstance(node.value, ast.Name)
        and node.value.id == "use_cases"
    )


def _is_inside_offload(
    node: ast.AST,
    parents: dict[ast.AST, ast.AST],
) -> bool:
    current = parents.get(node)
    while current is not None:
        if isinstance(current, ast.Call) and _call_name(current.func) in OFFLOAD_CALLS:
            return True
        current = parents.get(current)
    return False


def _call_name(node: ast.expr) -> str:
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Attribute):
        prefix = _call_name(node.value)
        return f"{prefix}.{node.attr}" if prefix else node.attr
    return ""
