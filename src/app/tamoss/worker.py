from __future__ import annotations

import logging
import os
import signal
import socket
import time

from tamoss.application.use_cases import DEFAULT_WORKER_LEASE_SECONDS, TamossUseCases
from tamoss.bootstrap import create_use_cases

logger = logging.getLogger("tamoss.worker")
_shutdown = False


def _handle_signal(signum: int, _frame: object) -> None:
    global _shutdown
    logger.info("received signal %s; shutting down", signum)
    _shutdown = True


def _positive_int_env(name: str, default: int) -> int:
    raw = os.getenv(name, str(default))
    try:
        value = int(raw)
    except ValueError as exc:
        raise SystemExit(f"{name} must be a positive integer") from exc
    if value <= 0:
        raise SystemExit(f"{name} must be a positive integer")
    return value


def drain_delete_requests(
    use_cases: TamossUseCases,
    *,
    max_requests: int = 50,
    worker_id: str | None = None,
    lease_seconds: int = DEFAULT_WORKER_LEASE_SECONDS,
) -> int:
    return use_cases.process_pending_delete_requests(
        max_requests=max_requests,
        worker_id=worker_id or _default_worker_id(),
        lease_seconds=lease_seconds,
    )


def drain_webhook_deliveries(
    use_cases: TamossUseCases,
    *,
    max_deliveries: int = 50,
    worker_id: str | None = None,
    lease_seconds: int = DEFAULT_WORKER_LEASE_SECONDS,
) -> int:
    return use_cases.process_pending_webhook_deliveries(
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


def _default_worker_id() -> str:
    return os.getenv("TAMOSS_WORKER_ID") or f"{socket.gethostname()}:{os.getpid()}"


def _close_use_cases(use_cases: TamossUseCases) -> None:
    close = getattr(getattr(use_cases, "repository", None), "close", None)
    if callable(close):
        close()


def main() -> None:
    log_level = os.getenv("TAMOSS_LOG_LEVEL") or os.getenv("LOG_LEVEL") or "INFO"
    logging.basicConfig(level=log_level.upper())
    signal.signal(signal.SIGTERM, _handle_signal)
    signal.signal(signal.SIGINT, _handle_signal)

    poll_interval = _positive_int_env("TAMOSS_WORKER_POLL_INTERVAL_SECONDS", 5)
    max_requests = _positive_int_env("TAMOSS_WORKER_MAX_REQUESTS", 50)
    lease_seconds = _positive_int_env(
        "TAMOSS_WORKER_LEASE_SECONDS",
        DEFAULT_WORKER_LEASE_SECONDS,
    )
    worker_id = _default_worker_id()
    enable_delete = os.getenv("TAMOSS_WORKER_ENABLE_DELETE", "1") == "1"
    enable_webhook = os.getenv("TAMOSS_WORKER_ENABLE_WEBHOOK", "1") == "1"

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
    use_cases = create_use_cases()
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
                processed = delete_processed + webhook_processed
                if delete_processed:
                    logger.info("processed %s delete request(s)", delete_processed)
                if webhook_processed:
                    logger.info("processed %s webhook delivery(ies)", webhook_processed)
            except Exception:
                logger.exception("worker poll failed; retrying")
            if not processed:
                time.sleep(poll_interval)
    finally:
        _close_use_cases(use_cases)


if __name__ == "__main__":
    main()
