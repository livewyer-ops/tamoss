from __future__ import annotations

import pytest
from fastapi.testclient import TestClient

from tests.tams.support import create_video_flow, register_segment

pytestmark = pytest.mark.tams_conformance


def test_segments_sort_exclusive_range_before_adjacent_point_and_reverse(
    client: TestClient,
) -> None:
    flow_id, _, _ = create_video_flow(client)
    range_object_id = register_segment(
        client,
        flow_id,
        timerange="[1:0_2:0)",
    )
    point_object_id = register_segment(
        client,
        flow_id,
        timerange="[2:0]",
    )

    forward = client.get(f"/flows/{flow_id}/segments")
    reverse = client.get(
        f"/flows/{flow_id}/segments",
        params={"reverse_order": "true"},
    )

    assert forward.status_code == 200
    assert reverse.status_code == 200
    assert [item["object_id"] for item in forward.json()] == [
        range_object_id,
        point_object_id,
    ]
    assert [item["object_id"] for item in reverse.json()] == [
        point_object_id,
        range_object_id,
    ]
    assert forward.headers["x-paging-reverse-order"] == "false"
    assert reverse.headers["x-paging-reverse-order"] == "true"
