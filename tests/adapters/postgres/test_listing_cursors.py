from __future__ import annotations

from datetime import UTC, datetime, timedelta
from uuid import UUID

import pytest
from fastapi.testclient import TestClient
from tamoss.adapters.postgres import PostgresRepository
from tamoss.app import create_app
from tamoss.domain.model import FlowRecord, SourceRecord

from tests.adapters.postgres.support import use_cases
from tests.tams.support import video_flow_payload

pytestmark = pytest.mark.needs_db


def seed_listing(repository: PostgresRepository, resource: str) -> None:
    for index, label in enumerate([None, None, "Alpha", "Alpha", "Beta", "Beta"]):
        timestamp = datetime(2026, 9, 1, tzinfo=UTC) + timedelta(days=index // 2)
        identity = UUID(int=index + 1, version=4)
        source_id = UUID(int=100, version=4)
        if resource == "flows":
            repository.flow_repository.save_flow(
                FlowRecord(
                    id=identity,
                    source_id=source_id,
                    format="urn:x-nmos:format:video",
                    container="video/mp4",
                    data={**video_flow_payload(identity, source_id), "label": label},
                    created=timestamp,
                    metadata_updated=timestamp,
                )
            )
        else:
            repository.source_repository.save_source(
                SourceRecord(
                    id=identity,
                    format="urn:x-nmos:format:video",
                    label=label,
                    created=timestamp,
                    metadata_updated=timestamp,
                )
            )


@pytest.mark.parametrize(
    ("resource", "sort_by"),
    [("flows", sort) for sort in ("created", "metadata_updated", "label")]
    + [("sources", sort) for sort in ("created", "updated", "label")],
)
@pytest.mark.parametrize("reverse_order", [False, True])
def test_cursor_survives_deleting_every_preceding_page_including_anchor(
    postgres_repo: PostgresRepository,
    resource: str,
    sort_by: str,
    reverse_order: bool,
) -> None:
    seed_listing(postgres_repo, resource)
    delete = (
        postgres_repo.flow_repository.delete_flow
        if resource == "flows"
        else postgres_repo.source_repository.delete_source
    )
    with TestClient(create_app(use_cases=use_cases(postgres_repo))) as client:
        params = {"sort_by": sort_by, "reverse_order": str(reverse_order).lower()}
        baseline = client.get(f"/{resource}", params=params)
        assert baseline.status_code == 200
        expected = [item["id"] for item in baseline.json()]
        assert len(expected) == 6
        response = client.get(f"/{resource}", params={**params, "limit": 2})
        seen = []
        for _ in range(3):
            assert response.status_code == 200
            page = response.json()
            assert len(page) == 2
            seen.extend(item["id"] for item in page)
            for item in page:
                delete(UUID(item["id"]))
            if "next" not in response.links:
                break
            next_url = response.links["next"]["url"]
            assert "page=k1." in next_url
            response = client.get(next_url)
        assert "next" not in response.links
        assert seen == expected


@pytest.mark.parametrize("resource", ["flows", "sources"])
def test_newer_insert_does_not_repeat_items_and_legacy_offsets_still_work(
    postgres_repo: PostgresRepository, resource: str
) -> None:
    seed_listing(postgres_repo, resource)
    with TestClient(create_app(use_cases=use_cases(postgres_repo))) as client:
        expected = client.get(f"/{resource}").json()
        first = client.get(f"/{resource}", params={"limit": 2})
        assert first.status_code == 200
        assert client.get(f"/{resource}", params={"page": "2"}).json() == expected[2:]
        if resource == "flows":
            record = postgres_repo.flow_repository.get_flow(UUID(int=1, version=4))
            assert record is not None
            record.id = UUID(int=99, version=4)
            record.created += timedelta(days=10)
            postgres_repo.flow_repository.save_flow(record)
        else:
            record = postgres_repo.source_repository.get_source(UUID(int=1, version=4))
            assert record is not None
            record.id = UUID(int=99, version=4)
            record.created += timedelta(days=10)
            postgres_repo.source_repository.save_source(record)
        seen = first.json()
        while "next" in first.links:
            first = client.get(first.links["next"]["url"])
            assert first.status_code == 200
            seen.extend(first.json())
        assert seen == expected


def test_invalid_and_wrong_context_cursors_return_bad_request(
    postgres_repo: PostgresRepository,
) -> None:
    seed_listing(postgres_repo, "flows")
    with TestClient(create_app(use_cases=use_cases(postgres_repo))) as client:
        for page in ("k1.invalid!", "k1.W10", "k1." + "x" * 4097, "-1"):
            assert client.get("/flows", params={"page": page}).status_code == 400
        first = client.get("/flows", params={"limit": 2})
        next_url = first.links["next"]["url"]
        assert client.get(next_url.replace("/flows?", "/sources?")).status_code == 400
        assert client.get(next_url + "&sort_by=label").status_code == 400
        assert client.get(next_url + "&reverse_order=true").status_code == 400
