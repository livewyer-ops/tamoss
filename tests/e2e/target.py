from __future__ import annotations

import os
import shlex
import subprocess
import warnings
from dataclasses import dataclass, field
from pathlib import Path
from urllib.parse import urlsplit

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
    s3_url: str | None
    namespace: str
    kubeconfig: str | None
    auth_namespace: str
    ui_auth_username: str
    ui_auth_password: str = field(repr=False)
    ui_expected_statuses: set[int]
    verify_tls: bool
    auth_headers: dict[str, str] = field(repr=False)
    basic_auth: HTTPBasicAuth | None = field(repr=False)
    certificate_refs: tuple[str, ...]
    oauth2_enabled: bool
    oauth_issuer_url: str
    oauth_client_id: str
    oauth_client_secret: str | None = field(repr=False)
    readiness_mode: str
    upload_checksum_header: bool
    timeout_seconds: float = 10.0
    memory_budget_mib: int | None = None
    browser_api_available: bool = True

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
            s3_url=_optional_origin(values.get("TEST_TAMOSS_S3", ""), "TEST_TAMOSS_S3"),
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
            readiness_mode=_readiness_mode(
                values.get("TEST_TAMOSS_READINESS_MODE", "tamoss")
            ),
            upload_checksum_header=_env_bool(
                values.get("TEST_TAMOSS_UPLOAD_CHECKSUM_HEADER", "true")
            ),
            timeout_seconds=float(values.get("TEST_TIMEOUT_SECONDS", "10")),
            memory_budget_mib=_memory_budget_mib(
                values.get("TEST_TAMOSS_MEMORY_BUDGET_MIB")
            ),
            browser_api_available=_env_bool(
                values.get("TEST_TAMOSS_BROWSER_API_AVAILABLE", "true")
            ),
        )

    def is_ui_origin(self, url: str) -> bool:
        """Whether url is served by the TAMOSS UI origin."""
        return _url_origin(url) == _url_origin(self.ui_url)

    def is_media_origin(self, url: str) -> bool:
        """Whether url is served by the media origin the target declares.

        Matching is by origin rather than string prefix so a CDN or ingress in
        front of object storage only has to be named once in TEST_TAMOSS_S3,
        whatever bucket or path layout it exposes.
        """
        if not self.s3_url:
            return False
        return _url_origin(url) == _url_origin(self.s3_url)


def _optional_origin(value: str, key: str) -> str | None:
    candidate = value.strip().rstrip("/")
    if not candidate:
        return None
    parts = urlsplit(candidate)
    if parts.scheme not in {"http", "https"} or not parts.netloc:
        raise pytest.UsageError(
            f"{key} must be an absolute http(s) origin such as "
            f"https://s3.example.com; got {value!r}."
        )
    return candidate


def _url_origin(url: str) -> tuple[str, str]:
    parts = urlsplit(url)
    return parts.scheme.lower(), parts.netloc.lower()


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
        return _normalize_token(values["TEST_TAMOSS_TOKEN"])
    if values.get("TEST_TAMOSS_TOKEN_COMMAND"):
        try:
            completed = subprocess.run(
                values["TEST_TAMOSS_TOKEN_COMMAND"],
                check=False,
                shell=True,
                capture_output=True,
                text=True,
                timeout=30,
            )
        except subprocess.TimeoutExpired:
            raise pytest.UsageError(
                "TEST_TAMOSS_TOKEN_COMMAND timed out after 30 seconds."
            ) from None
        if completed.returncode != 0:
            stderr = (completed.stderr or "").strip()[-500:]
            raise pytest.UsageError(
                "TEST_TAMOSS_TOKEN_COMMAND failed with exit code "
                f"{completed.returncode}: {stderr}"
            )
        # The last stdout line wins so the command may emit progress output.
        token = completed.stdout.strip().splitlines()[-1] if completed.stdout else ""
        if not token:
            raise pytest.UsageError("TEST_TAMOSS_TOKEN_COMMAND did not print a token.")
        return _normalize_token(token)
    secret_name = values.get("TEST_TAMOSS_TOKEN_SECRET")
    if not secret_name:
        return None
    namespace = values.get("TEST_TAMOSS_NAMESPACE", "tams")
    kubeconfig = values.get("KUBECONFIG") or os.getenv("KUBECONFIG")
    return _normalize_token(
        load_secret_value(
            kubeconfig=kubeconfig,
            namespace=namespace,
            secret_name=secret_name,
            key="TAMOSS_API_TOKEN",
        )
    )


def _normalize_token(value: str) -> str:
    token = value.strip()
    if token.lower().startswith("bearer "):
        return token[7:].strip()
    return token


def _readiness_mode(value: str) -> str:
    mode = value.strip().lower()
    if mode not in {"tamoss", "service"}:
        raise pytest.UsageError(
            "TEST_TAMOSS_READINESS_MODE must be 'tamoss' or 'service'."
        )
    return mode


def _memory_budget_mib(raw: str | None) -> int | None:
    if raw is None or not raw.strip():
        return None
    try:
        value = int(raw.strip())
    except ValueError:
        value = 0
    if value <= 0:
        raise pytest.UsageError(
            "TEST_TAMOSS_MEMORY_BUDGET_MIB must be a positive integer number of MiB."
        )
    return value


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
