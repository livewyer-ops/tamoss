from __future__ import annotations

import time
from collections.abc import Iterable, Iterator
from concurrent.futures import ThreadPoolExecutor
from threading import Event
from uuid import UUID, uuid4

import psycopg
import pytest
from psycopg import sql
from tamoss.adapters.postgres import PostgresRepository
from tamoss.application.contexts import deletion_processor
from tamoss.domain.model import (
    FlowRecord,
    MediaObjectRecord,
    ObjectCleanupRecord,
    ObjectCopyRecord,
    ObjectInstance,
    StorageBackend,
)
from tamoss.domain.segments import SegmentDeleteFilter

from tests.adapters.postgres.support import (
    database_url,
    primary_backend,
    replacement_backend,
    use_cases,
)
from tests.support.object_storage import InMemoryObjectStorage

pytestmark = pytest.mark.needs_db

type ConcurrentPostgresRepos = tuple[
    tuple[PostgresRepository, int],
    tuple[PostgresRepository, int],
]


@pytest.fixture()
def concurrent_postgres_repos(
    postgres_connection: psycopg.Connection,
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


@pytest.mark.parametrize("shared_kind", ["media", "init"])
def test_concurrent_cross_flow_registration_preserves_shared_object_references(
    shared_kind: str,
    postgres_connection: psycopg.Connection,
    postgres_repo: PostgresRepository,
    concurrent_postgres_repos: ConcurrentPostgresRepos,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    (first_repo, first_pid), (second_repo, second_pid) = concurrent_postgres_repos
    flow_ids = (uuid4(), uuid4())
    shared_object_id = f"bbc/shared-{shared_kind}-{uuid4()}.mxf"
    media_object_ids = (
        shared_object_id if shared_kind == "media" else f"bbc/media-a-{uuid4()}.mxf",
        shared_object_id if shared_kind == "media" else f"bbc/media-b-{uuid4()}.mxf",
    )

    for flow_id in flow_ids:
        postgres_repo.flow_repository.save_flow(
            _flow(flow_id, init_segments=shared_kind == "init")
        )
    for object_id in {shared_object_id, *media_object_ids}:
        postgres_repo.object_repository.save_object(MediaObjectRecord(id=object_id))

    first_locked = Event()
    release_first = Event()
    second_attempted = Event()
    second_locked = Event()
    original_first_lock = first_repo.segment_repository.lock_objects
    original_second_lock = second_repo.segment_repository.lock_objects

    def hold_first_lock(object_ids: set[str]) -> None:
        original_first_lock(object_ids)
        first_locked.set()
        if not release_first.wait(timeout=10):
            raise AssertionError("timed out holding the shared Object lock")

    def track_second_lock(object_ids: set[str]) -> None:
        second_attempted.set()
        original_second_lock(object_ids)
        second_locked.set()

    monkeypatch.setattr(
        first_repo.segment_repository,
        "lock_objects",
        hold_first_lock,
    )
    monkeypatch.setattr(
        second_repo.segment_repository,
        "lock_objects",
        track_second_lock,
    )

    with ThreadPoolExecutor(max_workers=2) as executor:
        first_future = executor.submit(
            use_cases(first_repo).segments.register_segment,
            flow_id=flow_ids[0],
            segment_post=_segment_post(
                object_id=media_object_ids[0],
                init_object_id=(shared_object_id if shared_kind == "init" else None),
            ),
        )
        assert first_locked.wait(timeout=5)

        second_future = executor.submit(
            use_cases(second_repo).segments.register_segment,
            flow_id=flow_ids[1],
            segment_post=_segment_post(
                object_id=media_object_ids[1],
                init_object_id=(shared_object_id if shared_kind == "init" else None),
            ),
        )
        assert second_attempted.wait(timeout=5)
        try:
            _wait_for_ungranted_advisory_lock(
                postgres_connection,
                waiter_pid=second_pid,
                blocker_pid=first_pid,
            )
            assert second_locked.is_set() is False
        finally:
            release_first.set()

        first_result = first_future.result(timeout=5)
        second_result = second_future.result(timeout=5)

    assert first_result.error is None
    assert second_result.error is None
    shared_object = postgres_repo.object_repository.get_object(shared_object_id)
    assert shared_object is not None
    assert shared_object.referenced_by_flows == set(flow_ids)
    assert len(postgres_repo.segment_repository.list_segments(flow_ids[0])) == 1
    assert len(postgres_repo.segment_repository.list_segments(flow_ids[1])) == 1


def test_implicit_init_reuse_locks_the_discovered_shared_object(
    postgres_connection: psycopg.Connection,
    postgres_repo: PostgresRepository,
    concurrent_postgres_repos: ConcurrentPostgresRepos,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    (first_repo, first_pid), (second_repo, second_pid) = concurrent_postgres_repos
    seed_flow_ids = (uuid4(), uuid4())
    reuse_flow_ids = (uuid4(), uuid4())
    media_object_ids = (
        f"bbc/implicit-media-a-{uuid4()}.mxf",
        f"bbc/implicit-media-b-{uuid4()}.mxf",
    )
    shared_init_id = f"bbc/implicit-init-{uuid4()}.mxf"
    for flow_id in (*seed_flow_ids, *reuse_flow_ids):
        postgres_repo.flow_repository.save_flow(_flow(flow_id, init_segments=True))
    for object_id in (*media_object_ids, shared_init_id):
        postgres_repo.object_repository.save_object(MediaObjectRecord(id=object_id))
    for flow_id, media_object_id in zip(seed_flow_ids, media_object_ids, strict=True):
        result = use_cases(postgres_repo).segments.register_segment(
            flow_id=flow_id,
            segment_post=_segment_post(
                object_id=media_object_id,
                init_object_id=shared_init_id,
            ),
        )
        assert result.error is None

    first_locked = Event()
    release_first = Event()
    second_attempted = Event()
    second_locked = Event()
    original_first_lock = first_repo.segment_repository.lock_objects
    original_second_lock = second_repo.segment_repository.lock_objects

    def hold_first_lock(object_ids: Iterable[str]) -> None:
        locked_ids = set(object_ids)
        assert shared_init_id in locked_ids
        original_first_lock(locked_ids)
        first_locked.set()
        if not release_first.wait(timeout=10):
            raise AssertionError("timed out holding the implicit init Object lock")

    def track_second_lock(object_ids: Iterable[str]) -> None:
        locked_ids = set(object_ids)
        assert shared_init_id in locked_ids
        second_attempted.set()
        original_second_lock(locked_ids)
        second_locked.set()

    monkeypatch.setattr(
        first_repo.segment_repository,
        "lock_objects",
        hold_first_lock,
    )
    monkeypatch.setattr(
        second_repo.segment_repository,
        "lock_objects",
        track_second_lock,
    )

    with ThreadPoolExecutor(max_workers=2) as executor:
        first_future = executor.submit(
            use_cases(first_repo).segments.register_segment,
            flow_id=reuse_flow_ids[0],
            segment_post=_segment_post(
                object_id=media_object_ids[0],
                init_object_id=None,
            ),
        )
        assert first_locked.wait(timeout=5)

        second_future = executor.submit(
            use_cases(second_repo).segments.register_segment,
            flow_id=reuse_flow_ids[1],
            segment_post=_segment_post(
                object_id=media_object_ids[1],
                init_object_id=None,
            ),
        )
        assert second_attempted.wait(timeout=5)
        try:
            _wait_for_ungranted_advisory_lock(
                postgres_connection,
                waiter_pid=second_pid,
                blocker_pid=first_pid,
            )
            assert second_locked.is_set() is False
        finally:
            release_first.set()

        first_result = first_future.result(timeout=5)
        second_result = second_future.result(timeout=5)

    assert first_result.error is None
    assert second_result.error is None
    shared_init = postgres_repo.object_repository.get_object(shared_init_id)
    assert shared_init is not None
    assert shared_init.referenced_by_flows == {
        *seed_flow_ids,
        *reuse_flow_ids,
    }


@pytest.mark.parametrize(
    ("initial_uses_init", "reuse_uses_init"),
    [(True, False), (False, True)],
)
def test_referenced_media_cannot_change_its_init_link_on_cross_flow_reuse(
    initial_uses_init: bool,
    reuse_uses_init: bool,
    postgres_repo: PostgresRepository,
) -> None:
    initial_flow_id = uuid4()
    reuse_flow_id = uuid4()
    media_object_id = f"bbc/immutable-init-link-media-{uuid4()}.mxf"
    init_object_id = f"bbc/immutable-init-link-init-{uuid4()}.mxf"
    for flow_id, init_segments in (
        (initial_flow_id, initial_uses_init),
        (reuse_flow_id, reuse_uses_init),
    ):
        postgres_repo.flow_repository.save_flow(
            _flow(flow_id, init_segments=init_segments)
        )
    for object_id in (media_object_id, init_object_id):
        postgres_repo.object_repository.save_object(MediaObjectRecord(id=object_id))

    initial_result = use_cases(postgres_repo).segments.register_segment(
        flow_id=initial_flow_id,
        segment_post=_segment_post(
            object_id=media_object_id,
            init_object_id=init_object_id if initial_uses_init else None,
        ),
    )
    assert initial_result.error is None

    reuse_result = use_cases(postgres_repo).segments.register_segment(
        flow_id=reuse_flow_id,
        segment_post=_segment_post(
            object_id=media_object_id,
            init_object_id=init_object_id if reuse_uses_init else None,
        ),
    )

    assert reuse_result.error is not None
    stored_media = postgres_repo.object_repository.get_object(media_object_id)
    assert stored_media is not None
    assert stored_media.referenced_by_flows == {initial_flow_id}
    assert stored_media.init_object_id == (
        init_object_id if initial_uses_init else None
    )
    assert postgres_repo.segment_repository.list_segments(reuse_flow_id) == []


@pytest.mark.parametrize("shared_kind", ["media", "init"])
def test_registration_and_cross_flow_deletion_preserve_the_retained_reference(
    shared_kind: str,
    postgres_connection: psycopg.Connection,
    postgres_repo: PostgresRepository,
    concurrent_postgres_repos: ConcurrentPostgresRepos,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    (registration_repo, registration_pid), (deletion_repo, deletion_pid) = (
        concurrent_postgres_repos
    )
    deleting_flow_id = uuid4()
    retained_flow_id = uuid4()
    shared_object_id = f"bbc/delete-shared-{shared_kind}-{uuid4()}.mxf"
    deleting_media_id = (
        shared_object_id
        if shared_kind == "media"
        else f"bbc/delete-media-{uuid4()}.mxf"
    )
    retained_media_id = (
        shared_object_id
        if shared_kind == "media"
        else f"bbc/retain-media-{uuid4()}.mxf"
    )

    for flow_id in (deleting_flow_id, retained_flow_id):
        postgres_repo.flow_repository.save_flow(
            _flow(flow_id, init_segments=shared_kind == "init")
        )
    for object_id in {
        shared_object_id,
        deleting_media_id,
        retained_media_id,
    }:
        postgres_repo.object_repository.save_object(MediaObjectRecord(id=object_id))

    initial_result = use_cases(postgres_repo).segments.register_segment(
        flow_id=deleting_flow_id,
        segment_post=_segment_post(
            object_id=deleting_media_id,
            init_object_id=(shared_object_id if shared_kind == "init" else None),
        ),
    )
    assert initial_result.error is None

    registration_locked = Event()
    release_registration = Event()
    deletion_attempted = Event()
    deletion_locked = Event()
    original_registration_lock = registration_repo.segment_repository.lock_objects
    original_deletion_lock = deletion_repo.deletion_repository.lock_objects

    def hold_registration_lock(object_ids: Iterable[str]) -> None:
        original_registration_lock(object_ids)
        registration_locked.set()
        if not release_registration.wait(timeout=10):
            raise AssertionError("timed out holding the registration Object lock")

    def track_deletion_lock(object_ids: Iterable[str]) -> None:
        deletion_attempted.set()
        original_deletion_lock(object_ids)
        deletion_locked.set()

    monkeypatch.setattr(
        registration_repo.segment_repository,
        "lock_objects",
        hold_registration_lock,
    )
    monkeypatch.setattr(
        deletion_repo.deletion_repository,
        "lock_objects",
        track_deletion_lock,
    )

    def delete_initial_segment() -> str:
        with deletion_repo.deletion_repository.unit_of_work():
            return deletion_processor.delete_matching_segments(
                repository=deletion_repo.deletion_repository,
                webhook_repository=deletion_repo.webhook_repository,
                delete_filter=SegmentDeleteFilter(flow_id=deleting_flow_id),
                publish_event=False,
                drain=True,
            )

    with ThreadPoolExecutor(max_workers=2) as executor:
        registration_future = executor.submit(
            use_cases(registration_repo).segments.register_segment,
            flow_id=retained_flow_id,
            segment_post=_segment_post(
                object_id=retained_media_id,
                init_object_id=(shared_object_id if shared_kind == "init" else None),
            ),
        )
        assert registration_locked.wait(timeout=5)

        deletion_future = executor.submit(delete_initial_segment)
        assert deletion_attempted.wait(timeout=5)
        try:
            _wait_for_ungranted_advisory_lock(
                postgres_connection,
                waiter_pid=deletion_pid,
                blocker_pid=registration_pid,
            )
            assert deletion_locked.is_set() is False
        finally:
            release_registration.set()

        registration_result = registration_future.result(timeout=5)
        remaining_timerange = deletion_future.result(timeout=5)

    assert registration_result.error is None
    assert remaining_timerange == "()"
    shared_object = postgres_repo.object_repository.get_object(shared_object_id)
    assert shared_object is not None
    assert shared_object.referenced_by_flows == {retained_flow_id}
    assert postgres_repo.segment_repository.list_segments(deleting_flow_id) == []
    assert len(postgres_repo.segment_repository.list_segments(retained_flow_id)) == 1


def test_cleanup_finishes_before_an_object_id_can_be_reallocated(
    postgres_connection: psycopg.Connection,
    postgres_repo: PostgresRepository,
    concurrent_postgres_repos: ConcurrentPostgresRepos,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    (cleanup_repo, cleanup_pid), (allocation_repo, allocation_pid) = (
        concurrent_postgres_repos
    )
    flow_id = uuid4()
    object_id = f"bbc/reallocation-{uuid4()}.mxf"
    backend = primary_backend()
    storage = InMemoryObjectStorage()
    storage.write(object_id, b"old", backend=backend)
    postgres_repo.flow_repository.save_flow(_flow(flow_id, init_segments=False))
    postgres_repo.deletion_repository.save_object_cleanup(
        ObjectCleanupRecord(
            id=uuid4(),
            object_id=object_id,
            storage_backend_id=backend.id,
            status="pending",
        )
    )
    deleting = Event()
    release_cleanup = Event()
    allocation_attempted = Event()
    delete_batch = storage.delete_batch
    lock_objects = allocation_repo.storage_repository.lock_objects

    def hold_delete(
        object_ids: Iterable[str], *, backend: StorageBackend | None = None
    ) -> None:
        deleting.set()
        assert release_cleanup.wait(timeout=10)
        delete_batch(object_ids, backend=backend)

    def track_allocation(object_ids: Iterable[str]) -> None:
        allocation_attempted.set()
        lock_objects(object_ids)

    def reallocate_and_upload() -> None:
        use_cases(
            allocation_repo, object_storage=storage
        ).storage.allocate_flow_storage(
            flow_id=flow_id, request={"object_ids": [object_id]}
        )
        storage.write(object_id, b"new", backend=backend)

    monkeypatch.setattr(storage, "delete_batch", hold_delete)
    monkeypatch.setattr(
        allocation_repo.storage_repository, "lock_objects", track_allocation
    )
    with ThreadPoolExecutor(max_workers=2) as executor:
        cleanup = executor.submit(
            use_cases(
                cleanup_repo, object_storage=storage
            ).deletion.process_pending_object_cleanups,
            worker_id="cleanup",
            lease_seconds=60,
        )
        assert deleting.wait(timeout=5)
        allocation = executor.submit(reallocate_and_upload)
        try:
            assert allocation_attempted.wait(timeout=5)
            _wait_for_ungranted_advisory_lock(
                postgres_connection, waiter_pid=allocation_pid, blocker_pid=cleanup_pid
            )
        finally:
            release_cleanup.set()
        assert cleanup.result(timeout=5) == 1
        allocation.result(timeout=5)
    assert storage.read(object_id, backend=backend) == b"new"


def test_segment_deletion_waits_for_an_object_copy_to_be_advertised(
    postgres_connection: psycopg.Connection,
    postgres_repo: PostgresRepository,
    concurrent_postgres_repos: ConcurrentPostgresRepos,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    (copy_repo, copy_pid), (deletion_repo, deletion_pid) = concurrent_postgres_repos
    flow_id = uuid4()
    object_id = f"bbc/copy-{uuid4()}.mxf"
    source, destination = primary_backend(), replacement_backend()
    storage = InMemoryObjectStorage()
    storage.write(object_id, b"media", backend=source)
    PostgresRepository(
        connection=postgres_connection,
        storage_backend=destination,
        register_storage_backend=True,
    )
    postgres_repo.flow_repository.save_flow(_flow(flow_id, init_segments=False))
    postgres_repo.object_repository.save_object(
        MediaObjectRecord(
            id=object_id,
            instances=[
                ObjectInstance(
                    storage_backend=source,
                    url=None,
                    label=source.label,
                    controlled=True,
                )
            ],
        )
    )
    assert (
        use_cases(postgres_repo, object_storage=storage)
        .segments.register_segment(
            flow_id=flow_id,
            segment_post=_segment_post(object_id=object_id, init_object_id=None),
        )
        .error
        is None
    )
    postgres_repo.object_repository.save_object_copy(
        ObjectCopyRecord(
            id=uuid4(),
            object_id=object_id,
            source_storage_backend_id=source.id,
            destination_storage_backend_id=destination.id,
            status="pending",
        )
    )
    copying = Event()
    release_copy = Event()
    copy_object = storage.copy

    def hold_copy(
        object_id: str,
        *,
        source_backend: StorageBackend,
        destination_backend: StorageBackend,
    ) -> None:
        copying.set()
        assert release_copy.wait(timeout=10)
        copy_object(
            object_id,
            source_backend=source_backend,
            destination_backend=destination_backend,
        )

    def delete_segments() -> str:
        with deletion_repo.deletion_repository.unit_of_work():
            return deletion_processor.delete_matching_segments(
                repository=deletion_repo.deletion_repository,
                webhook_repository=deletion_repo.webhook_repository,
                delete_filter=SegmentDeleteFilter(flow_id=flow_id),
                publish_event=False,
            )

    monkeypatch.setattr(storage, "copy", hold_copy)
    with ThreadPoolExecutor(max_workers=2) as executor:
        copy = executor.submit(
            use_cases(
                copy_repo, object_storage=storage
            ).objects.process_pending_object_copies,
            worker_id="copy",
            lease_seconds=60,
        )
        assert copying.wait(timeout=5)
        deletion = executor.submit(delete_segments)
        try:
            _wait_for_ungranted_advisory_lock(
                postgres_connection, waiter_pid=deletion_pid, blocker_pid=copy_pid
            )
        finally:
            release_copy.set()
        assert copy.result(timeout=5) == 1
        assert deletion.result(timeout=5) == "()"
    use_cases(
        postgres_repo, object_storage=storage
    ).deletion.process_pending_object_cleanups(
        worker_id="cleanup",
        lease_seconds=60,
    )
    assert storage.read(object_id, backend=source) is None
    assert storage.read(object_id, backend=destination) is None


def test_object_instance_writer_reloads_references_after_registration_lock(
    postgres_connection: psycopg.Connection,
    postgres_repo: PostgresRepository,
    concurrent_postgres_repos: ConcurrentPostgresRepos,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    (registration_repo, registration_pid), (instance_repo, instance_pid) = (
        concurrent_postgres_repos
    )
    initial_flow_id = uuid4()
    registering_flow_id = uuid4()
    shared_object_id = f"bbc/instance-shared-{uuid4()}.mxf"
    for flow_id in (initial_flow_id, registering_flow_id):
        postgres_repo.flow_repository.save_flow(_flow(flow_id, init_segments=False))
    postgres_repo.object_repository.save_object(MediaObjectRecord(id=shared_object_id))
    initial_result = use_cases(postgres_repo).segments.register_segment(
        flow_id=initial_flow_id,
        segment_post=_segment_post(
            object_id=shared_object_id,
            init_object_id=None,
        ),
    )
    assert initial_result.error is None

    registration_locked = Event()
    release_registration = Event()
    instance_attempted = Event()
    instance_locked = Event()
    original_registration_lock = registration_repo.segment_repository.lock_objects
    original_instance_lock = instance_repo.object_repository.lock_objects

    def hold_registration_lock(object_ids: Iterable[str]) -> None:
        original_registration_lock(object_ids)
        registration_locked.set()
        if not release_registration.wait(timeout=10):
            raise AssertionError("timed out holding the registration Object lock")

    def track_instance_lock(object_ids: Iterable[str]) -> None:
        instance_attempted.set()
        original_instance_lock(object_ids)
        instance_locked.set()

    monkeypatch.setattr(
        registration_repo.segment_repository,
        "lock_objects",
        hold_registration_lock,
    )
    monkeypatch.setattr(
        instance_repo.object_repository,
        "lock_objects",
        track_instance_lock,
    )

    with ThreadPoolExecutor(max_workers=2) as executor:
        registration_future = executor.submit(
            use_cases(registration_repo).segments.register_segment,
            flow_id=registering_flow_id,
            segment_post=_segment_post(
                object_id=shared_object_id,
                init_object_id=None,
            ),
        )
        assert registration_locked.wait(timeout=5)

        instance_future = executor.submit(
            use_cases(instance_repo).objects.register_object_instance,
            object_id=shared_object_id,
            registration={
                "url": "https://external.example.test/shared.mxf",
                "label": "external-origin",
            },
        )
        assert instance_attempted.wait(timeout=5)
        try:
            _wait_for_ungranted_advisory_lock(
                postgres_connection,
                waiter_pid=instance_pid,
                blocker_pid=registration_pid,
            )
            assert instance_locked.is_set() is False
        finally:
            release_registration.set()

        registration_result = registration_future.result(timeout=5)
        instance_future.result(timeout=5)

    assert registration_result.error is None
    shared_object = postgres_repo.object_repository.get_object(shared_object_id)
    assert shared_object is not None
    assert shared_object.referenced_by_flows == {
        initial_flow_id,
        registering_flow_id,
    }
    assert any(
        instance.label == "external-origin" for instance in shared_object.instances
    )


def _flow(flow_id: UUID, *, init_segments: bool) -> FlowRecord:
    return FlowRecord(
        id=flow_id,
        data={
            "id": str(flow_id),
            "format": "urn:x-nmos:format:video",
            "codec": "video/h264",
            "container": "video/mp2t",
            "essence_parameters": {"init_segments": init_segments},
        },
        source_id=None,
        format="urn:x-nmos:format:video",
        container="video/mp2t",
        init_segments=init_segments,
    )


def _segment_post(*, object_id: str, init_object_id: str | None) -> dict[str, object]:
    payload: dict[str, object] = {
        "object_id": object_id,
        "timerange": "[0:0_10:0)",
    }
    if init_object_id is not None:
        payload["init_object_id"] = init_object_id
    return payload


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
        "shared Object advisory lock"
    )


@pytest.mark.parametrize("replacement", ["absent", "source", "destination"])
def test_object_copy_recovery_does_not_apply_to_a_reallocated_object(
    replacement: str,
    postgres_connection: psycopg.Connection,
    postgres_repo: PostgresRepository,
) -> None:
    source, destination = primary_backend(), replacement_backend()
    PostgresRepository(
        connection=postgres_connection,
        storage_backend=destination,
        register_storage_backend=True,
    )
    storage = InMemoryObjectStorage()
    app = use_cases(postgres_repo, object_storage=storage)
    object_id = f"bbc/recovered-copy-{uuid4()}.mxf"
    original = MediaObjectRecord(
        id=object_id,
        referenced_by_flows={uuid4()},
        instances=[
            ObjectInstance(
                storage_backend=source, url=None, label=source.label, controlled=True
            )
        ],
    )
    postgres_repo.object_repository.save_object(original)
    app.objects.register_object_instance(
        object_id=object_id, registration={"storage_id": str(destination.id)}
    )
    # A worker can finish copying and exit before committing its advertisement.
    storage.write(object_id, b"old", backend=destination)
    postgres_repo.object_repository.delete_object(object_id)
    if replacement != "absent":
        backend = source if replacement == "source" else destination
        postgres_repo.object_repository.save_object(
            MediaObjectRecord(
                id=object_id,
                referenced_by_flows={uuid4()},
                instances=[
                    ObjectInstance(
                        storage_backend=backend,
                        url=None,
                        label=backend.label,
                        controlled=True,
                    )
                ],
            )
        )
        storage.write(object_id, b"new", backend=backend)

    assert (
        app.objects.process_pending_object_copies(
            worker_id="recovery", lease_seconds=60
        )
        == 1
    )
    assert storage.read(object_id, backend=destination) == (
        b"new" if replacement == "destination" else None
    )
    assert (
        len(postgres_repo.object_repository.list_object_copies(statuses={"done"})) == 1
    )
    if replacement != "absent":
        assert storage.read(object_id, backend=backend) == b"new"
        current = postgres_repo.object_repository.get_object(object_id)
        assert current is not None
        assert len(current.instances) == 1
