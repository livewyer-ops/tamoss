from __future__ import annotations

from typing import Any

from fastapi import APIRouter, Depends, Query, Request, Response

from tamoss.api.dependencies import get_service_use_cases
from tamoss.api.presenters import (
    head_response,
    storage_backend_response,
    with_page_headers,
)
from tamoss.api.query_params import validate_query_params
from tamoss.application.contexts.service import ServiceUseCases
from tamoss.contract.generated import contract_models
from tamoss.contract.serialization import contract_dump

router = APIRouter(tags=["Service"])


@router.get("/")
@router.head("/")
def root(
    request: Request, service: ServiceUseCases = Depends(get_service_use_cases)
) -> Any:
    validate_query_params(request, set())
    if head := head_response(request):
        return head
    return service.root_paths()


@router.get("/service")
@router.head("/service")
def service(
    request: Request, service: ServiceUseCases = Depends(get_service_use_cases)
) -> Any:
    validate_query_params(request, set())
    if head := head_response(request):
        return head
    return service.service_info()


@router.post(
    "/service",
    response_class=Response,
    responses={
        400: {"description": "Bad request. Invalid service JSON."},
        403: {"description": "Forbidden."},
    },
)
def post_service(
    service_update: contract_models.ServicePost,
    service: ServiceUseCases = Depends(get_service_use_cases),
) -> Response:
    service.update_service_info(contract_dump(service_update))
    return Response()


@router.get("/service/storage-backends")
@router.head("/service/storage-backends")
def storage_backends(
    request: Request,
    response: Response,
    reverse_order: bool = False,
    page: str | None = None,
    limit: int | None = Query(default=None, gt=0),
    service: ServiceUseCases = Depends(get_service_use_cases),
) -> Any:
    validate_query_params(request, {"reverse_order", "page", "limit"})
    backend_page = service.list_storage_backends_page(
        tag_values={},
        tag_exists={},
        reverse_order=reverse_order,
        page=page,
        limit=limit,
    )
    with_page_headers(response, request, backend_page, reverse_order=reverse_order)
    if head := head_response(request, response):
        return head
    return [storage_backend_response(backend) for backend in backend_page.items]
