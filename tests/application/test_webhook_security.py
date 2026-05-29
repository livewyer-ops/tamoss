from __future__ import annotations

import ipaddress
from typing import ClassVar

import pytest
from tamoss.application import webhooks


@pytest.mark.parametrize(
    "url",
    [
        "http://127.0.0.1/hook",
        "http://[::1]/hook",
        "http://169.254.169.254/latest/meta-data",
        "https://receiver.default.svc.cluster.local/hook",
        "https://receiver.default.svc/hook",
    ],
)
def test_webhook_url_rejects_restricted_targets(url: str) -> None:
    with pytest.raises(ValueError, match="restricted network destination"):
        webhooks.validate_webhook_url(url)


def test_webhook_url_rejects_hosts_that_resolve_to_private_addresses(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(
        webhooks,
        "_resolve_host_addresses",
        lambda hostname, port: [ipaddress.ip_address("10.10.0.20")],
    )

    with pytest.raises(ValueError, match="resolves to a restricted"):
        webhooks.validate_webhook_url("https://receiver.example.test/hook")


def test_webhook_delivery_revalidates_destination_addresses(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setattr(
        webhooks,
        "_resolve_host_addresses",
        lambda hostname, port: [ipaddress.ip_address("10.10.0.20")],
    )

    with pytest.raises(webhooks.WebhookEgressError, match="restricted"):
        webhooks.send_webhook_delivery(
            webhook={"url": "https://receiver.example.test/hook"},
            payload={"event_type": "flows/created"},
            timeout_seconds=1,
        )


def test_webhook_delivery_blocks_redirect_to_private_target(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    post_calls: list[dict[str, object]] = []

    class RedirectResponse:
        status_code = 302
        headers: ClassVar[dict[str, str]] = {"Location": "http://127.0.0.1/internal"}

    def post(*_args: object, **kwargs: object) -> RedirectResponse:
        post_calls.append(kwargs)
        return RedirectResponse()

    monkeypatch.setattr(
        webhooks,
        "_resolve_host_addresses",
        lambda hostname, port: [ipaddress.ip_address("93.184.216.34")],
    )
    monkeypatch.setattr(webhooks.requests, "post", post)

    with pytest.raises(webhooks.WebhookEgressError, match="restricted"):
        webhooks.send_webhook_delivery(
            webhook={"url": "https://receiver.example.test/hook"},
            payload={"event_type": "flows/created"},
            timeout_seconds=1,
        )

    assert post_calls[0]["allow_redirects"] is False


def test_webhook_url_accepts_allowlisted_private_targets() -> None:
    policy = webhooks.WebhookEgressPolicy(allowed_hosts=("10.10.0.0/16",))

    webhooks.validate_webhook_url("https://10.10.0.20/hook", egress_policy=policy)


def test_webhook_delivery_snapshot_uses_credential_reference() -> None:
    snapshot = webhooks.webhook_delivery_snapshot(
        {
            "url": "https://receiver.example.test/hook",
            "api_key_name": "x-api-key",
            "api_key_value": "secret-value",
        },
        status="started",
    )

    assert "api_key_value" not in snapshot
    assert snapshot["api_key_value_ref"] == "webhook.api_key_value"

    delivery_webhook = webhooks.webhook_for_delivery(
        snapshot,
        {"api_key_value": "secret-value"},
    )
    assert delivery_webhook["api_key_name"] == "x-api-key"
    assert delivery_webhook["api_key_value"] == "secret-value"
