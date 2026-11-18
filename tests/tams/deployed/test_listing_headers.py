from uuid import uuid4

import pytest

from tests.e2e.client import E2EClient
from tests.tams.support import video_flow_payload

pytestmark = [
    pytest.mark.e2e,
    pytest.mark.tams_conformance,
    pytest.mark.tamoss_extension,
]


def test_long_label_pages_through_ingress(e2e_client: E2EClient) -> None:
    marker = str(uuid4())
    created = []
    source_ids = []
    try:
        for label in ("A" * 6000, "A" * 6000, "é" * 800, "é" * 800, None):
            flow_id, source_id = str(uuid4()), str(uuid4())
            payload = video_flow_payload(
                flow_id, source_id, tags={"listing-header-test": marker}
            )
            if label is None:
                payload.pop("label", None)
            else:
                payload["label"] = label
            e2e_client.request("PUT", f"/flows/{flow_id}", json=payload, expected=201)
            created.append(flow_id)
            source_ids.append(source_id)

        for resource, ids in (("flows", created), ("sources", source_ids)):
            for reverse_order in ("false", "true"):
                params = {
                    "sort_by": "label",
                    "reverse_order": reverse_order,
                    "tag.listing-header-test": marker,
                }
                baseline = e2e_client.request_json("GET", f"/{resource}", params=params)
                assert {item["id"] for item in baseline} == set(ids)
                response = e2e_client.request(
                    "GET", f"/{resource}", params={**params, "limit": "1"}
                )
                seen = []
                for _ in ids:
                    seen.extend(response.json())
                    head = e2e_client.request("HEAD", response.url)
                    for page in (response, head):
                        assert (
                            sum(len(k) + len(v) + 4 for k, v in page.headers.items())
                            < 2048
                        )
                        assert page.headers["x-paging-count"] == "1"
                    if "next" not in response.links:
                        break
                    assert response.headers["x-paging-nextkey"].isdigit()
                    response = e2e_client.request("GET", response.links["next"]["url"])
                assert seen == baseline
                assert "next" not in response.links
    finally:
        for flow_id in created:
            e2e_client.request("DELETE", f"/flows/{flow_id}", expected={204, 404})
