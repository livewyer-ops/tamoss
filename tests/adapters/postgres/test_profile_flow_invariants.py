from __future__ import annotations

import time
from collections.abc import Iterator
from concurrent.futures import ThreadPoolExecutor
from threading import Event
from typing import Any, cast
from uuid import UUID, uuid4

import psycopg
import pytest
from psycopg import sql
from tamoss.adapters.postgres import PostgresRepository
from tamoss.domain.model import FlowRecord, MediaObjectRecord, SegmentRecord
from tamoss.errors import BadRequest

from tests.adapters.postgres.support import (
    database_url,
    identity,
    primary_backend,
    use_cases,
    video_flow_write,
)

pytestmark = pytest.mark.needs_db

type ConcurrentPostgresRepos = tuple[
    tuple[PostgresRepository, int],
    tuple[PostgresRepository, int],
]


@pytest.fixture()
def concurrent_postgres_repos(
    postgres_connection: psycopg.Connection,
    postgres_repo: PostgresRepository,
) -> Iterator[ConcurrentPostgresRepos]:
    schema = postgres_connection.execute("SELECT current_schema()").fetchone()[0]
    connections: list[psycopg.Connection] = []
    repositories: list[PostgresRepository] = []
    for _ in range(2):
        connection = psycopg.connect(database_url(), connect_timeout=2)
        connection.autocommit = True
        connection.execute(
            sql.SQL("SET search_path TO {}").format(sql.Identifier(schema))
        )
        connections.append(connection)
        repositories.append(
            PostgresRepository(
                connection=connection,
                storage_backend=primary_backend(),
            )
        )

    try:
        yield (
            (repositories[0], connections[0].info.backend_pid),
            (repositories[1], connections[1].info.backend_pid),
        )
    finally:
        for connection in connections:
            connection.close()


def _wait_for_ungranted_advisory_lock(
    connection: psycopg.Connection,
    *,
    waiter_pid: int,
    blocker_pid: int,
    timeout: float = 5.0,
) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        waiting = connection.execute(
            """
            SELECT EXISTS (
                SELECT 1
                FROM pg_locks
                WHERE pid = %s
                  AND locktype = 'advisory'
                  AND granted IS FALSE
            ) AND %s = ANY(pg_blocking_pids(%s))
            """,
            (waiter_pid, blocker_pid, waiter_pid),
        ).fetchone()[0]
        if waiting:
            return
        time.sleep(0.01)
    raise AssertionError(
        f"backend {waiter_pid} did not wait for backend {blocker_pid}'s "
        "Flow advisory lock"
    )


def _init_segments_payload(
    flow_id: UUID,
    source_id: UUID,
    *,
    enabled: bool,
) -> dict[str, Any]:
    payload = cast(dict[str, Any], video_flow_write(flow_id, source_id))
    payload["essence_parameters"]["init_segments"] = enabled
    return payload


def _seed_flow_and_allocated_object(
    postgres_repo: PostgresRepository,
    *,
    flow_id: UUID,
    source_id: UUID,
    object_id: str,
) -> None:
    payload = _init_segments_payload(flow_id, source_id, enabled=False)
    _, created = use_cases(postgres_repo).flows.put_flow(
        flow_id=flow_id,
        flow=payload,
        identity=identity(),
    )
    assert created is True
    postgres_repo.object_repository.save_object(
        MediaObjectRecord(id=object_id, allocated_by_flow=flow_id)
    )


def test_flow_repository_has_segments_reports_persisted_existence(
    postgres_repo: PostgresRepository,
) -> None:
    flow_id = uuid4()
    flow = FlowRecord(
        id=flow_id,
        data={},
        source_id=uuid4(),
        format="urn:x-nmos:format:video",
        container="video/mp2t",
    )
    postgres_repo.flow_repository.save_flow(flow)
    with postgres_repo.flow_repository.unit_of_work():
        postgres_repo.flow_repository.lock_flow_segments(flow_id)
    assert postgres_repo.flow_repository.has_segments(flow_id) is False

    postgres_repo.flow_repository.append_segment(
        SegmentRecord(
            flow_id=flow_id,
            object_id="bbc/profile-invariant.ts",
            timerange="[0:0_10:0)",
        )
    )
    assert postgres_repo.flow_repository.has_segments(flow_id) is True


def test_first_segment_serializes_before_init_segments_change(
    postgres_connection: psycopg.Connection,
    postgres_repo: PostgresRepository,
    concurrent_postgres_repos: ConcurrentPostgresRepos,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    (put_repo, put_pid), (segment_repo, segment_pid) = concurrent_postgres_repos
    put_use_cases = use_cases(put_repo)
    segment_use_cases = use_cases(segment_repo)
    flow_id = uuid4()
    source_id = uuid4()
    object_id = "bbc/profile-invariant/segment-first.ts"
    _seed_flow_and_allocated_object(
        postgres_repo,
        flow_id=flow_id,
        source_id=source_id,
        object_id=object_id,
    )

    segment_has_lock = Event()
    release_segment = Event()
    put_attempted_lock = Event()
    put_has_lock = Event()
    original_segment_lock = segment_repo.segment_repository.lock_flow_segments
    original_put_lock = put_repo.flow_repository.lock_flow_segments

    def hold_segment_lock(requested_flow_id: UUID) -> None:
        original_segment_lock(requested_flow_id)
        segment_has_lock.set()
        if not release_segment.wait(timeout=10):
            raise AssertionError("timed out holding the Segment transaction lock")

    def track_put_lock(requested_flow_id: UUID) -> None:
        put_attempted_lock.set()
        original_put_lock(requested_flow_id)
        put_has_lock.set()

    monkeypatch.setattr(
        segment_repo.segment_repository,
        "lock_flow_segments",
        hold_segment_lock,
    )
    monkeypatch.setattr(
        put_repo.flow_repository,
        "lock_flow_segments",
        track_put_lock,
    )

    with ThreadPoolExecutor(max_workers=2) as executor:
        segment_future = executor.submit(
            segment_use_cases.segments.register_segment,
            flow_id=flow_id,
            segment_post={"object_id": object_id, "timerange": "[0:0_10:0)"},
        )
        assert segment_has_lock.wait(timeout=5)
        put_future = executor.submit(
            put_use_cases.flows.put_flow,
            flow_id=flow_id,
            flow=_init_segments_payload(flow_id, source_id, enabled=True),
            identity=identity(),
        )
        assert put_attempted_lock.wait(timeout=5)
        try:
            _wait_for_ungranted_advisory_lock(
                postgres_connection,
                waiter_pid=put_pid,
                blocker_pid=segment_pid,
            )
            assert put_has_lock.is_set() is False
        finally:
            release_segment.set()

        segment_result = segment_future.result(timeout=5)
        with pytest.raises(BadRequest, match="init_segments cannot change"):
            put_future.result(timeout=5)

    assert segment_result.error is None
    stored_flow = postgres_repo.flow_repository.get_flow(flow_id)
    assert stored_flow is not None
    assert stored_flow.init_segments is False
    assert [
        segment.object_id
        for segment in postgres_repo.segment_repository.list_segments(flow_id)
    ] == [object_id]


def test_init_segments_change_serializes_before_incompatible_first_segment(
    postgres_connection: psycopg.Connection,
    postgres_repo: PostgresRepository,
    concurrent_postgres_repos: ConcurrentPostgresRepos,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    (put_repo, put_pid), (segment_repo, segment_pid) = concurrent_postgres_repos
    put_use_cases = use_cases(put_repo)
    segment_use_cases = use_cases(segment_repo)
    flow_id = uuid4()
    source_id = uuid4()
    object_id = "bbc/profile-invariant/put-first.ts"
    _seed_flow_and_allocated_object(
        postgres_repo,
        flow_id=flow_id,
        source_id=source_id,
        object_id=object_id,
    )

    put_has_lock = Event()
    release_put = Event()
    segment_attempted_lock = Event()
    segment_has_lock = Event()
    original_put_lock = put_repo.flow_repository.lock_flow_segments
    original_segment_lock = segment_repo.segment_repository.lock_flow_segments

    def hold_put_lock(requested_flow_id: UUID) -> None:
        original_put_lock(requested_flow_id)
        put_has_lock.set()
        if not release_put.wait(timeout=10):
            raise AssertionError("timed out holding the Flow PUT transaction lock")

    def track_segment_lock(requested_flow_id: UUID) -> None:
        segment_attempted_lock.set()
        original_segment_lock(requested_flow_id)
        segment_has_lock.set()

    monkeypatch.setattr(
        put_repo.flow_repository,
        "lock_flow_segments",
        hold_put_lock,
    )
    monkeypatch.setattr(
        segment_repo.segment_repository,
        "lock_flow_segments",
        track_segment_lock,
    )

    with ThreadPoolExecutor(max_workers=2) as executor:
        put_future = executor.submit(
            put_use_cases.flows.put_flow,
            flow_id=flow_id,
            flow=_init_segments_payload(flow_id, source_id, enabled=True),
            identity=identity(),
        )
        assert put_has_lock.wait(timeout=5)
        segment_future = executor.submit(
            segment_use_cases.segments.register_segment,
            flow_id=flow_id,
            segment_post={"object_id": object_id, "timerange": "[0:0_10:0)"},
        )
        assert segment_attempted_lock.wait(timeout=5)
        try:
            _wait_for_ungranted_advisory_lock(
                postgres_connection,
                waiter_pid=segment_pid,
                blocker_pid=put_pid,
            )
            assert segment_has_lock.is_set() is False
        finally:
            release_put.set()

        _, created = put_future.result(timeout=5)
        segment_result = segment_future.result(timeout=5)

    assert created is False
    assert (
        segment_result.error == "Bad request. init_object_id is required for this Flow."
    )
    stored_flow = postgres_repo.flow_repository.get_flow(flow_id)
    assert stored_flow is not None
    assert stored_flow.init_segments is True
    assert postgres_repo.segment_repository.list_segments(flow_id) == []
