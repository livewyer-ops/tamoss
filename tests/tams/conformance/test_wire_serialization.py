from uuid import uuid4

import pytest
from fastapi.testclient import TestClient

from tests.support.bbc_contract import response_validator
from tests.tams.support import create_video_flow

pytestmark = [pytest.mark.tams_conformance, pytest.mark.tams_contract]


def test_storage_response_matches_unmodified_bbc_schema(client: TestClient) -> None:
    flow_id, _, _ = create_video_flow(client)
    response = client.post(
        f"/flows/{flow_id}/storage", json={"object_ids": [f"wire/{uuid4()}.ts"]}
    )
    assert response.status_code == 201
    response_validator("/flows/{flowId}/storage", "post", 201).validate(response.json())
    assert "body" not in response.json()["media_objects"][0]["put_url"]


def test_response_models_preserve_explicit_null_extensions(
    client: TestClient, monkeypatch: pytest.MonkeyPatch
) -> None:
    flow_id, _, _ = create_video_flow(client)
    use_cases = client.app.state.tamoss_use_cases
    allocate = use_cases.storage.allocate_flow_storage

    def extended_allocation(**kwargs):
        allocations = allocate(**kwargs)
        allocations[0]["put_url"]["x-optional"] = None
        return allocations

    monkeypatch.setattr(use_cases.storage, "allocate_flow_storage", extended_allocation)
    response = client.post(f"/flows/{flow_id}/storage", json={"limit": 1})
    assert response.json()["media_objects"][0]["put_url"]["x-optional"] is None
    monkeypatch.setattr(
        use_cases.flows,
        "get_flow_collection",
        lambda _: [{"id": str(uuid4()), "x-optional": None}],
    )
    collection = client.get(f"/flows/{flow_id}/flow_collection")
    assert "role" not in collection.json()[0]
    assert collection.json()[0]["x-optional"] is None
