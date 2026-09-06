from __future__ import annotations

import logging
from collections.abc import Callable, Iterable, Iterator
from contextlib import contextmanager
from contextvars import ContextVar
from dataclasses import replace
from threading import Event, Thread
from uuid import UUID

from tamoss.domain.model import (
    DeletionRequestRecord,
    ObjectCleanupRecord,
    ObjectCopyRecord,
    WebhookDeliveryRecord,
)

type WorkerRecord = (
    WebhookDeliveryRecord
    | DeletionRequestRecord
    | ObjectCleanupRecord
    | ObjectCopyRecord
)

logger = logging.getLogger(__name__)
active_worker_claims: ContextVar[dict[UUID, WorkerRecord] | None] = ContextVar(
    "active_worker_claims", default=None
)


class WorkerClaimLost(RuntimeError):
    """The worker no longer owns this queue record."""


@contextmanager
def keep_worker_claims(
    records: Iterable[WorkerRecord],
    *,
    renew: Callable[[WorkerRecord, int], bool],
    lease_seconds: int,
) -> Iterator[None]:
    # Keep the original generation even when processors clear or re-read claims.
    claims = [replace(record) for record in records]
    if not claims:
        yield
        return
    for claim in claims:
        if not renew(claim, lease_seconds):
            raise WorkerClaimLost(str(claim.id))
    stopped = Event()

    def heartbeat() -> None:
        pending = claims
        while pending and not stopped.wait(lease_seconds / 3):
            try:
                pending = [claim for claim in pending if renew(claim, lease_seconds)]
            except Exception:
                # Saves also check expiry, so renewal failure cannot authorise
                # stale results. Leave work retryable once its lease expires.
                logger.exception("worker claim renewal failed")
                return

    token = active_worker_claims.set({claim.id: claim for claim in claims})
    thread = Thread(target=heartbeat, name="tamoss-claim-renewal", daemon=True)
    thread.start()
    try:
        yield
    finally:
        stopped.set()
        thread.join()
        active_worker_claims.reset(token)
