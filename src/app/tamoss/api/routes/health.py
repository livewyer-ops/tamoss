from __future__ import annotations

from fastapi import APIRouter, Depends
from fastapi.responses import JSONResponse, PlainTextResponse

from tamoss.api.dependencies import get_use_cases
from tamoss.application.use_cases import TamossUseCases

router = APIRouter(tags=["Health"])


@router.get("/healthz", include_in_schema=False, response_class=PlainTextResponse)
def healthz() -> str:
    return "ok"


@router.get("/readyz", include_in_schema=False)
def readyz(
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> JSONResponse:
    repository_ready = True
    try:
        use_cases.repository.get_service_metadata()
    except Exception:
        repository_ready = False
    storage_backends = use_cases.list_storage_backends()
    ready = repository_ready and bool(storage_backends)
    return JSONResponse(
        status_code=200 if ready else 503,
        content={
            "status": "ready" if ready else "not_ready",
            "checks": {
                "repository": {
                    "ok": repository_ready,
                },
                "storage_backends": {
                    "ok": bool(storage_backends),
                    "count": len(storage_backends),
                },
            },
        },
    )
