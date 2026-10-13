from __future__ import annotations

import importlib.util
import io
import traceback
from pathlib import Path
from urllib.error import HTTPError

import pytest

ROOT = Path(__file__).resolve().parents[1] / "operator/test/chainsaw"
WEB_IDENTITY = "storagebackend-reconciliation/default-web-identity/test_web_identity.py"
MULTI_BACKEND = (
    "storagebackend-reconciliation/multiple-storagebackends-ingest/"
    "test_multiple_storagebackends.py"
)


def load_fixture(relative_path):
    spec = importlib.util.spec_from_file_location("fixture_check", ROOT / relative_path)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


@pytest.fixture(autouse=True)
def fixture_imports(monkeypatch):
    monkeypatch.syspath_prepend(ROOT / "fixtures")


@pytest.mark.parametrize("scheme", ["file", "data"])
@pytest.mark.parametrize("method", ["GET", "PUT"])
@pytest.mark.parametrize("relative_path", [WEB_IDENTITY, MULTI_BACKEND])
def test_media_checks_reject_non_http_urls(scheme, method, relative_path):
    url = f"{scheme}:///private-fixture-data"
    module = load_fixture(relative_path)

    with pytest.raises(AssertionError, match=r"HTTP\(S\)"):
        module.request(method, url)


@pytest.mark.parametrize("relative_path", [WEB_IDENTITY, MULTI_BACKEND])
@pytest.mark.parametrize("method", ["GET", "PUT"])
def test_http_failure_diagnostics_omit_response_secrets(
    relative_path, method, monkeypatch
):
    module = load_fixture(relative_path)
    marker = "PRIVATE_RESPONSE_MARKER"

    def fail(*_args, **_kwargs):
        raise HTTPError(
            f"http://fixture/?token={marker}",
            403,
            marker,
            {},
            io.BytesIO(marker.encode()),
        )

    monkeypatch.setattr("media_requests.urlopen", fail)

    with pytest.raises(AssertionError) as caught:
        module.request(method, "/flows")

    assert "403" in str(caught.value)
    assert marker not in "".join(traceback.format_exception(caught.value))
