from __future__ import annotations

import base64
import os
import shlex
import subprocess
import time
import warnings
from collections.abc import Iterable, Iterator
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.parse import urljoin

import pytest
import requests
import urllib3
from playwright.sync_api import Browser, sync_playwright
from requests.auth import HTTPBasicAuth
from urllib3.exceptions import InsecureRequestWarning

_TEST_DESCRIPTIONS = {
    "test_deployed_storage_ingest_and_async_delete": (
        "API storage ingest, segment registration, and async flow deletion"
    ),
    "test_deployed_rejects_duplicate_controlled_object_instance": (
        "duplicate controlled object instance rejection"
    ),
    "test_deployed_webhook_registration_and_event_status": (
        "webhook registration, delivery, and event status"
    ),
    "test_deployed_ui_ingress_authenticates_and_proxies_api": (
        "UI ingress authentication and /api proxying"
    ),
    "test_deployed_oauth2_client_credentials_token_grants_api_access": (
        "OAuth2 client-credentials token with explicit scopes"
    ),
    "test_deployed_oauth2_client_credentials_without_scope_grants_api_access": (
        "OAuth2 client-credentials token without requested scopes"
    ),
    "test_deployed_ui_ingest_uploads_and_registers_media": (
        "UI ingest upload and media registration"
    ),
}


def pytest_addoption(parser: pytest.Parser) -> None:
    parser.addoption(
        "--target-env",
        action="store",
        default=os.getenv("TAMOSS_E2E_TARGET_ENV"),
        help="Path to a deployed TAMOSS target env file.",
    )


def pytest_runtest_protocol(item: pytest.Item, nextitem: pytest.Item | None) -> None:
    _ = nextitem
    terminal = item.config.pluginmanager.get_plugin("terminalreporter")
    message = f"E2E: testing {_test_description(item.name)}"
    if terminal is not None:
        terminal.write_line(f"\n{message}")
    else:
        print(f"\n{message}")
    return None


@pytest.fixture(scope="session")
def e2e_target(pytestconfig: pytest.Config) -> E2ETarget:
    target_env = pytestconfig.getoption("--target-env")
    if not target_env:
        raise pytest.UsageError(
            "Pass --target-env=tests/targets/kind.env or set TAMOSS_E2E_TARGET_ENV."
        )
    target = E2ETarget.from_file(Path(target_env))
    if not target.verify_tls:
        urllib3.disable_warnings(InsecureRequestWarning)
        warnings.filterwarnings("ignore", category=InsecureRequestWarning)
    return target


@pytest.fixture(scope="session")
def e2e_client(e2e_target: E2ETarget) -> E2EClient:
    client = E2EClient(e2e_target)
    client.wait_ready()
    return client


@pytest.fixture(scope="session")
def e2e_browser() -> Iterator[Browser]:
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=_browser_headless())
        try:
            yield browser
        finally:
            browser.close()


@dataclass(frozen=True)
class E2ETarget:
    name: str
    api_url: str
    ui_url: str
    auth_url: str
    namespace: str
    kubeconfig: str | None
    auth_namespace: str
    ui_auth_username: str
    ui_auth_password: str
    ui_expected_statuses: set[int]
    verify_tls: bool
    auth_headers: dict[str, str]
    basic_auth: HTTPBasicAuth | None
    timeout_seconds: float = 10.0

    @classmethod
    def from_file(cls, path: Path) -> E2ETarget:
        if not path.is_file():
            raise pytest.UsageError(f"Target env file does not exist: {path}")
        values = _load_env_file(path)
        api_url = _required(values, "TEST_TAMOSS_API")
        ui_url = _required(values, "TEST_TAMOSS_UI")
        auth_url = values.get("TEST_TAMOSS_AUTH", ui_url)
        token = _load_token(values)
        username = values.get("TEST_BASIC_AUTH_USER")
        password = values.get("TEST_BASIC_AUTH_PASSWORD")
        namespace = values.get("TEST_TAMOSS_NAMESPACE", "tams")
        auth_namespace = values.get("TEST_TAMOSS_AUTH_NAMESPACE", "authentik")
        kubeconfig = values.get("KUBECONFIG") or os.getenv("KUBECONFIG")
        if token:
            auth_headers = {"Authorization": f"Bearer {token}"}
            basic_auth = None
        elif username and password:
            auth_headers = {}
            basic_auth = HTTPBasicAuth(username, password)
        else:
            raise pytest.UsageError(
                "Target must provide TEST_TAMOSS_TOKEN, TEST_TAMOSS_TOKEN_SECRET, "
                "or TEST_BASIC_AUTH_USER/TEST_BASIC_AUTH_PASSWORD."
            )
        return cls(
            name=values.get("TEST_TARGET", path.stem),
            api_url=api_url.rstrip("/"),
            ui_url=ui_url.rstrip("/"),
            auth_url=auth_url.rstrip("/"),
            namespace=namespace,
            kubeconfig=kubeconfig,
            auth_namespace=auth_namespace,
            ui_auth_username=values.get("TEST_TAMOSS_AUTH_USER", "akadmin"),
            ui_auth_password=_load_auth_password(
                values, kubeconfig=kubeconfig, namespace=auth_namespace
            ),
            ui_expected_statuses=_status_set(
                values.get("TEST_TAMOSS_UI_EXPECT_STATUS", "200")
            ),
            verify_tls=not _env_bool(values.get("TEST_INSECURE_SKIP_TLS_VERIFY")),
            auth_headers=auth_headers,
            basic_auth=basic_auth,
            timeout_seconds=float(values.get("TEST_TIMEOUT_SECONDS", "10")),
        )


class E2EClient:
    def __init__(self, target: E2ETarget):
        self.target = target
        self.session = requests.Session()

    def wait_ready(self, *, timeout_seconds: float = 180.0) -> None:
        deadline = time.monotonic() + timeout_seconds
        last_error: str | None = None
        while time.monotonic() < deadline:
            try:
                health = self.request("GET", "/healthz", expected={200}, auth=False)
                ready = self.request("GET", "/readyz", expected={200}, auth=False)
                service = self.request("GET", "/service", expected={200})
                if (
                    health.text
                    and ready.status_code == 200
                    and service.status_code == 200
                ):
                    return
            except Exception as exc:  # noqa: BLE001 - surfaced if readiness times out.
                last_error = str(exc)
            time.sleep(2)
        raise AssertionError(f"{self.target.name} did not become ready: {last_error}")

    def request(
        self,
        method: str,
        path_or_url: str,
        *,
        base: str = "api",
        expected: Iterable[int] | int = 200,
        auth: bool = True,
        headers: dict[str, str] | None = None,
        **kwargs: Any,
    ) -> requests.Response:
        expected_statuses = {expected} if isinstance(expected, int) else set(expected)
        merged_headers: dict[str, str] = {}
        if auth:
            merged_headers.update(self.target.auth_headers)
        if headers:
            merged_headers.update(headers)
        response = self.session.request(
            method,
            self._url(path_or_url, base=base),
            auth=self.target.basic_auth if auth else None,
            headers=merged_headers,
            timeout=self.target.timeout_seconds,
            verify=self.target.verify_tls,
            **kwargs,
        )
        if response.status_code not in expected_statuses:
            raise AssertionError(
                f"{method} {response.url} returned {response.status_code}; "
                f"expected {sorted(expected_statuses)}. Body: {response.text[:1000]}"
            )
        return response

    def request_json(
        self,
        method: str,
        path_or_url: str,
        *,
        base: str = "api",
        expected: Iterable[int] | int = 200,
        **kwargs: Any,
    ) -> Any:
        response = self.request(
            method, path_or_url, base=base, expected=expected, **kwargs
        )
        return response.json()

    def upload_put_url(
        self, put_url: str, *, body: bytes, headers: dict[str, str] | None = None
    ) -> requests.Response:
        upload_headers = dict(headers or {})
        auth_headers = (
            self.target.auth_headers if put_url.startswith(self.target.api_url) else {}
        )
        response = self.session.put(
            put_url,
            data=body,
            headers={**auth_headers, **upload_headers},
            auth=self.target.basic_auth
            if put_url.startswith(self.target.api_url)
            else None,
            timeout=self.target.timeout_seconds,
            verify=self.target.verify_tls,
        )
        if response.status_code not in {200, 201, 204}:
            raise AssertionError(
                f"PUT {put_url} returned {response.status_code}; "
                f"expected upload success. Body: {response.text[:1000]}"
            )
        return response

    def poll_delete_request(
        self, request_id: str, *, timeout_seconds: float = 180.0
    ) -> dict[str, Any]:
        deadline = time.monotonic() + timeout_seconds
        last_payload: dict[str, Any] | None = None
        while time.monotonic() < deadline:
            payload = self.request_json("GET", f"/flow-delete-requests/{request_id}")
            last_payload = payload
            if payload["status"] == "done":
                return payload
            if payload["status"] == "error":
                raise AssertionError(f"Delete request failed: {payload}")
            time.sleep(2)
        raise AssertionError(f"Delete request did not complete: {last_payload}")

    def _url(self, path_or_url: str, *, base: str) -> str:
        if path_or_url.startswith(("http://", "https://")):
            return path_or_url
        base_url = self.target.ui_url if base == "ui" else self.target.api_url
        return urljoin(f"{base_url}/", path_or_url.lstrip("/"))


def _test_description(test_name: str) -> str:
    return _TEST_DESCRIPTIONS.get(
        test_name,
        test_name.removeprefix("test_").replace("_", " "),
    )


def _load_env_file(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("export "):
            line = line.removeprefix("export ").strip()
        if "=" not in line:
            continue
        key, raw_value = line.split("=", 1)
        parts = shlex.split(raw_value, comments=True, posix=True)
        values[key.strip()] = parts[0] if parts else ""
    return values


def _load_token(values: dict[str, str]) -> str | None:
    if values.get("TEST_TAMOSS_TOKEN"):
        return values["TEST_TAMOSS_TOKEN"]
    secret_name = values.get("TEST_TAMOSS_TOKEN_SECRET")
    if not secret_name:
        return None
    namespace = values.get("TEST_TAMOSS_NAMESPACE", "tams")
    kubeconfig = values.get("KUBECONFIG") or os.getenv("KUBECONFIG")
    return _load_secret_value(
        kubeconfig=kubeconfig,
        namespace=namespace,
        secret_name=secret_name,
        key="TAMOSS_API_TOKEN",
    )


def _load_auth_password(
    values: dict[str, str], *, kubeconfig: str | None, namespace: str
) -> str:
    if values.get("TEST_TAMOSS_AUTH_PASSWORD"):
        return values["TEST_TAMOSS_AUTH_PASSWORD"]
    secret_name = values.get("TEST_TAMOSS_AUTH_PASSWORD_SECRET")
    if not secret_name:
        raise pytest.UsageError(
            "Target must provide TEST_TAMOSS_AUTH_PASSWORD or "
            "TEST_TAMOSS_AUTH_PASSWORD_SECRET for browser UI checks."
        )
    return _load_secret_value(
        kubeconfig=kubeconfig,
        namespace=namespace,
        secret_name=secret_name,
        key=values.get("TEST_TAMOSS_AUTH_PASSWORD_KEY", "AUTHENTIK_BOOTSTRAP_PASSWORD"),
    )


def _load_secret_value(
    *,
    kubeconfig: str | None,
    namespace: str,
    secret_name: str,
    key: str,
) -> str:
    command = ["kubectl"]
    if kubeconfig:
        command.extend(["--kubeconfig", kubeconfig])
    command.extend(
        [
            "-n",
            namespace,
            "get",
            "secret",
            secret_name,
            "-o",
            f"jsonpath={{.data.{key}}}",
        ]
    )
    try:
        encoded = subprocess.check_output(command, stderr=subprocess.STDOUT)
    except (OSError, subprocess.CalledProcessError) as exc:
        output = getattr(exc, "output", b"")
        detail = output.decode(errors="replace").strip()
        raise pytest.UsageError(
            f"Unable to load {key} from secret {namespace}/{secret_name}: "
            f"{detail or exc}"
        ) from exc
    value = base64.b64decode(encoded.strip(), validate=True).decode().strip()
    if not value:
        raise pytest.UsageError(
            f"Secret {namespace}/{secret_name} did not contain {key}."
        )
    return value


def _required(values: dict[str, str], key: str) -> str:
    value = values.get(key)
    if not value:
        raise pytest.UsageError(f"Target env must set {key}.")
    return value


def _env_bool(value: str | None) -> bool:
    return value is not None and value.lower() in {"1", "true", "yes", "on"}


def _status_set(value: str) -> set[int]:
    statuses: set[int] = set()
    for item in value.split(","):
        item = item.strip()
        if item:
            statuses.add(int(item))
    if not statuses:
        raise pytest.UsageError(
            "TEST_TAMOSS_UI_EXPECT_STATUS must contain a status code."
        )
    return statuses


def _browser_headless() -> bool:
    return os.getenv("TAMOSS_E2E_HEADED", "").lower() not in {"1", "true", "yes"}
