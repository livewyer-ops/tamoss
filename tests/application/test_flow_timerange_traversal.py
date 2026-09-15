from collections import Counter
from uuid import UUID, uuid4

import pytest
from fastapi.testclient import TestClient
from tamoss.application.contexts import flows
from tamoss.domain.model import FlowRecord

from tests.tams.support import (
    flow_collection_item,
    multi_flow_payload,
    register_segment,
    video_flow_payload,
)


def test_timerange_read_expands_shared_descendants_once(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    depth = 8
    ids = [uuid4() for _ in range(1 + 2 * depth)]
    for index in reversed(range(len(ids))):
        level = 0 if index == 0 else 1 + (index - 1) // 2
        children = ids[1 + 2 * level : 1 + 2 * (level + 1)]
        payload = (
            video_flow_payload(ids[index], uuid4())
            if index == len(ids) - 1
            else multi_flow_payload(
                ids[index],
                uuid4(),
                flow_collection=[flow_collection_item(child) for child in children],
            )
        )
        assert client.put(f"/flows/{ids[index]}", json=payload).status_code == 201
    register_segment(client, ids[-1])
    assert (
        client.put(
            f"/flows/{ids[-2]}/flow_collection",
            json=[flow_collection_item(ids[0])],
        ).status_code
        == 204
    )

    expansions: Counter[UUID] = Counter()
    original = flows.flow_collection

    def counted_collection(flow: FlowRecord) -> list[dict]:
        expansions[flow.id] += 1
        return original(flow)

    monkeypatch.setattr(flows, "flow_collection", counted_collection)
    response = client.get(f"/flows/{ids[0]}", params={"include_timerange": "true"})

    assert response.status_code == 200
    assert response.json()["timerange"] == "[0:0_10:0)"
    assert expansions == Counter(dict.fromkeys(ids, 1))
