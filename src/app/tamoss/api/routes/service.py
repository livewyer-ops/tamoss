from __future__ import annotations

from typing import Any

from fastapi import APIRouter, Depends, Request, Response

from tamoss.api.dependencies import get_use_cases
from tamoss.api.presenters import head_response, storage_backend_response
from tamoss.api.query_params import validate_query_params
from tamoss.api.schemas import ServiceInfoUpdate
from tamoss.application.use_cases import TamossUseCases

router = APIRouter(tags=["Service"])


@router.get("/")
@router.head("/")
def root(request: Request, use_cases: TamossUseCases = Depends(get_use_cases)) -> Any:
    validate_query_params(request, set())
    if head := head_response(request):
        return head
    return use_cases.root_paths()


@router.get("/service")
@router.head("/service")
def service(
    request: Request, use_cases: TamossUseCases = Depends(get_use_cases)
) -> Any:
    validate_query_params(request, set())
    if head := head_response(request):
        return head
    return use_cases.service_info()


@router.post(
    "/service",
    response_class=Response,
    responses={
        400: {"description": "Bad request. Invalid service JSON."},
        403: {"description": "Forbidden."},
    },
)
def post_service(
    service_update: ServiceInfoUpdate,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    use_cases.update_service_info(service_update)
    return Response()


@router.get("/service/storage-backends")
@router.head("/service/storage-backends")
def storage_backends(
    request: Request, use_cases: TamossUseCases = Depends(get_use_cases)
) -> Any:
    validate_query_params(request, set())
    if head := head_response(request):
        return head
    return [
        storage_backend_response(backend)
        for backend in use_cases.list_storage_backends()
    ]
