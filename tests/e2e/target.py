from __future__ import annotations

import os
import shlex
import warnings
from dataclasses import dataclass
from pathlib import Path

import pytest
import urllib3
from requests.auth import HTTPBasicAuth
from urllib3.exceptions import InsecureRequestWarning

from tests.e2e.kubernetes import load_jsonpath, load_secret_value
from tests.support.paths import REPO_ROOT


def pytest_addoption(parser: pytest.Parser) -> None:
    try:
        parser.addoption(
            "--target-env",
            action="store",
            default=os.getenv("TAMOSS_E2E_TARGET_ENV"),
            help="Path to a deployed TAMOSS target env file.",
        )
    except ValueError as exc:
        if "--target-env" not in str(exc):
            raise


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
    certificate_refs: tuple[str, ...]
    oauth2_enabled: bool
    oauth_issuer_url: str
    oauth_client_id: str
    oauth_client_secret: str | None
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
        cr_name = values.get("TEST_TAMOSS_CR_NAME")
        auth_namespace = values.get("TEST_TAMOSS_AUTH_NAMESPACE", "authentik")
        kubeconfig = values.get("KUBECONFIG") or os.getenv("KUBECONFIG")
        oauth2_enabled = _env_bool(values.get("TEST_TAMOSS_OAUTH2_ENABLED"))
        oauth_secret_namespace = values.get(
            "TEST_TAMOSS_OAUTH_CLIENT_SECRET_NAMESPACE", namespace
        )
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
            certificate_refs=_csv_values(values.get("TEST_TAMOSS_CERTIFICATES", "")),
            oauth2_enabled=oauth2_enabled,
            oauth_issuer_url=_load_oauth_issuer_url(
                values,
                kubeconfig=kubeconfig,
                namespace=namespace,
                cr_name=cr_name,
                auth_url=auth_url.rstrip("/"),
                enabled=oauth2_enabled,
            ),
            oauth_client_id=_load_oauth_client_id(
                values,
                kubeconfig=kubeconfig,
                namespace=oauth_secret_namespace,
                enabled=oauth2_enabled,
            ),
            oauth_client_secret=_load_oauth_client_secret(
                values,
                kubeconfig=kubeconfig,
                namespace=oauth_secret_namespace,
                enabled=oauth2_enabled,
            ),
            timeout_seconds=float(values.get("TEST_TIMEOUT_SECONDS", "10")),
        )


def _load_env_file(path: Path, seen: set[Path] | None = None) -> dict[str, str]:
    path = path.resolve()
    seen = set() if seen is None else seen
    if path in seen:
        raise pytest.UsageError(f"Target env source cycle includes {path}")
    seen.add(path)

    values: dict[str, str] = {}
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        source_path = _source_path(line, relative_to=path.parent)
        if source_path is not None:
            values.update(_load_env_file(source_path, seen))
            continue
        if line.startswith("export "):
            line = line.removeprefix("export ").strip()
        if "=" not in line:
            continue
        key, raw_value = line.split("=", 1)
        parts = shlex.split(raw_value, comments=True, posix=True)
        values[key.strip()] = parts[0] if parts else ""
    seen.remove(path)
    return values


def _source_path(line: str, *, relative_to: Path) -> Path | None:
    if line.startswith(". "):
        raw_path = line[2:].strip()
    elif line.startswith("source "):
        raw_path = line.removeprefix("source ").strip()
    else:
        return None
    parts = shlex.split(raw_path, comments=True, posix=True)
    if not parts:
        return None
    candidate = Path(parts[0])
    if candidate.is_absolute():
        return candidate
    local_path = relative_to / candidate
    if local_path.is_file():
        return local_path
    return REPO_ROOT / candidate


def _load_token(values: dict[str, str]) -> str | None:
    if values.get("TEST_TAMOSS_TOKEN"):
        return values["TEST_TAMOSS_TOKEN"]
    secret_name = values.get("TEST_TAMOSS_TOKEN_SECRET")
    if not secret_name:
        return None
    namespace = values.get("TEST_TAMOSS_NAMESPACE", "tams")
    kubeconfig = values.get("KUBECONFIG") or os.getenv("KUBECONFIG")
    return load_secret_value(
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
    return load_secret_value(
        kubeconfig=kubeconfig,
        namespace=namespace,
        secret_name=secret_name,
        key=values.get("TEST_TAMOSS_AUTH_PASSWORD_KEY", "AUTHENTIK_BOOTSTRAP_PASSWORD"),
    )


def _load_oauth_client_secret(
    values: dict[str, str],
    *,
    kubeconfig: str | None,
    namespace: str,
    enabled: bool,
) -> str | None:
    if not enabled:
        return None
    if values.get("TEST_TAMOSS_OAUTH_CLIENT_SECRET"):
        return values["TEST_TAMOSS_OAUTH_CLIENT_SECRET"]
    secret_name = values.get("TEST_TAMOSS_OAUTH_CLIENT_SECRET_NAME")
    if not secret_name:
        raise pytest.UsageError(
            "Target sets TEST_TAMOSS_OAUTH2_ENABLED but does not provide "
            "TEST_TAMOSS_OAUTH_CLIENT_SECRET or "
            "TEST_TAMOSS_OAUTH_CLIENT_SECRET_NAME."
        )
    return load_secret_value(
        kubeconfig=kubeconfig,
        namespace=namespace,
        secret_name=secret_name,
        key=values.get(
            "TEST_TAMOSS_OAUTH_CLIENT_SECRET_KEY", "TAMOSS_OAUTH_CLIENT_SECRET"
        ),
    )


def _load_oauth_client_id(
    values: dict[str, str],
    *,
    kubeconfig: str | None,
    namespace: str,
    enabled: bool,
) -> str:
    if values.get("TEST_TAMOSS_OAUTH_CLIENT_ID"):
        return values["TEST_TAMOSS_OAUTH_CLIENT_ID"]
    if not enabled:
        return "tams-api-client"
    secret_name = values.get("TEST_TAMOSS_OAUTH_CLIENT_ID_SECRET_NAME") or values.get(
        "TEST_TAMOSS_OAUTH_CLIENT_SECRET_NAME"
    )
    if not secret_name:
        raise pytest.UsageError(
            "Target sets TEST_TAMOSS_OAUTH2_ENABLED but does not provide "
            "TEST_TAMOSS_OAUTH_CLIENT_ID, TEST_TAMOSS_OAUTH_CLIENT_ID_SECRET_NAME, "
            "or TEST_TAMOSS_OAUTH_CLIENT_SECRET_NAME."
        )
    return load_secret_value(
        kubeconfig=kubeconfig,
        namespace=namespace,
        secret_name=secret_name,
        key=values.get(
            "TEST_TAMOSS_OAUTH_CLIENT_ID_SECRET_KEY", "TAMOSS_OAUTH_CLIENT_ID"
        ),
    )


def _load_oauth_issuer_url(
    values: dict[str, str],
    *,
    kubeconfig: str | None,
    namespace: str,
    cr_name: str | None,
    auth_url: str,
    enabled: bool,
) -> str:
    if values.get("TEST_TAMOSS_OAUTH_ISSUER"):
        return _normalize_url(values["TEST_TAMOSS_OAUTH_ISSUER"])
    if not enabled:
        return ""
    if not cr_name:
        raise pytest.UsageError(
            "Target sets TEST_TAMOSS_OAUTH2_ENABLED but does not provide "
            "TEST_TAMOSS_OAUTH_ISSUER or TEST_TAMOSS_CR_NAME."
        )
    slug = load_jsonpath(
        kubeconfig=kubeconfig,
        namespace=namespace,
        resource=f"tamoss/{cr_name}",
        jsonpath="{.status.auth.applicationSlug}",
    )
    if not slug:
        raise pytest.UsageError(
            f"Tamoss {namespace}/{cr_name} did not report status.auth.applicationSlug."
        )
    return _normalize_url(f"{auth_url}/application/o/{slug}/")


def _normalize_url(value: str) -> str:
    return value.rstrip("/") + "/"


def _required(values: dict[str, str], key: str) -> str:
    value = values.get(key)
    if not value:
        raise pytest.UsageError(f"Target env must set {key}.")
    return value


def _env_bool(value: str | None) -> bool:
    return value is not None and value.lower() in {"1", "true", "yes", "on"}


def _status_set(value: str) -> set[int]:
    statuses: set[int] = set()
    for raw_item in value.split(","):
        item = raw_item.strip()
        if item:
            statuses.add(int(item))
    if not statuses:
        raise pytest.UsageError(
            "TEST_TAMOSS_UI_EXPECT_STATUS must contain a status code."
        )
    return statuses


def _csv_values(value: str) -> tuple[str, ...]:
    return tuple(item.strip() for item in value.split(",") if item.strip())
