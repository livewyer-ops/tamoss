from __future__ import annotations

import time
from collections.abc import Iterable
from typing import Any
from urllib.parse import urljoin

import pytest
import requests

from tests.e2e.target import E2ETarget


@pytest.fixture(scope="session")
def e2e_client(e2e_target: E2ETarget) -> E2EClient:
    client = E2EClient(e2e_target)
    client.wait_ready()
    return client


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
            except Exception as exc:
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
