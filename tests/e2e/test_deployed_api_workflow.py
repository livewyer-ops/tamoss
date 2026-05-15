from __future__ import annotations

from uuid import uuid4

import pytest

from tests.e2e.conftest import E2EClient

pytestmark = pytest.mark.e2e


def test_deployed_storage_ingest_and_async_delete(e2e_client: E2EClient) -> None:
    service = e2e_client.request_json("GET", "/service")
    assert service["api_version"] == "8.0"
    assert {"name": "webhooks"} in service["event_stream_mechanisms"]

    backends = e2e_client.request_json("GET", "/service/storage-backends")
    default_backend = next((item for item in backends if item["default_storage"]), None)
    assert default_backend is not None

    flow_id = str(uuid4())
    source_id = str(uuid4())
    object_id = f"bbc/e2e/{uuid4()}.ts"
    flow_payload = {
        "id": flow_id,
        "source_id": source_id,
        "format": "urn:x-nmos:format:video",
        "codec": "video/h264",
        "container": "video/mp2t",
        "label": f"TAMOSS E2E {flow_id[:8]}",
        "essence_parameters": {
            "frame_width": 1920,
            "frame_height": 1080,
            "frame_rate": {"numerator": 25, "denominator": 1},
        },
    }

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
            json={"object_ids": [object_id], "storage_id": default_backend["id"]},
            expected=201,
        )
        media_object = allocated["media_objects"][0]
        assert media_object["object_id"] == object_id
        put_request = media_object["put_url"]

        e2e_client.upload_put_url(
            put_request["url"],
            body=b"tamoss deployed e2e segment\n",
            headers=put_request.get("headers") or {},
        )

        segment = e2e_client.request(
            "POST",
            f"/flows/{flow_id}/segments",
            json={"object_id": object_id, "timerange": "[0:0_10:0)"},
            expected=201,
        )
        assert segment.status_code == 201

        segments = e2e_client.request(
            "GET",
            f"/flows/{flow_id}/segments",
            params={
                "limit": "1",
                "accept_get_urls": default_backend["label"],
                "accept_storage_ids": default_backend["id"],
                "presigned": "true",
                "verbose_storage": "true",
            },
        )
        assert segments.headers["x-paging-count"] == "1"
        assert segments.json()[0]["get_urls"][0]["storage_id"] == default_backend["id"]

        media = e2e_client.request_json(
            "GET",
            f"/objects/{object_id}",
            params={
                "accept_get_urls": default_backend["label"],
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
    finally:
        if not deleted:
            cleanup = e2e_client.request(
                "DELETE", f"/flows/{flow_id}", expected={202, 204, 404}
            )
            if cleanup.status_code == 202:
                e2e_client.poll_delete_request(cleanup.json()["id"])


def test_deployed_rejects_duplicate_controlled_object_instance(
    e2e_client: E2EClient,
) -> None:
    backends = e2e_client.request_json("GET", "/service/storage-backends")
    assert len(backends) == 1
    primary = next((item for item in backends if item["default_storage"]), backends[0])

    flow_id = str(uuid4())
    source_id = str(uuid4())
    object_id = f"bbc/e2e/single-backend/{uuid4()}.ts"
    body = b"tamoss deployed single-backend segment\n"

    deleted = False
    try:
        e2e_client.request_json(
            "PUT",
            f"/flows/{flow_id}",
            json={
                "id": flow_id,
                "source_id": source_id,
                "format": "urn:x-nmos:format:video",
                "codec": "video/h264",
                "container": "video/mp2t",
                "label": f"TAMOSS storage E2E {flow_id[:8]}",
                "essence_parameters": {
                    "frame_width": 1920,
                    "frame_height": 1080,
                    "frame_rate": {"numerator": 25, "denominator": 1},
                },
            },
            expected=201,
        )
        allocated = e2e_client.request_json(
            "POST",
            f"/flows/{flow_id}/storage",
            json={"object_ids": [object_id], "storage_id": primary["id"]},
            expected=201,
        )
        put_request = allocated["media_objects"][0]["put_url"]
        e2e_client.upload_put_url(
            put_request["url"],
            body=body,
            headers=put_request.get("headers") or {},
        )
        e2e_client.request(
            "POST",
            f"/flows/{flow_id}/segments",
            json={"object_id": object_id, "timerange": "[0:0_10:0)"},
            expected=201,
        )

        duplicate = e2e_client.request(
            "POST",
            f"/objects/{object_id}/instances",
            json={"storage_id": primary["id"]},
            expected=400,
        )
        duplicate_error = duplicate.json()
        assert duplicate_error["type"] == "bad_request"
        assert duplicate_error["summary"]

        media_object = e2e_client.request_json(
            "GET",
            f"/objects/{object_id}",
            params={
                "accept_storage_ids": primary["id"],
                "presigned": "true",
                "verbose_storage": "true",
            },
        )
        get_urls = media_object["get_urls"]
        assert len(get_urls) == 1
        assert get_urls[0]["storage_id"] == primary["id"]
        assert get_urls[0]["controlled"] is True
        assert get_urls[0]["presigned"] is True

        accepted = e2e_client.request_json(
            "DELETE", f"/flows/{flow_id}", expected={202}
        )
        delete_result = e2e_client.poll_delete_request(accepted["id"])
        assert delete_result["status"] == "done"
        deleted = True
    finally:
        if not deleted:
            cleanup = e2e_client.request(
                "DELETE", f"/flows/{flow_id}", expected={202, 204, 404}
            )
            if cleanup.status_code == 202:
                e2e_client.poll_delete_request(cleanup.json()["id"])
