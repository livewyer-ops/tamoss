from __future__ import annotations

import psycopg
from fastapi import APIRouter, Depends
from fastapi.responses import JSONResponse, PlainTextResponse
from psycopg_pool import PoolTimeout

from tamoss.api.dependencies import get_use_cases
from tamoss.application.use_cases import TamossUseCases
from tamoss.db.migrations import CURRENT_SCHEMA_REVISION
from tamoss.db.migrations.runner import MultipleAlembicHeads, UnsupportedSchemaRevision
from tamoss.domain.model import StorageBackend
from tamoss.errors import ConfigurationError

router = APIRouter(tags=["Health"])

_DEPENDENCY_READINESS_ERRORS = (
    ConfigurationError,
    OSError,
    PoolTimeout,
    psycopg.Error,
    UnsupportedSchemaRevision,
)

# These strings are readiness contract reason codes, not route-local labels.
REASON_DATABASE_UNAVAILABLE = "DatabaseUnavailable"
REASON_OBJECT_STORE_REACHABILITY_SKIPPED = "ObjectStoreReachabilitySkipped"
REASON_OBJECT_STORE_REACHABLE = "ObjectStoreReachable"
REASON_OBJECT_STORE_UNREACHABLE = "ObjectStoreUnreachable"
REASON_REPOSITORY_READY = "RepositoryReady"
REASON_SCHEMA_REVISION_MISMATCH = "SchemaRevisionMismatch"
REASON_SCHEMA_REVISION_MULTIPLE_HEADS = "SchemaRevisionMultipleHeads"
REASON_SCHEMA_REVISION_UNSUPPORTED = "SchemaRevisionUnsupported"
REASON_STORAGE_BACKEND_METADATA_MISSING = "StorageBackendMetadataMissing"
REASON_STORAGE_BACKEND_METADATA_READY = "StorageBackendMetadataReady"
REASON_STORAGE_BACKEND_METADATA_UNAVAILABLE = "StorageBackendMetadataUnavailable"


@router.get("/healthz", include_in_schema=False, response_class=PlainTextResponse)
def healthz() -> str:
    return "ok"


@router.get("/readyz", include_in_schema=False)
def readyz(
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> JSONResponse:
    repository_check = _repository_readiness(use_cases)
    storage_backend_check = _storage_backend_readiness(use_cases)
    object_store_check = _object_store_readiness(use_cases, storage_backend_check)
    ready = bool(
        repository_check["ok"]
        and storage_backend_check["ok"]
        and object_store_check["ok"]
    )
    return JSONResponse(
        status_code=200 if ready else 503,
        content={
            "status": "ready" if ready else "not_ready",
            "checks": {
                "repository": repository_check,
                "storage_backends": storage_backend_check,
                "object_store": object_store_check,
            },
        },
    )


def _repository_readiness(use_cases: TamossUseCases) -> dict[str, object]:
    try:
        use_cases.service.service_info()
        schema_revision = use_cases.repository.current_schema_revision()
    except MultipleAlembicHeads as exc:
        return {
            "ok": False,
            "reason": REASON_SCHEMA_REVISION_MULTIPLE_HEADS,
            "error": type(exc).__name__,
        }
    except UnsupportedSchemaRevision as exc:
        return {
            "ok": False,
            "reason": REASON_SCHEMA_REVISION_UNSUPPORTED,
            "error": type(exc).__name__,
        }
    except _DEPENDENCY_READINESS_ERRORS as exc:
        return {
            "ok": False,
            "reason": REASON_DATABASE_UNAVAILABLE,
            "error": type(exc).__name__,
        }
    if schema_revision != CURRENT_SCHEMA_REVISION:
        return {
            "ok": False,
            "reason": REASON_SCHEMA_REVISION_MISMATCH,
            "observed": schema_revision or "",
            "expected": CURRENT_SCHEMA_REVISION,
        }
    return {
        "ok": True,
        "reason": REASON_REPOSITORY_READY,
        "schemaRevision": schema_revision,
    }


def _storage_backend_readiness(use_cases: TamossUseCases) -> dict[str, object]:
    try:
        storage_backends = use_cases.service.list_storage_backends()
    except _DEPENDENCY_READINESS_ERRORS as exc:
        return {
            "ok": False,
            "reason": REASON_STORAGE_BACKEND_METADATA_UNAVAILABLE,
            "error": type(exc).__name__,
            "count": 0,
        }
    if not storage_backends:
        return {
            "ok": False,
            "reason": REASON_STORAGE_BACKEND_METADATA_MISSING,
            "count": 0,
        }
    return {
        "ok": True,
        "reason": REASON_STORAGE_BACKEND_METADATA_READY,
        "count": len(storage_backends),
        "backendIds": [str(backend.id) for backend in storage_backends],
    }


def _object_store_readiness(
    use_cases: TamossUseCases, storage_backend_check: dict[str, object]
) -> dict[str, object]:
    if not storage_backend_check["ok"]:
        return {
            "ok": False,
            "reason": REASON_OBJECT_STORE_REACHABILITY_SKIPPED,
            "count": 0,
        }
    try:
        storage_backends = use_cases.service.list_storage_backends()
        for backend in storage_backends:
            _check_object_store_backend(use_cases, backend)
    except Exception as exc:
        return {
            "ok": False,
            "reason": REASON_OBJECT_STORE_UNREACHABLE,
            "error": type(exc).__name__,
        }
    return {
        "ok": True,
        "reason": REASON_OBJECT_STORE_REACHABLE,
        "count": len(storage_backends),
    }


def _check_object_store_backend(
    use_cases: TamossUseCases, backend: StorageBackend
) -> None:
    use_cases.object_storage.check_backend(backend)
