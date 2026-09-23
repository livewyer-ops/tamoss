"""Observe local HTTP responses independently of the application models."""

import json
import re
import subprocess
from collections import defaultdict
from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from starlette.routing import compile_path

from tests.support.bbc_contract import bbc_spec, response_contract, response_validator
from tests.support.paths import BBC_API_SPEC_PATH, REPO_ROOT

_observed: dict[tuple[str, str], set[int]] = defaultdict(set)
_validated: dict[tuple[str, str], set[int]] = defaultdict(set)
_paths = [
    (
        path,
        compile_path(
            re.sub(
                r"\{([^}]+)\}",
                lambda match: (
                    "{"
                    + re.sub(r"\W", "_", match[1])
                    + (":path" if match[1] == "objectId" else "")
                    + "}"
                ),
                path,
            )
        )[0],
    )
    for path in bbc_spec()["paths"]
]


@pytest.fixture(autouse=True)
def validate_bbc_http_responses(
    request: pytest.FixtureRequest, monkeypatch: pytest.MonkeyPatch
) -> None:
    for name in ("client", "real_storage_client"):
        if name not in request.fixturenames:
            continue
        client = request.getfixturevalue(name)
        if not isinstance(client, TestClient):
            continue
        monkeypatch.setitem(
            client.event_hooks,
            "response",
            [*client.event_hooks["response"], _check_response],
        )


def _check_response(response) -> None:
    method = response.request.method.lower()
    for path, pattern in _paths:
        if method not in bbc_spec()["paths"][path] or not pattern.fullmatch(
            response.request.url.path
        ):
            continue
        key = (method, path)
        _observed[key].add(response.status_code)
        # Error cases have operation-specific expectations in their tests.
        # Absence of an error schema does not prohibit TAMOSS error details.
        if response.is_success:
            contract = response_contract(path, method, response.status_code)
            content = contract.get("content", {})
            response.read()
            if method == "head" or response.status_code == 204:
                assert response.content == b""
            elif "application/json" in content:
                assert (
                    response.headers["content-type"].split(";")[0] == "application/json"
                )
                response_validator(path, method, response.status_code).validate(
                    response.json()
                )
                _validated[key].add(response.status_code)
        break


def pytest_sessionfinish(session: pytest.Session) -> None:
    operations = []
    for path, item in bbc_spec()["paths"].items():
        for method in ("get", "head", "post", "put", "delete"):
            if method not in item:
                continue
            documented = {int(status) for status in item[method]["responses"]}
            observed = _observed[(method, path)]
            operations.append(
                {
                    "operation": f"{method.upper()} {path}",
                    "documented_statuses": sorted(documented),
                    "observed_statuses": sorted(observed),
                    "schema_validated_statuses": sorted(_validated[(method, path)]),
                    "unobserved_statuses": sorted(documented - observed),
                }
            )
    report = {
        "bbc_revision": _revision(BBC_API_SPEC_PATH.parent),
        "tamoss_revision": _revision(REPO_ROOT),
        "tamoss_worktree_dirty": subprocess.run(
            ["git", "-C", str(REPO_ROOT), "diff", "--quiet", "HEAD"], check=False
        ).returncode
        != 0,
        "exit_status": int(session.exitstatus),
        "limits": (
            "Observed branches are not proof of complete conformance. "
            "Mandatory prose is checked by named semantic tests. "
            "Unobserved branches remain unverified."
        ),
        "operations": operations,
    }
    junit = session.config.getoption("xmlpath")
    path = (
        Path(junit).with_suffix(".bbc.json")
        if junit
        else REPO_ROOT / "reports/tams-runtime-contract.json"
    )
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(report, indent=2) + "\n")


def _revision(path: Path) -> str:
    return subprocess.check_output(
        ["git", "-C", str(path), "rev-parse", "HEAD"], text=True
    ).strip()
