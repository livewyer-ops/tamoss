from __future__ import annotations

import json
import logging
from dataclasses import dataclass
from pathlib import Path
from threading import RLock
from uuid import UUID

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class StorageBackendCredential:
    access_key: str
    secret_key: str


class StorageBackendCredentialFile:
    """Reload storage credentials when the configured file's mtime changes.

    The parsed credential map is cached between calls. A missing or malformed
    file keeps the last good credentials so in-flight storage operations do not
    lose access during a partial Secret projection update.
    """

    def __init__(self, path: str | None) -> None:
        self._path = Path(path) if path else None
        self._lock = RLock()
        self._credentials: dict[UUID, StorageBackendCredential] = {}
        self._mtime_ns: int | None = None
        self._loaded_once = False

    def get(self, storage_backend_id: UUID) -> StorageBackendCredential | None:
        if self._path is None:
            return None
        self._reload_if_changed()
        with self._lock:
            return self._credentials.get(storage_backend_id)

    def _reload_if_changed(self) -> None:
        assert self._path is not None
        try:
            stat = self._path.stat()
        except OSError as exc:
            if not self._loaded_once:
                logger.warning(
                    "storage backend credentials file is unavailable: %s", exc
                )
            return

        with self._lock:
            if self._loaded_once and stat.st_mtime_ns == self._mtime_ns:
                return

        try:
            credentials = _parse_credentials_file(self._path)
        except (OSError, ValueError):
            logger.warning(
                "storage backend credentials file could not be parsed",
                exc_info=True,
            )
            return

        with self._lock:
            self._credentials = credentials
            self._mtime_ns = stat.st_mtime_ns
            self._loaded_once = True


def _parse_credentials_file(path: Path) -> dict[UUID, StorageBackendCredential]:
    with path.open(encoding="utf-8") as handle:
        payload = json.load(handle)
    if isinstance(payload, dict):
        raw_credentials = payload.get("credentials")
    else:
        raw_credentials = payload
    if not isinstance(raw_credentials, list):
        raise ValueError("credentials file must contain a credentials list")

    parsed: dict[UUID, StorageBackendCredential] = {}
    for index, item in enumerate(raw_credentials):
        if not isinstance(item, dict):
            raise ValueError(f"credential entry {index} must be an object")
        storage_backend_id = item.get("storageBackendId") or item.get(
            "storage_backend_id"
        )
        access_key = item.get("accessKey") or item.get("access_key")
        secret_key = item.get("secretKey") or item.get("secret_key")
        if not storage_backend_id or not access_key or not secret_key:
            raise ValueError(
                "credential entries require storageBackendId, accessKey, and secretKey"
            )
        parsed[UUID(str(storage_backend_id))] = StorageBackendCredential(
            access_key=str(access_key),
            secret_key=str(secret_key),
        )
    return parsed


def validate_credentials_file(path: str) -> None:
    _parse_credentials_file(Path(path))
