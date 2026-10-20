from __future__ import annotations

import importlib.util
import io
import os
import subprocess
import sys
import traceback
from http.client import InvalidURL
from pathlib import Path
from urllib.error import HTTPError, URLError

import pytest

ROOT = Path(__file__).resolve().parents[1] / "operator/test/chainsaw"
WEB_IDENTITY = "storagebackend-reconciliation/default-web-identity/test_web_identity.py"


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
def test_media_checks_reject_non_http_urls(scheme, method):
    url = f"{scheme}:///private-fixture-data"
    module = load_fixture("fixtures/media_requests.py")

    with pytest.raises(AssertionError, match=r"HTTP\(S\)"):
        module.request(method, url)


@pytest.mark.parametrize("method", ["GET", "PUT"])
def test_http_failure_diagnostics_omit_response_secrets(method, monkeypatch):
    module = load_fixture("fixtures/media_requests.py")

    marker = "PRIVATE_RESPONSE_MARKER"

    def fail(*_args, **_kwargs):
        raise HTTPError(
            f"http://fixture/?token={marker}",
            403,
            marker,
            {},
            io.BytesIO(marker.encode()),
        )

    monkeypatch.setattr(module, "urlopen", fail)

    with pytest.raises(AssertionError) as caught:
        module.request(method, "/flows")

    assert "403" in str(caught.value)
    assert marker not in "".join(traceback.format_exception(caught.value))


@pytest.mark.parametrize("method", ["GET", "PUT"])
@pytest.mark.parametrize("error_type", [InvalidURL, URLError, OSError])
def test_transport_failure_diagnostics_omit_secrets(method, error_type, monkeypatch):
    module = load_fixture("fixtures/media_requests.py")

    marker = "PRIVATE_TRANSPORT_MARKER"

    def fail(*_args, **_kwargs):
        raise error_type(f"http://fixture/?X-Amz-Signature={marker}")

    monkeypatch.setattr(module, "urlopen", fail)

    with pytest.raises(AssertionError, match="HTTP operation failed") as caught:
        module.request(method, "/flows")

    assert marker not in "".join(traceback.format_exception(caught.value))


@pytest.mark.parametrize("method", ["GET", "PUT"])
@pytest.mark.parametrize("authority", ["127.0.0.1:1/bad path", "[invalid-host]"])
def test_malformed_signed_urls_are_redacted(method, authority):
    module = load_fixture("fixtures/media_requests.py")

    marker = "PRIVATE_URL_MARKER"
    with pytest.raises(AssertionError) as caught:
        module.request(method, f"http://{authority}?X-Amz-Signature={marker}")

    assert marker not in "".join(traceback.format_exception(caught.value))


def test_native_pytest_failure_output_omits_signed_url(tmp_path):
    marker = "PRIVATE_PYTEST_MARKER"
    test = tmp_path / "test_media_failure.py"
    test.write_text(
        "import os\n"
        "from media_requests import request\n"
        "def test_request():\n"
        "    request('GET', os.environ['SIGNED_URL'])\n"
    )
    result = subprocess.run(
        [
            sys.executable,
            "-m",
            "pytest",
            "--assert=plain",
            "--tb=short",
            "--show-capture=no",
            "-q",
            str(test),
        ],
        env=os.environ
        | {
            "PYTHONPATH": str(ROOT / "fixtures"),
            "SIGNED_URL": f"http://127.0.0.1:1/bad path?X-Amz-Signature={marker}",
        },
        capture_output=True,
        text=True,
        check=False,
        timeout=30,
    )
    assert result.returncode == 1
    assert "HTTP operation failed" in result.stdout
    assert marker not in result.stdout + result.stderr


@pytest.mark.parametrize("phase", ["secret", "reenabled"])
@pytest.mark.parametrize(
    "damage", [None, "missing-flow", "missing-object", "corrupt-bytes"]
)
def test_credential_transition_detects_media_loss_without_repair(
    phase, damage, monkeypatch
):
    module = load_fixture(WEB_IDENTITY)
    monkeypatch.setenv("TEST_PHASE", phase)
    monkeypatch.setenv("EXPECT_SESSION", str(phase == "reenabled").lower())
    methods = []
    deleted = []
    monkeypatch.setattr(module, "static_client", lambda _prefix: None)
    monkeypatch.setattr(module, "delete_media", lambda *args: deleted.append(args[:2]))

    def request(method, url, *_args, **_kwargs):
        methods.append(method)
        assert method == "GET"
        if url.startswith("/flows/") and "include_timerange" in url:
            if damage == "missing-flow":
                raise AssertionError("HTTP operation failed with status 404")
            return {
                "id": module.RETAINED_FLOW,
                "source_id": module.RETAINED_FLOW,
                "timerange": "[0:0_1:0)",
            }
        if url.endswith("/segments"):
            return [{"object_id": module.RETAINED_OBJECT, "timerange": "[0:0_1:0)"}]
        if url.startswith("/objects/"):
            if damage == "missing-object":
                raise AssertionError("HTTP operation failed with status 404")
            default_url = "http://fixture/default"
            if phase == "reenabled":
                default_url += "?X-Amz-Security-Token=fixture"
            return {
                "get_urls": [
                    {"storage_id": module.DEFAULT, "url": default_url},
                    {"storage_id": module.ADDITIONAL, "url": "http://fixture/archive"},
                ]
            }
        return b"corrupt media" if damage == "corrupt-bytes" else module.BODY

    monkeypatch.setattr(module, "request", request)
    if damage is None:
        module.test_retained_media()
    else:
        with pytest.raises(AssertionError):
            module.test_retained_media()
    assert methods and set(methods) == {"GET"}
    assert deleted == (
        [(module.RETAINED_FLOW, module.RETAINED_OBJECT)]
        if damage is None and phase == "reenabled"
        else []
    )
