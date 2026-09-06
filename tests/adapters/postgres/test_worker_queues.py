from __future__ import annotations

from dataclasses import replace
from datetime import timedelta
from threading import Event
from uuid import uuid4

import psycopg
import pytest
from psycopg import sql
from tamoss.adapters.postgres import PostgresRepository
from tamoss.domain.model import (
    DeletionRequestRecord,
    ObjectCleanupRecord,
    ObjectCopyRecord,
    WebhookDeliveryRecord,
    utc_now,
)
from tamoss.worker import purge_finished_queue_records
from tamoss.worker_claims import WorkerClaimLost, keep_worker_claims

from tests.adapters.postgres.support import database_url, primary_backend, use_cases

pytestmark = pytest.mark.needs_db


@pytest.fixture
def peer_repo(postgres_connection):
    schema = postgres_connection.execute("SHOW search_path").fetchone()[0]
    with psycopg.connect(database_url(), autocommit=True) as connection:
        connection.execute(
            sql.SQL("SET search_path TO {}").format(sql.Identifier(schema))
        )
        yield PostgresRepository(
            connection=connection, storage_backend=primary_backend()
        )


@pytest.fixture(
    params=["webhook_delivery", "delete_request", "object_cleanup", "object_copy"]
)
def queue(request, postgres_repo, peer_repo):
    kind = request.param
    record = {
        "webhook_delivery": lambda: WebhookDeliveryRecord(
            id=uuid4(),
            webhook_id=uuid4(),
            webhook_snapshot={},
            event_type="flows/created",
            event_timestamp=utc_now(),
            payload={},
            status="pending",
        ),
        "delete_request": lambda: DeletionRequestRecord(
            id=uuid4(),
            flow_id=uuid4(),
            timerange_to_delete="_",
            delete_flow=False,
            status="created",
        ),
        "object_cleanup": lambda: ObjectCleanupRecord(
            id=uuid4(),
            object_id="media",
            storage_backend_id=primary_backend().id,
            status="pending",
        ),
        "object_copy": lambda: ObjectCopyRecord(
            id=uuid4(),
            object_id="media",
            source_storage_backend_id=primary_backend().id,
            destination_storage_backend_id=uuid4(),
            status="pending",
        ),
    }[kind]()
    store_name = {
        "webhook_delivery": "webhook_repository",
        "delete_request": "deletion_repository",
        "object_cleanup": "deletion_repository",
        "object_copy": "object_repository",
    }[kind]
    plural = {
        "webhook_delivery": "webhook_deliveries",
        "object_copy": "object_copies",
    }.get(kind, kind + "s")
    store = getattr(postgres_repo, store_name)
    peer = getattr(peer_repo, store_name)
    save = getattr(store, "save_" + kind)
    save(record)
    return (
        record,
        store,
        save,
        getattr(store, "claim_" + plural),
        getattr(peer, "claim_" + plural),
        "tamoss_" + plural,
    )


def expire(connection, table):
    connection.execute(
        sql.SQL(
            "UPDATE {} SET claim_expires_at = clock_timestamp() - INTERVAL '1 second'"
        ).format(sql.Identifier(table))
    )


def test_stale_claim_cannot_overwrite_or_recreate_work(queue, postgres_connection):
    _, _, save, claim, peer_claim, table = queue
    stale = claim(worker_id="a", limit=1, lease_seconds=30)[0]
    expire(postgres_connection, table)
    current = peer_claim(worker_id="b", limit=1, lease_seconds=30)[0]
    stale.status = "done"
    stale.claimed_at = stale.claimed_by = stale.claim_expires_at = None
    with pytest.raises(WorkerClaimLost):
        save(stale)
    save(
        replace(
            current,
            status="done",
            claimed_at=None,
            claimed_by=None,
            claim_expires_at=None,
        )
    )
    with pytest.raises(WorkerClaimLost):
        save(stale)
    assert (
        postgres_connection.execute(
            sql.SQL("SELECT status FROM {}").format(sql.Identifier(table))
        ).fetchone()[0]
        == "done"
    )
    postgres_connection.execute(sql.SQL("DELETE FROM {}").format(sql.Identifier(table)))
    with pytest.raises(WorkerClaimLost):
        save(stale)


def test_renewal_keeps_active_and_waiting_work_owned(queue, postgres_connection):
    record, store, save, claim, peer_claim, table = queue
    save(replace(record, id=uuid4()))
    claimed = claim(worker_id="a", limit=2, lease_seconds=1)
    assert len(claimed) == 2
    with keep_worker_claims(claimed, renew=store.renew_worker_claim, lease_seconds=1):
        Event().wait(1.4)
        assert peer_claim(worker_id="b", limit=2, lease_seconds=30) == []
        for item in claimed:
            save(item)
        assert postgres_connection.execute(
            sql.SQL(
                "SELECT bool_and(claim_expires_at > clock_timestamp()) FROM {}"
            ).format(sql.Identifier(table))
        ).fetchone()[0]
    expire(postgres_connection, table)
    assert len(peer_claim(worker_id="b", limit=2, lease_seconds=30)) == 2


def test_stale_parent_cannot_adopt_a_new_claim_or_save_children(
    postgres_repo, peer_repo, postgres_connection
):
    store = postgres_repo.deletion_repository
    request = DeletionRequestRecord(
        id=uuid4(),
        flow_id=uuid4(),
        timerange_to_delete="_",
        delete_flow=False,
        status="created",
    )
    store.save_delete_request(request)
    claimed = store.claim_delete_requests(worker_id="a", limit=1, lease_seconds=30)[0]
    with keep_worker_claims(
        [claimed], renew=store.renew_worker_claim, lease_seconds=30
    ):
        expire(postgres_connection, "tamoss_delete_requests")
        peer_repo.deletion_repository.claim_delete_requests(
            worker_id="b", limit=1, lease_seconds=30
        )
        fresh = store.get_delete_request(request.id)
        with pytest.raises(WorkerClaimLost):
            store.save_delete_request(replace(fresh, status="done"))
        with pytest.raises(WorkerClaimLost):
            store.save_object_cleanup(
                ObjectCleanupRecord(
                    id=uuid4(),
                    object_id="media",
                    storage_backend_id=primary_backend().id,
                    status="done",
                    delete_request_id=request.id,
                )
            )
    assert store.list_object_cleanups() == []


def test_production_retention_preserves_unfinished_dependencies(
    postgres_repo, postgres_connection
):
    store = postgres_repo.deletion_repository
    old = utc_now() - timedelta(days=8)
    active = DeletionRequestRecord(
        id=uuid4(),
        flow_id=uuid4(),
        timerange_to_delete="_",
        delete_flow=False,
        status="started",
    )
    finished = replace(active, id=uuid4(), status="done")
    for request in [active, finished]:
        store.save_delete_request(request)
        store.save_object_cleanup(
            ObjectCleanupRecord(
                id=uuid4(),
                object_id=str(request.id),
                storage_backend_id=primary_backend().id,
                status="done",
                delete_request_id=request.id,
            )
        )
    for table in ["tamoss_delete_requests", "tamoss_object_cleanups"]:
        postgres_connection.execute(
            sql.SQL("UPDATE {} SET updated_at = %s").format(sql.Identifier(table)),
            (old,),
        )
    cases = use_cases(postgres_repo)
    assert (
        purge_finished_queue_records(cases, retention_seconds=7 * 86400, limit=1) == 1
    )
    assert purge_finished_queue_records(cases, retention_seconds=7 * 86400) == 1
    assert store.get_delete_request(active.id) is not None
    assert len(store.list_object_cleanups(delete_request_id=active.id)) == 1
    assert purge_finished_queue_records(cases, retention_seconds=7 * 86400) == 0
