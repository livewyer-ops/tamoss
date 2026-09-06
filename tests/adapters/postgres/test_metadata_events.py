from __future__ import annotations

import time
from concurrent.futures import ThreadPoolExecutor
from contextlib import ExitStack
from threading import Event
from uuid import uuid4

import psycopg
import pytest
from fastapi.testclient import TestClient
from psycopg import sql
from tamoss.adapters.postgres import PostgresRepository
from tamoss.app import create_app

from tests.adapters.postgres.support import database_url, primary_backend, use_cases
from tests.tams.support import video_flow_payload, webhook_payload

pytestmark = pytest.mark.needs_db


@pytest.mark.parametrize(
    ("resource", "method", "property_path", "value"),
    [
        ("flows", "PUT", "label", "After"),
        ("flows", "DELETE", "label", None),
        ("flows", "PUT", "description", "After"),
        ("flows", "PUT", "avg_bit_rate", 1000),
        ("flows", "PUT", "max_bit_rate", 2000),
        ("flows", "PUT", "read_only", True),
        ("flows", "PUT", "tags/review", "After"),
        ("flows", "DELETE", "tags/review", None),
        ("flows", "PUT", "flow_collection", []),
        ("sources", "PUT", "label", "After"),
        ("sources", "DELETE", "label", None),
        ("sources", "PUT", "description", "After"),
        ("sources", "PUT", "tags/review", "After"),
        ("sources", "DELETE", "tags/review", None),
    ],
)
def test_metadata_and_all_notifications_roll_back_together(
    postgres_repo: PostgresRepository,
    postgres_connection: psycopg.Connection,
    monkeypatch: pytest.MonkeyPatch,
    resource: str,
    method: str,
    property_path: str,
    value: object,
) -> None:
    cases = use_cases(postgres_repo)
    hooks = postgres_repo.webhook_repository
    with TestClient(
        create_app(use_cases=cases), raise_server_exceptions=False
    ) as client:
        flow_id, source_id = uuid4(), uuid4()
        assert (
            client.put(
                f"/flows/{flow_id}",
                json=video_flow_payload(flow_id, source_id, label="Before"),
            ).status_code
            == 201
        )
        path = f"/{resource}/{flow_id if resource == 'flows' else source_id}"
        assert client.put(path + "/tags/review", json="Before").status_code == 204
        before = client.get(path).json()
        for _ in range(2):
            assert (
                client.post(
                    "/service/webhooks",
                    json=webhook_payload(events=[f"{resource}/updated"]),
                ).status_code
                == 201
            )
        insert = hooks.save_webhook_delivery
        inserted = 0

        def fail_second_insert(delivery):
            nonlocal inserted
            inserted += 1
            if inserted == 2:
                postgres_connection.execute("SELECT 1 / 0")
            insert(delivery)

        with monkeypatch.context() as patch:
            patch.setattr(hooks, "save_webhook_delivery", fail_second_insert)
            response = client.request(method, path + "/" + property_path, json=value)
        assert response.status_code == 500
        assert inserted == 2
        assert client.get(path).json() == before
        assert hooks.list_webhook_deliveries() == []
        assert all(hook.status == "created" for hook in hooks.list_webhooks())

        retry = client.request(method, path + "/" + property_path, json=value)
        assert retry.status_code == 204
        assert len(hooks.list_webhook_deliveries()) == 2


@pytest.mark.parametrize("resource", ["flow", "source"])
def test_concurrent_metadata_edits_lock_before_reading_and_preserve_both_events(
    postgres_repo: PostgresRepository,
    postgres_connection: psycopg.Connection,
    monkeypatch: pytest.MonkeyPatch,
    resource: str,
) -> None:
    cases = use_cases(postgres_repo)
    flow_id, source_id = uuid4(), uuid4()
    with TestClient(create_app(use_cases=cases)) as client:
        assert (
            client.put(
                f"/flows/{flow_id}", json=video_flow_payload(flow_id, source_id)
            ).status_code
            == 201
        )
        assert (
            client.post(
                "/service/webhooks",
                json=webhook_payload(events=[f"{resource}s/updated"]),
            ).status_code
            == 201
        )

    schema = postgres_connection.execute("SELECT current_schema()").fetchone()[0]
    with ExitStack() as stack:
        connections = [
            stack.enter_context(psycopg.connect(database_url(), autocommit=True))
            for _ in range(2)
        ]
        for connection in connections:
            connection.execute(
                sql.SQL("SET search_path TO {}").format(sql.Identifier(schema))
            )
        repos = [
            PostgresRepository(connection=connection, storage_backend=primary_backend())
            for connection in connections
        ]
        first = getattr(use_cases(repos[0]), resource + "s")
        second = getattr(use_cases(repos[1]), resource + "s")
        repository = getattr(repos[0], resource + "_repository")
        get_record = getattr(repository, "get_" + resource)
        entered, release = Event(), Event()

        def hold_first_read(identity):
            record = get_record(identity)
            if not entered.is_set():
                entered.set()
                assert release.wait(10)
            return record

        monkeypatch.setattr(repository, "get_" + resource, hold_first_read)
        identity = flow_id if resource == "flow" else source_id
        with ThreadPoolExecutor(max_workers=2) as executor:
            writer = executor.submit(
                getattr(first, f"set_{resource}_tag"), identity, "first", "one"
            )
            try:
                assert entered.wait(5)
                waiter = executor.submit(
                    getattr(second, f"set_{resource}_tag"), identity, "second", "two"
                )
                deadline = time.monotonic() + 5
                blocked = False
                while time.monotonic() < deadline:
                    blocked = postgres_connection.execute(
                        "SELECT %s = ANY(pg_blocking_pids(%s))",
                        (
                            connections[0].info.backend_pid,
                            connections[1].info.backend_pid,
                        ),
                    ).fetchone()[0]
                    if blocked:
                        break
                    time.sleep(0.01)
                assert blocked, "Second writer did not wait for the first writer"
            finally:
                release.set()
            writer.result(timeout=5)
            waiter.result(timeout=5)

        record = getattr(getattr(cases, resource + "s"), "get_" + resource)(identity)
        assert record.tags == {"first": "one", "second": "two"}
        deliveries = postgres_repo.webhook_repository.list_webhook_deliveries()
        assert len(deliveries) == 2
        tags = [delivery.payload["event"][resource]["tags"] for delivery in deliveries]
        assert {"first": "one"} in tags
        assert {"first": "one", "second": "two"} in tags
