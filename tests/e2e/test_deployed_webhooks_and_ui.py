from __future__ import annotations

import json
import re
import time
from contextlib import suppress
from pathlib import Path
from string import Template
from subprocess import CalledProcessError, CompletedProcess
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

from tests.e2e.client import E2EClient
from tests.e2e.kubernetes import kubectl
from tests.e2e.target import E2ETarget
from tests.support.fixtures import load_json_fixture
from tests.support.paths import REPO_ROOT

pytestmark = pytest.mark.e2e

TINY_INGEST_MP4 = REPO_ROOT / "tests/fixtures/e2e/tiny-ingest.mp4"
DEMO_FLOW_ID = "00000000-0000-4000-8000-000000000102"
WEBHOOK_RECEIVER_MANIFEST = REPO_ROOT / "tests/fixtures/k8s/webhook-receiver.yaml.tpl"
TAMOSS_API_SCOPES = (
    "tams-api/admin",
    "tams-api/read",
    "tams-api/write",
    "tams-api/delete",
)


@pytest.mark.smoke
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
            json=_webhook_payload(receiver["url"], source_id),
            expected=201,
        )
        webhook_id = created["id"]
        assert created["status"] == "created"
        assert created["api_key_name"] == "x-api-key"
        assert "api_key_value" not in created

        e2e_client.request_json(
            "PUT",
            f"/flows/{flow_id}",
            json=_video_flow_payload(flow_id, source_id),
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
        deliveries_by_type = {
            delivery["body"]["event_type"]: delivery for delivery in deliveries
        }
        assert set(deliveries_by_type) == {"sources/created", "flows/created"}
        assert all(
            delivery["headers"].get("x-api-key") == "secret-value"
            for delivery in deliveries
        )
        assert (
            deliveries_by_type["sources/created"]["body"]["event"]["source"]["id"]
            == source_id
        )
        assert (
            deliveries_by_type["flows/created"]["body"]["event"]["flow"]["id"]
            == flow_id
        )
    finally:
        if webhook_id is not None:
            e2e_client.request(
                "DELETE", f"/service/webhooks/{webhook_id}", expected={204, 404}
            )
        e2e_client.request("DELETE", f"/flows/{flow_id}", expected={204, 404})
        _delete_webhook_receiver(e2e_target, receiver["name"])


@pytest.mark.smoke
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
    assert proxied_service["api_version"] == "8.1"
    assert {"name": "webhooks"} in proxied_service["event_stream_mechanisms"]


def test_deployed_cert_manager_certificates_are_ready(e2e_target: E2ETarget) -> None:
    if not e2e_target.kubeconfig:
        pytest.skip("cert-manager certificate checks require Kubernetes access")
    if not e2e_target.certificate_refs:
        pytest.skip("no cert-manager Certificate refs configured for this target")

    for ref in e2e_target.certificate_refs:
        namespace, name = _split_resource_ref(
            ref, default_namespace=e2e_target.namespace
        )
        result = _kubectl_for_target(
            e2e_target,
            "-n",
            namespace,
            "get",
            "certificate",
            name,
            "-o",
            "json",
        )
        certificate = json.loads(result.stdout)
        ready = next(
            (
                condition
                for condition in certificate.get("status", {}).get("conditions", [])
                if condition.get("type") == "Ready"
            ),
            None,
        )
        assert ready is not None, (
            f"Certificate {namespace}/{name} has no Ready condition"
        )
        assert ready.get("status") == "True", (
            f"Certificate {namespace}/{name} is not Ready: {ready}"
        )


_MEMORY_UNIT_MIB = {"Ki": 1 / 1024, "Mi": 1.0, "Gi": 1024.0, "Ti": 1024.0 * 1024.0}


def test_deployed_node_memory_usage_stays_within_budget(
    e2e_target: E2ETarget,
) -> None:
    if e2e_target.memory_budget_mib is None:
        pytest.skip("no TEST_TAMOSS_MEMORY_BUDGET_MIB configured for this target")
    if not e2e_target.kubeconfig:
        pytest.skip("node memory checks require Kubernetes access")

    try:
        result = _kubectl_for_target(e2e_target, "top", "node", "--no-headers")
    except CalledProcessError as exc:
        pytest.fail(f"kubectl top node failed: {exc.stderr or exc}")
    rows = [line.split() for line in result.stdout.splitlines() if line.strip()]
    assert rows, "kubectl top node returned no nodes"
    for row in rows:
        node_name, memory_raw = row[0], row[3]
        memory_mib = _memory_quantity_to_mib(memory_raw)
        assert memory_mib < e2e_target.memory_budget_mib, (
            f"Node {node_name} memory usage {memory_raw} is not below the "
            f"{e2e_target.memory_budget_mib}Mi budget"
        )


def _memory_quantity_to_mib(raw: str) -> float:
    match = re.fullmatch(r"(\d+)(Ki|Mi|Gi|Ti)", raw)
    assert match, f"Unrecognised kubectl top memory quantity: {raw!r}"
    return int(match.group(1)) * _MEMORY_UNIT_MIB[match.group(2)]


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


def test_deployed_oidc_metadata_and_jwks_are_ready(
    e2e_client: E2EClient,
    e2e_target: E2ETarget,
) -> None:
    if not e2e_target.oauth2_enabled:
        pytest.skip("OAuth2 E2E is disabled for this target")

    metadata_response = e2e_client.session.get(
        f"{e2e_target.oauth_issuer_url}.well-known/openid-configuration",
        timeout=e2e_target.timeout_seconds,
        verify=e2e_target.verify_tls,
    )
    assert metadata_response.status_code == 200, metadata_response.text
    metadata = metadata_response.json()
    assert metadata["issuer"] == e2e_target.oauth_issuer_url
    assert metadata["token_endpoint"] == f"{e2e_target.auth_url}/application/o/token/"

    jwks_response = e2e_client.session.get(
        metadata["jwks_uri"],
        timeout=e2e_target.timeout_seconds,
        verify=e2e_target.verify_tls,
    )
    assert jwks_response.status_code == 200, jwks_response.text
    keys = jwks_response.json().get("keys", [])
    assert any(key.get("kty") == "RSA" and key.get("kid") for key in keys)


def _client_credentials_token(
    e2e_client: E2EClient,
    e2e_target: E2ETarget,
    *,
    scope: str | None = None,
) -> dict[str, Any]:
    if not e2e_target.oauth2_enabled:
        pytest.skip("OAuth2 E2E is disabled for this target")
    if not e2e_target.oauth_client_secret:
        pytest.skip("OAuth2 E2E requires a client secret")
    token_request = {
        "grant_type": "client_credentials",
        "client_id": e2e_target.oauth_client_id,
        "client_secret": e2e_target.oauth_client_secret,
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


def _webhook_payload(receiver_url: str, source_id: str) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("e2e/webhook_registration.json")
    payload["url"] = receiver_url
    payload["source_ids"] = [source_id]
    return payload


def _video_flow_payload(flow_id: str, source_id: str) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/video_flow_payload.json")
    payload["id"] = flow_id
    payload["source_id"] = source_id
    return payload


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
    assert api_response.json()["api_version"] == "8.1"


@pytest.mark.smoke
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


@pytest.mark.smoke
def test_deployed_ui_playback_preview_buffers_demo_media(
    e2e_target: E2ETarget,
    e2e_browser: Browser,
) -> None:
    context = e2e_browser.new_context(ignore_https_errors=not e2e_target.verify_tls)
    page = context.new_page()
    page.set_default_timeout(60_000)
    try:
        _login_through_ui_ingress(page, e2e_target)
        page.goto(
            f"{e2e_target.ui_url}/playback?flow={DEMO_FLOW_ID}",
            wait_until="domcontentloaded",
        )
        page.get_by_text("Preview ready").wait_for()
        play_result = page.evaluate(
            """async () => {
                const video = document.querySelector('video');
                if (!video) return {ok: false, error: 'no video element'};
                video.muted = true;
                video.currentTime = 0;
                try {
                    await video.play();
                    return {ok: true};
                } catch (err) {
                    return {ok: false, error: String(err)};
                }
            }"""
        )
        page.wait_for_timeout(3_000)
        state = page.evaluate(
            """() => {
                const video = document.querySelector('video');
                if (!video) return null;
                return {
                    readyState: video.readyState,
                    paused: video.paused,
                    currentTime: video.currentTime,
                    duration: video.duration,
                    bufferedEnd: video.buffered.length
                        ? video.buffered.end(video.buffered.length - 1)
                        : 0,
                };
            }"""
        )
    finally:
        context.close()

    assert play_result["ok"], play_result
    assert state is not None
    assert state["readyState"] >= 2
    assert state["duration"] > 0
    assert state["currentTime"] >= min(0.75, state["duration"])
    assert state["bufferedEnd"] >= state["currentTime"]


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
    app_root_pattern = _app_root_url_pattern(target)
    page.goto(target.ui_url, wait_until="domcontentloaded")
    for _ in range(8):
        if app_root_pattern.match(page.url) and _has_runtime_config(page):
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

    page.wait_for_url(app_root_pattern, timeout=60_000)
    page.wait_for_load_state("domcontentloaded")
    page.wait_for_function("() => Boolean(window.__TAMOSS_CONFIG__)", timeout=60_000)


def _app_root_url_pattern(target: E2ETarget) -> re.Pattern[str]:
    app_root = re.escape(target.ui_url.rstrip("/"))
    return re.compile(f"^{app_root}/?(?:[?#].*)?$")


def _has_runtime_config(page: Page) -> bool:
    with suppress(PlaywrightTimeoutError):
        return bool(
            page.wait_for_function(
                "() => Boolean(window.__TAMOSS_CONFIG__)",
                timeout=1_000,
            )
        )
    return False


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
    _kubectl_for_target(target, "apply", "-f", "-", input_text=manifest)
    _kubectl_for_target(
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
        "url": f"http://{name}.{target.namespace}.svc.cluster.local/events",
    }


def _delete_webhook_receiver(target: E2ETarget, name: str) -> None:
    _kubectl_for_target(
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
    result = _kubectl_for_target(
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


def _split_resource_ref(ref: str, *, default_namespace: str) -> tuple[str, str]:
    if "/" in ref:
        namespace, name = ref.split("/", 1)
        return namespace, name
    return default_namespace, ref


def _deployed_worker_image(target: E2ETarget) -> str:
    result = _kubectl_for_target(
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


def _kubectl_for_target(
    target: E2ETarget,
    *args: str,
    input_text: str | None = None,
) -> CompletedProcess[str]:
    return kubectl(
        kubeconfig=target.kubeconfig,
        args=list(args),
        input_text=input_text,
    )


def _flow_id_from_href(href: str | None) -> str:
    assert href is not None
    match = re.search(r"/flows/([^/?#]+)", href)
    assert match is not None
    return match.group(1)


def _tiny_ingest_mp4() -> Path:
    assert TINY_INGEST_MP4.exists()
    return TINY_INGEST_MP4
