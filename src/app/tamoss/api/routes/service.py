from __future__ import annotations

from typing import Any

from fastapi import APIRouter, Body, Depends, Query, Request, Response

from tamoss.api.dependencies import get_service_use_cases
from tamoss.api.presenters import (
    head_response,
    storage_backend_response,
    with_page_headers,
)
from tamoss.api.query_params import tag_filter_parameters, validate_query_params
from tamoss.application.contexts.service import ServiceUseCases
from tamoss.contract.generated import contract_models
from tamoss.contract.serialization import contract_dump
from tamoss.contract.validation import strict_contract_model
from tamoss.domain.tags import parse_tag_filters
from tamoss.errors import BadRequest

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
    service_update: dict[str, Any] = Body(...),
    service: ServiceUseCases = Depends(get_service_use_cases),
) -> Response:
    try:
        validated = strict_contract_model(
            contract_models.ServicePost,
            service_update,
            non_nullable_fields=contract_models.ServicePost.model_fields,
        )
    except ValueError as exc:
        raise BadRequest("Bad request. Invalid service JSON.") from exc
    service.update_service_info(contract_dump(validated))
    return Response()


@router.get(
    "/service/storage-backends",
    dependencies=[Depends(tag_filter_parameters)],
)
@router.head(
    "/service/storage-backends",
    dependencies=[Depends(tag_filter_parameters)],
)
def storage_backends(
    request: Request,
    response: Response,
    reverse_order: bool = False,
    page: str | None = None,
    limit: int | None = Query(default=None, gt=0),
    service: ServiceUseCases = Depends(get_service_use_cases),
) -> Any:
    validate_query_params(
        request,
        {"reverse_order", "page", "limit"},
        allowed_prefixes=("tag.", "tag_exists."),
    )
    try:
        tag_values, tag_exists = parse_tag_filters(request.query_params)
    except ValueError as exc:
        raise BadRequest("Bad request. Invalid query options.") from exc
    backend_page = service.list_storage_backends_page(
        tag_values=tag_values,
        tag_exists=tag_exists,
        reverse_order=reverse_order,
        page=page,
        limit=limit,
    )
    with_page_headers(response, request, backend_page, reverse_order=reverse_order)
    if head := head_response(request, response):
        return head
    return [storage_backend_response(backend) for backend in backend_page.items]
