from __future__ import annotations

import logging
import signal
import sys
import time
from collections.abc import Callable
from datetime import timedelta

from tamoss.application.use_cases import TamossUseCases
from tamoss.bootstrap import create_use_cases
from tamoss.domain.model import utc_now
from tamoss.metrics import record_worker_tasks_processed, start_metrics_server
from tamoss.settings import DEFAULT_WORKER_LEASE_SECONDS, Settings, get_settings
from tamoss.storage_credentials import validate_credentials_file

logger = logging.getLogger("tamoss.worker")
_shutdown = False


class WorkerHealthError(RuntimeError):
    pass


def _handle_signal(signum: int, _frame: object) -> None:
    global _shutdown
    logger.info("received signal %s; shutting down", signum)
    _shutdown = True


def drain_delete_requests(
    use_cases: TamossUseCases,
    *,
    max_requests: int = 50,
    worker_id: str | None = None,
    lease_seconds: int = DEFAULT_WORKER_LEASE_SECONDS,
) -> int:
    resolved_worker_id = worker_id or _default_worker_id()
    processed = use_cases.deletion.process_pending_delete_requests(
        max_requests=max_requests,
        worker_id=resolved_worker_id,
        lease_seconds=lease_seconds,
    )
    processed += use_cases.objects.process_pending_object_copies(
        max_copies=max_requests,
        worker_id=resolved_worker_id,
        lease_seconds=lease_seconds,
    )
    processed += use_cases.deletion.queue_stale_allocated_object_cleanups(
        max_objects=max_requests,
    )
    processed += use_cases.deletion.process_pending_object_cleanups(
        max_cleanups=max_requests,
        worker_id=resolved_worker_id,
        lease_seconds=lease_seconds,
    )
    return processed


def drain_webhook_deliveries(
    use_cases: TamossUseCases,
    *,
    max_deliveries: int = 50,
    worker_id: str | None = None,
    lease_seconds: int = DEFAULT_WORKER_LEASE_SECONDS,
) -> int:
    return use_cases.webhooks.process_pending_webhook_deliveries(
        max_deliveries=max_deliveries,
        worker_id=worker_id or _default_worker_id(),
        lease_seconds=lease_seconds,
    )


def drain_once(
    use_cases: TamossUseCases,
    *,
    max_requests: int,
    worker_id: str,
    lease_seconds: int,
    enable_delete: bool,
    enable_webhook: bool,
) -> tuple[int, int]:
    # Keep one worker process sequential; run more replicas or split loops if queue
    # isolation becomes necessary.
    delete_processed = 0
    webhook_processed = 0
    if enable_delete:
        delete_processed = drain_delete_requests(
            use_cases,
            max_requests=max_requests,
            worker_id=worker_id,
            lease_seconds=lease_seconds,
        )
    if enable_webhook:
        webhook_processed = drain_webhook_deliveries(
            use_cases,
            max_deliveries=max_requests,
            worker_id=worker_id,
            lease_seconds=lease_seconds,
        )
    return delete_processed, webhook_processed


def purge_finished_queue_records(
    use_cases: TamossUseCases,
    *,
    retention_seconds: int,
    limit: int = 1000,
) -> int:
    """Drop terminal queue rows older than the retention window.

    Webhook deliveries accumulate one row per event per webhook; without a
    purge the queue tables grow without bound and their claim scans degrade.
    """
    if retention_seconds <= 0:
        return 0
    purge = getattr(use_cases.repository, "purge_finished_worker_records", None)
    if not callable(purge):
        return 0
    cutoff = utc_now() - timedelta(seconds=retention_seconds)
    return int(purge(older_than=cutoff, limit=limit))


def _default_worker_id() -> str:
    return get_settings().worker_id


def _close_use_cases(use_cases: TamossUseCases) -> None:
    close = getattr(getattr(use_cases, "repository", None), "close", None)
    if callable(close):
        close()


def _repository_health_check(repository: object) -> Callable[[], None]:
    check_connection = getattr(repository, "check_connection", None)
    if not callable(check_connection):
        raise WorkerHealthError("repository does not expose a health check")
    return check_connection


def check_health(settings: Settings | None = None) -> None:
    resolved_settings = settings or get_settings()
    if resolved_settings.storage_backend_credentials_file:
        try:
            validate_credentials_file(
                resolved_settings.storage_backend_credentials_file
            )
        except (OSError, ValueError) as exc:
            raise WorkerHealthError(
                f"storage backend credentials file is not readable: {exc}"
            ) from exc

    try:
        use_cases = create_use_cases(resolved_settings)
    except Exception as exc:
        raise WorkerHealthError(f"worker configuration is invalid: {exc}") from exc
    try:
        _repository_health_check(use_cases.repository)()
    except WorkerHealthError:
        raise
    except Exception as exc:
        raise WorkerHealthError(f"database connectivity check failed: {exc}") from exc
    finally:
        _close_use_cases(use_cases)


def health_main(settings: Settings | None = None) -> int:
    try:
        check_health(settings)
    except WorkerHealthError as exc:
        print(f"worker health check failed: {exc}", file=sys.stderr)
        return 1
    print("worker health check passed")
    return 0


def main(argv: list[str] | None = None) -> None:
    args = [] if argv is None else argv
    if args == ["health"]:
        raise SystemExit(health_main())
    if args:
        raise SystemExit(f"unsupported worker command: {' '.join(args)}")

    settings = get_settings()
    logging.basicConfig(level=settings.log_level.upper())
    signal.signal(signal.SIGTERM, _handle_signal)
    signal.signal(signal.SIGINT, _handle_signal)

    poll_interval = settings.worker_poll_interval_seconds
    max_requests = settings.worker_max_requests
    lease_seconds = settings.worker_lease_seconds
    worker_id = settings.worker_id
    enable_delete = settings.worker_enable_delete
    enable_webhook = settings.worker_enable_webhook
    queue_retention_seconds = settings.worker_queue_retention_seconds

    logger.info(
        "starting TAMOSS worker worker_id=%s poll_interval=%s max_requests=%s "
        "lease_seconds=%s delete=%s webhook=%s",
        worker_id,
        poll_interval,
        max_requests,
        lease_seconds,
        enable_delete,
        enable_webhook,
    )
    metrics_server = start_metrics_server(settings)
    use_cases = create_use_cases(settings)
    try:
        while not _shutdown:
            processed = 0
            try:
                delete_processed, webhook_processed = drain_once(
                    use_cases,
                    max_requests=max_requests,
                    worker_id=worker_id,
                    lease_seconds=lease_seconds,
                    enable_delete=enable_delete,
                    enable_webhook=enable_webhook,
                )
                record_worker_tasks_processed("delete", delete_processed)
                record_worker_tasks_processed("webhook", webhook_processed)
                processed = delete_processed + webhook_processed
                if delete_processed:
                    logger.info(
                        "processed %s object lifecycle task(s)", delete_processed
                    )
                if webhook_processed:
                    logger.info("processed %s webhook delivery(ies)", webhook_processed)
                purged = purge_finished_queue_records(
                    use_cases,
                    retention_seconds=queue_retention_seconds,
                )
                if purged:
                    logger.info("purged %s finished queue record(s)", purged)
            except Exception:
                logger.exception("worker poll failed; retrying")
            if not processed:
                time.sleep(poll_interval)
    finally:
        if metrics_server is not None:
            metrics_server.close()
        _close_use_cases(use_cases)


if __name__ == "__main__":
    main(sys.argv[1:])
