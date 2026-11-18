from __future__ import annotations

from copy import deepcopy
from uuid import uuid4

import pytest
from fastapi.testclient import TestClient
from tamoss.adapters.postgres import PostgresRepository
from tamoss.app import create_app
from tamoss.application.use_cases import TamossUseCases

from tests.adapters.postgres.support import primary_backend
from tests.support.object_storage import InMemoryObjectStorage
from tests.support.settings import bbc_parity_settings
from tests.tams.support import video_flow_payload

pytestmark = pytest.mark.needs_db


@pytest.mark.parametrize("allocated", [False, True])
def test_rejected_registration_cannot_change_later_batch_entries(
    postgres_repo: PostgresRepository, allocated: bool
) -> None:
    settings = bbc_parity_settings()
    storage = InMemoryObjectStorage()
    use_cases = TamossUseCases(
        repository=postgres_repo, object_storage=storage, settings=settings
    )
    with TestClient(create_app(settings, use_cases=use_cases)) as client:
        flow_id = uuid4()
        payload = video_flow_payload(flow_id, uuid4(), container="video/iso.segment")
        payload["essence_parameters"]["init_segments"] = True
        assert client.put(f"/flows/{flow_id}", json=payload).status_code == 201
        for object_id in ["init", *(["media"] if allocated else [])]:
            assert (
                client.post(
                    f"/flows/{flow_id}/storage", json={"object_ids": [object_id]}
                ).status_code
                == 201
            )
            storage.write(object_id, b"uploaded", backend=primary_backend())
        before = deepcopy(
            postgres_repo.object_repository.get_objects(["media", "init"])
        )
        invalid = {
            "object_id": "media",
            "timerange": "[0:0_1:0)",
            "get_urls": [
                {"url": "https://media.example/rejected.m4s", "label": "external"}
            ],
        }
        reuse = {
            "object_id": "media",
            "timerange": "[1:0_2:0)",
            "init_object_id": "init",
        }
        # A controlled allocation also fails after resolving its init Object.
        if allocated:
            invalid.update(init_object_id="init", object_timerange="[10:0_11:0)")
            reuse["object_timerange"] = "[10:0_11:0)"
        response = client.post(f"/flows/{flow_id}/segments", json=[invalid, reuse])
        assert response.status_code == 200
        assert len(response.json()["failed_segments"]) == 2
        assert client.get(f"/flows/{flow_id}/segments").json() == []
        assert postgres_repo.object_repository.get_objects(["media", "init"]) == before

        valid = {
            "object_id": "media",
            "timerange": "[0:0_1:0)",
            "init_object_id": "init",
            "get_urls": [
                {"url": "https://media.example/accepted.m4s", "label": "external"}
            ],
        }
        response = client.post(
            f"/flows/{flow_id}/segments",
            json=[
                valid,
                {"object_id": "media", "timerange": "[1:0_2:0)", "ts_offset": "1:0"},
            ],
        )
        assert response.status_code == 201, response.text
        segments = client.get(f"/flows/{flow_id}/segments").json()
        assert len(segments) == 2
        assert all(
            segment["init_object"]["object_id"] == "init" for segment in segments
        )
        media = postgres_repo.object_repository.get_object("media")
        assert media is not None
        assert media.referenced_by_flows == {flow_id}
        assert [
            instance.url for instance in media.instances if not instance.controlled
        ] == ["https://media.example/accepted.m4s"]
