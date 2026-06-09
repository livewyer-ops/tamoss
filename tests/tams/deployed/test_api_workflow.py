from __future__ import annotations

import base64
import hashlib
import json
from typing import Any
from uuid import uuid4

import pytest

from tests.e2e.client import E2EClient
from tests.support.fixtures import load_json_fixture

pytestmark = [
    pytest.mark.e2e,
    pytest.mark.tams_conformance,
]

DEMO_SOURCE_ID = "00000000-0000-4000-8000-000000000101"
DEMO_FLOW_ID = "00000000-0000-4000-8000-000000000102"
DEMO_OBJECT_ID = "tamoss-demo/tamoss-demo.ts"
DEMO_TIMERANGE = "[0:0_1:0)"
DEMO_OBJECT_TIMERANGE = "[1:600000000_2:600000000)"
DEMO_TS_OFFSET = "-1:600000000"
DEMO_LAST_DURATION = "0:100000000"


@pytest.mark.smoke
def test_deployed_default_install_has_playable_demo_media(
    e2e_client: E2EClient,
) -> None:
    sources = e2e_client.request_json("GET", "/sources", params={"limit": "100"})
    flows = e2e_client.request_json("GET", "/flows", params={"limit": "100"})
    assert not any(_payload_mentions(item, "bunny") for item in [*sources, *flows])

    backends = e2e_client.request_json("GET", "/service/storage-backends")
    default_backend = next((item for item in backends if item["default_storage"]), None)
    assert default_backend is not None

    flow = e2e_client.request_json("GET", f"/flows/{DEMO_FLOW_ID}")
    assert flow["id"] == DEMO_FLOW_ID
    assert flow["source_id"] == DEMO_SOURCE_ID
    assert flow["label"] == "TAMOSS Demo"
    assert flow["container"] == "video/mp2t"

    source = e2e_client.request_json("GET", f"/sources/{DEMO_SOURCE_ID}")
    assert source["id"] == DEMO_SOURCE_ID
    assert source["label"] == "TAMOSS Demo"

    segments = e2e_client.request_json(
        "GET",
        f"/flows/{DEMO_FLOW_ID}/segments",
        params={
            "limit": "1",
            "object_id": DEMO_OBJECT_ID,
            "accept_storage_ids": default_backend["id"],
            "presigned": "true",
            "verbose_storage": "true",
            "include_object_timerange": "true",
        },
    )
    assert len(segments) == 1
    assert segments[0]["object_id"] == DEMO_OBJECT_ID
    assert segments[0]["timerange"] == DEMO_TIMERANGE
    assert segments[0]["object_timerange"] == DEMO_OBJECT_TIMERANGE
    assert segments[0]["ts_offset"] == DEMO_TS_OFFSET
    assert segments[0]["last_duration"] == DEMO_LAST_DURATION
    assert segments[0]["key_frame_count"] == 1

    get_urls = segments[0]["get_urls"]
    assert len(get_urls) == 1
    assert get_urls[0]["storage_id"] == default_backend["id"]
    assert get_urls[0]["presigned"] is True

    media = e2e_client.request("GET", get_urls[0]["url"], auth=False)
    assert media.content


@pytest.mark.smoke
def test_deployed_storage_object_lifecycle_and_async_delete(
    e2e_client: E2EClient,
) -> None:
    service = e2e_client.request_json("GET", "/service")
    assert service["api_version"] == "8.1"
    assert {"name": "webhooks"} in service["event_stream_mechanisms"]

    backends = e2e_client.request_json("GET", "/service/storage-backends")
    default_backend = next((item for item in backends if item["default_storage"]), None)
    assert default_backend is not None

    flow_id = str(uuid4())
    source_id = str(uuid4())
    object_id = f"bbc-e2e-{uuid4()}.ts"
    uploaded_body = b"tamoss deployed e2e segment\n"
    flow_payload = _video_flow_payload(
        flow_id,
        source_id,
        label=f"TAMOSS E2E {flow_id[:8]}",
    )

    deleted = False
    try:
        created = e2e_client.request_json(
            "PUT", f"/flows/{flow_id}", json=flow_payload, expected=201
        )
        assert created["source_id"] == source_id
        assert (
            e2e_client.request_json("GET", f"/sources/{source_id}")["id"] == source_id
        )

        allocated = e2e_client.request_json(
            "POST",
            f"/flows/{flow_id}/storage",
            json=_storage_allocation_payload(object_id, default_backend["id"]),
            expected=201,
        )
        media_object = allocated["media_objects"][0]
        assert media_object["object_id"] == object_id
        put_request = media_object["put_url"]
        put_headers = _put_url_headers(put_request)
        if e2e_client.target.upload_checksum_header:
            put_headers["x-amz-checksum-sha256"] = _checksum_value(
                uploaded_body,
                "sha256",
            )

        e2e_client.upload_put_url(
            put_request["url"],
            body=uploaded_body,
            headers=put_headers,
        )

        segment = e2e_client.request(
            "POST",
            f"/flows/{flow_id}/segments",
            json=_segment_payload(object_id),
            expected=201,
        )
        assert segment.status_code == 201

        # Label-based get_url negotiation is exercised only when the target's
        # default backend advertises a label.
        accept_params: dict[str, str] = {}
        if default_backend.get("label"):
            accept_params["accept_get_urls"] = default_backend["label"]

        segments = e2e_client.request(
            "GET",
            f"/flows/{flow_id}/segments",
            params={
                "limit": "1",
                **accept_params,
                "accept_storage_ids": default_backend["id"],
                "presigned": "true",
                "verbose_storage": "true",
            },
        )
        assert segments.headers["x-paging-count"] == "1"
        segment_payload = segments.json()[0]
        assert segment_payload["get_urls"][0]["storage_id"] == default_backend["id"]
        direct_get_url = segment_payload["get_urls"][0]["url"]
        direct_object = e2e_client.request("GET", direct_get_url, auth=False)
        assert direct_object.content == uploaded_body

        media = e2e_client.request_json(
            "GET",
            f"/objects/{object_id}",
            params={
                **accept_params,
                "accept_storage_ids": default_backend["id"],
                "presigned": "true",
                "verbose_storage": "true",
            },
        )
        assert media["id"] == object_id
        assert media["first_referenced_by_flow"] == flow_id
        assert media["referenced_by_flows"] == [flow_id]

        accepted = e2e_client.request_json(
            "DELETE", f"/flows/{flow_id}", expected={202}
        )
        delete_result = e2e_client.poll_delete_request(accepted["id"])
        assert delete_result["status"] == "done"
        deleted = True

        e2e_client.request("GET", f"/flows/{flow_id}", expected=404)
        e2e_client.request("GET", f"/sources/{source_id}", expected=404)
        e2e_client.request("GET", f"/objects/{object_id}", expected=404)
        e2e_client.request("GET", direct_get_url, auth=False, expected={403, 404})
    finally:
        if not deleted:
            cleanup = e2e_client.request(
                "DELETE", f"/flows/{flow_id}", expected={202, 204, 404}
            )
            if cleanup.status_code == 202:
                e2e_client.poll_delete_request(cleanup.json()["id"])


def _payload_mentions(payload: object, value: str) -> bool:
    return value.lower() in json.dumps(payload, sort_keys=True).lower()


def _video_flow_payload(flow_id: str, source_id: str, *, label: str) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/video_flow_payload.json")
    payload["id"] = flow_id
    payload["source_id"] = source_id
    payload["label"] = label
    return payload


def _segment_payload(object_id: str) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/segment_payload.json")
    payload["object_id"] = object_id
    return payload


def _storage_allocation_payload(object_id: str, storage_id: str) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/storage_allocation.json")
    payload["object_ids"] = [object_id]
    payload["storage_id"] = storage_id
    return payload


def _put_url_headers(put_url: dict[str, Any]) -> dict[str, str]:
    headers = dict(put_url.get("headers") or {})
    content_type = put_url.get("content-type") or put_url.get("content_type")
    if content_type and not any(name.lower() == "content-type" for name in headers):
        headers["Content-Type"] = str(content_type)
    return headers


def _checksum_value(body: bytes, algorithm: str) -> str:
    digest = hashlib.new(algorithm, body).digest()
    return base64.b64encode(digest).decode("ascii")
