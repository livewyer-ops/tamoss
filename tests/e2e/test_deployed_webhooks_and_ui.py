from __future__ import annotations

import base64
import json
import re
import subprocess
import time
from contextlib import suppress
from pathlib import Path
from string import Template
from typing import Any
from uuid import uuid4

import pytest
from playwright.sync_api import (
    Browser,
    Locator,
    Page,
)
from playwright.sync_api import (
    TimeoutError as PlaywrightTimeoutError,
)

from tests.e2e.conftest import E2EClient, E2ETarget

pytestmark = pytest.mark.e2e

REPO_ROOT = Path(__file__).resolve().parents[2]
TINY_INGEST_MP4 = REPO_ROOT / "tests/fixtures/e2e/tiny-ingest.mp4"
WEBHOOK_RECEIVER_MANIFEST = REPO_ROOT / "tests/fixtures/k8s/webhook-receiver.yaml.tpl"
TAMOSS_API_SCOPES = (
    "tams-api/admin",
    "tams-api/read",
    "tams-api/write",
    "tams-api/delete",
)


def test_deployed_webhook_registration_and_event_status(
    e2e_client: E2EClient,
    e2e_target: E2ETarget,
) -> None:
    source_id = str(uuid4())
    flow_id = str(uuid4())
    webhook_id: str | None = None
    receiver = _start_webhook_receiver(e2e_target)

    try:
        created = e2e_client.request_json(
            "POST",
            "/service/webhooks",
            json={
                "url": receiver["url"],
                "events": ["sources/created", "flows/created"],
                "source_ids": [source_id],
                "api_key_name": "x-api-key",
                "api_key_value": "secret-value",
                "tags": {"suite": "deployed-e2e"},
            },
            expected=201,
        )
        webhook_id = created["id"]
        assert created["status"] == "created"
        assert created["api_key_name"] == "x-api-key"
        assert "api_key_value" not in created

        e2e_client.request_json(
            "PUT",
            f"/flows/{flow_id}",
            json={
                "id": flow_id,
                "source_id": source_id,
                "format": "urn:x-nmos:format:video",
                "codec": "video/h264",
                "container": "video/mp2t",
                "essence_parameters": {
                    "frame_width": 1920,
                    "frame_height": 1080,
                    "frame_rate": {"numerator": 25, "denominator": 1},
                },
            },
            expected=201,
        )

        detail = _poll_webhook_status(e2e_client, webhook_id)
        assert detail["status"] == "started"
        assert detail["source_ids"] == [source_id]
        assert "api_key_value" not in detail

        deliveries = _poll_webhook_receiver_events(
            e2e_target,
            receiver["pod"],
            expected_event_types={"sources/created", "flows/created"},
        )
        event_types = [delivery["body"]["event_type"] for delivery in deliveries]
        assert event_types == ["sources/created", "flows/created"]
        assert all(
            delivery["headers"].get("x-api-key") == "secret-value"
            for delivery in deliveries
        )
        assert deliveries[0]["body"]["event"]["source"]["id"] == source_id
        assert deliveries[1]["body"]["event"]["flow"]["id"] == flow_id
    finally:
        if webhook_id is not None:
            e2e_client.request(
                "DELETE", f"/service/webhooks/{webhook_id}", expected={204, 404}
            )
        e2e_client.request("DELETE", f"/flows/{flow_id}", expected={204, 404})
        _delete_webhook_receiver(e2e_target, receiver["name"])


def test_deployed_ui_ingress_authenticates_and_proxies_api(
    e2e_client: E2EClient, e2e_target: E2ETarget, e2e_browser: Browser
) -> None:
    root = e2e_client.request(
        "GET",
        "/",
        base="ui",
        auth=False,
        allow_redirects=False,
        expected=e2e_client.target.ui_expected_statuses,
    )
    assert root.status_code in e2e_client.target.ui_expected_statuses

    context = e2e_browser.new_context(ignore_https_errors=not e2e_target.verify_tls)
    page = context.new_page()
    try:
        _login_through_ui_ingress(page, e2e_target)
        runtime_config = page.evaluate("() => window.__TAMOSS_CONFIG__")
        assert runtime_config["apiUrl"] == "/api"

        proxied = page.evaluate(
            """async () => {
                const response = await fetch('/api/service', {
                    headers: {Accept: 'application/json'},
                });
                return {
                    status: response.status,
                    contentType: response.headers.get('content-type') || '',
                    body: await response.text(),
                };
            }"""
        )
    finally:
        context.close()

    assert proxied["status"] == 200
    assert "application/json" in proxied["contentType"]
    proxied_service = json.loads(proxied["body"])
    assert proxied_service["api_version"] == "8.0"
    assert {"name": "webhooks"} in proxied_service["event_stream_mechanisms"]


def test_deployed_oauth2_client_credentials_token_grants_api_access(
    e2e_client: E2EClient,
    e2e_target: E2ETarget,
) -> None:
    token_payload = _client_credentials_token(
        e2e_client,
        e2e_target,
        scope=" ".join(TAMOSS_API_SCOPES),
    )
    assert set(TAMOSS_API_SCOPES).issubset(set(token_payload.get("scope", "").split()))
    _assert_bearer_token_grants_service_access(e2e_client, e2e_target, token_payload)


def test_deployed_oauth2_client_credentials_without_scope_grants_api_access(
    e2e_client: E2EClient,
    e2e_target: E2ETarget,
) -> None:
    token_payload = _client_credentials_token(e2e_client, e2e_target)
    assert token_payload.get("scope") == ""
    _assert_bearer_token_grants_service_access(e2e_client, e2e_target, token_payload)


def _client_credentials_token(
    e2e_client: E2EClient,
    e2e_target: E2ETarget,
    *,
    scope: str | None = None,
) -> dict[str, Any]:
    if not e2e_target.kubeconfig:
        pytest.skip("OAuth2 E2E requires Kubernetes secret access")

    client_secret = _secret_value(
        e2e_target,
        namespace=e2e_target.auth_namespace,
        secret_name="tams-authentik",
        key="TAMOSS_OAUTH_CLIENT_SECRET",
    )
    token_request = {
        "grant_type": "client_credentials",
        "client_id": "tams-api-client",
        "client_secret": client_secret,
    }
    if scope is not None:
        token_request["scope"] = scope

    token_response = e2e_client.session.post(
        f"{e2e_target.auth_url}/application/o/token/",
        data=token_request,
        timeout=e2e_target.timeout_seconds,
        verify=e2e_target.verify_tls,
    )
    assert token_response.status_code == 200, token_response.text
    return token_response.json()


def _assert_bearer_token_grants_service_access(
    e2e_client: E2EClient,
    e2e_target: E2ETarget,
    token_payload: dict[str, Any],
) -> None:
    access_token = token_payload["access_token"]

    api_response = e2e_client.session.get(
        f"{e2e_target.api_url}/service",
        headers={"Authorization": f"Bearer {access_token}"},
        timeout=e2e_target.timeout_seconds,
        verify=e2e_target.verify_tls,
    )
    assert api_response.status_code == 200, api_response.text
    assert api_response.json()["api_version"] == "8.0"


def test_deployed_ui_ingest_uploads_and_registers_media(
    e2e_client: E2EClient,
    e2e_target: E2ETarget,
    e2e_browser: Browser,
) -> None:
    media_path = _tiny_ingest_mp4()
    label = f"TAMOSS UI E2E {uuid4()}"
    flow_id: str | None = None

    context = e2e_browser.new_context(ignore_https_errors=not e2e_target.verify_tls)
    page = context.new_page()
    page.set_default_timeout(120_000)
    try:
        _login_through_ui_ingress(page, e2e_target)
        page.goto(f"{e2e_target.ui_url}/ingest", wait_until="domcontentloaded")
        page.get_by_role("button", name="Create source").click()
        page.get_by_label(re.compile("New source label", re.I)).fill(label)
        page.locator("#new-source-format").select_option("urn:x-nmos:format:video")
        page.get_by_label(re.compile("Segment Duration", re.I)).fill("1")
        page.locator("input[type='file']").set_input_files(str(media_path))

        page.get_by_role("button", name="Create Source & Start Ingest").click()
        page.get_by_text("Ingest Complete").wait_for()
        flow_id = _flow_id_from_href(
            page.get_by_role("link", name="Video flow").first.get_attribute("href")
        )
    finally:
        context.close()

    assert flow_id is not None
    flow = e2e_client.request_json("GET", f"/flows/{flow_id}")
    assert flow["format"] == "urn:x-nmos:format:video"
    assert flow["label"] == f"{media_path.name} (video)"

    segments = e2e_client.request_json("GET", f"/flows/{flow_id}/segments")
    assert len(segments) == 1
    object_id = segments[0]["object_id"]
    media_object = e2e_client.request_json("GET", f"/objects/{object_id}")
    assert media_object["id"] == object_id
    assert media_object["referenced_by_flows"] == [flow_id]

    accepted = e2e_client.request("DELETE", f"/flows/{flow_id}", expected={202, 204})
    if accepted.status_code == 202:
        e2e_client.poll_delete_request(accepted.json()["id"])


def _poll_webhook_status(
    e2e_client: E2EClient, webhook_id: str, *, timeout_seconds: float = 60.0
) -> dict:
    deadline = time.monotonic() + timeout_seconds
    last_detail: dict | None = None
    while time.monotonic() < deadline:
        detail = e2e_client.request_json("GET", f"/service/webhooks/{webhook_id}")
        last_detail = detail
        if detail["status"] in {"started", "error"}:
            return detail
        time.sleep(1)
    raise AssertionError(f"Webhook did not observe an event: {last_detail}")


def _login_through_ui_ingress(page: Page, target: E2ETarget) -> None:
    page.goto(target.ui_url, wait_until="domcontentloaded")
    for _ in range(8):
        if _is_app_url(page, target):
            page.wait_for_load_state("domcontentloaded")
            return
        _fill_first_visible(
            page,
            [
                "input[name='uidField']",
                "input[name='username']",
                "input[type='email']",
                "input[type='text']",
            ],
            target.ui_auth_username,
        )
        _fill_first_visible(
            page,
            ["input[name='password']", "input[type='password']"],
            target.ui_auth_password,
        )
        _click_authentik_submit(page)
        page.wait_for_timeout(1000)

    page.wait_for_url(
        re.compile(f"^{re.escape(target.ui_url.rstrip('/'))}(/|$)"),
        timeout=60_000,
    )
    page.wait_for_load_state("domcontentloaded")


def _is_app_url(page: Page, target: E2ETarget) -> bool:
    return page.url.startswith(target.ui_url.rstrip("/"))


def _fill_first_visible(page: Page, selectors: list[str], value: str) -> bool:
    for selector in selectors:
        locator = page.locator(selector)
        for index in range(locator.count()):
            candidate = locator.nth(index)
            if candidate.is_visible():
                candidate.fill(value)
                return True
    return False


def _click_authentik_submit(page: Page) -> None:
    for label in ["Continue", "Sign in", "Log in", "Login", "Submit"]:
        button = page.get_by_role("button", name=re.compile(label, re.I))
        if button.count():
            _click_authentik_button(page, button.first)
            return
    _click_authentik_button(
        page, page.locator("button[type='submit'], input[type='submit']").first
    )


def _click_authentik_button(page: Page, button: Locator) -> None:
    with suppress(PlaywrightTimeoutError):
        page.locator("ak-loading-overlay").wait_for(
            state="hidden",
            timeout=10_000,
        )

    try:
        button.click(timeout=10_000)
    except PlaywrightTimeoutError:
        try:
            button.evaluate("(element) => element.click()", timeout=1_000)
        except PlaywrightTimeoutError:
            return


def _start_webhook_receiver(target: E2ETarget) -> dict[str, str]:
    name = f"tamoss-webhook-{uuid4().hex[:8]}"
    image = _deployed_worker_image(target)
    manifest = Template(
        WEBHOOK_RECEIVER_MANIFEST.read_text(encoding="utf-8")
    ).substitute(
        image=image,
        name=name,
        namespace=target.namespace,
    )
    _kubectl(target, "apply", "-f", "-", input_text=manifest)
    _kubectl(
        target,
        "-n",
        target.namespace,
        "wait",
        "--for=condition=Ready",
        f"pod/{name}",
        "--timeout=60s",
    )
    return {
        "name": name,
        "pod": name,
        "url": f"http://{name}.{target.namespace}.svc.cluster.local:8080/events",
    }


def _delete_webhook_receiver(target: E2ETarget, name: str) -> None:
    _kubectl(
        target,
        "-n",
        target.namespace,
        "delete",
        "pod,svc",
        "-l",
        f"app.kubernetes.io/name={name}",
        "--ignore-not-found=true",
    )


def _poll_webhook_receiver_events(
    target: E2ETarget,
    pod_name: str,
    *,
    expected_event_types: set[str],
    timeout_seconds: float = 90.0,
) -> list[dict]:
    deadline = time.monotonic() + timeout_seconds
    last_events: list[dict] = []
    while time.monotonic() < deadline:
        events = _receiver_events(target, pod_name)
        last_events = events
        seen = {event["body"].get("event_type") for event in events}
        if expected_event_types.issubset(seen):
            return [
                event
                for event in events
                if event["body"].get("event_type") in expected_event_types
            ]
        time.sleep(1)
    raise AssertionError(f"Webhook receiver did not observe events: {last_events}")


def _receiver_events(target: E2ETarget, pod_name: str) -> list[dict]:
    result = _kubectl(
        target,
        "-n",
        target.namespace,
        "exec",
        pod_name,
        "--",
        "sh",
        "-c",
        "test -f /tmp/webhook-events.jsonl && cat /tmp/webhook-events.jsonl || true",
    )
    return [json.loads(line) for line in result.stdout.splitlines() if line.strip()]


def _secret_value(
    target: E2ETarget,
    *,
    namespace: str,
    secret_name: str,
    key: str,
) -> str:
    result = _kubectl(
        target,
        "-n",
        namespace,
        "get",
        "secret",
        secret_name,
        "-o",
        f"jsonpath={{.data.{key}}}",
    )
    return base64.b64decode(result.stdout.strip(), validate=True).decode().strip()


def _deployed_worker_image(target: E2ETarget) -> str:
    result = _kubectl(
        target,
        "-n",
        target.namespace,
        "get",
        "deploy",
        "tams-worker",
        "-o",
        "jsonpath={.spec.template.spec.containers[0].image}",
    )
    image = result.stdout.strip()
    assert image
    return image


def _kubectl(
    target: E2ETarget,
    *args: str,
    input_text: str | None = None,
) -> subprocess.CompletedProcess[str]:
    command = ["kubectl"]
    if target.kubeconfig:
        command.extend(["--kubeconfig", target.kubeconfig])
    command.extend(args)
    return subprocess.run(
        command,
        input=input_text,
        text=True,
        check=True,
        capture_output=True,
    )


def _flow_id_from_href(href: str | None) -> str:
    assert href is not None
    match = re.search(r"/flows/([^/?#]+)", href)
    assert match is not None
    return match.group(1)


def _tiny_ingest_mp4() -> Path:
    assert TINY_INGEST_MP4.exists()
    return TINY_INGEST_MP4
