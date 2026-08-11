from __future__ import annotations

from typing import Annotated, Any
from urllib.parse import unquote
from uuid import UUID

from fastapi import APIRouter, Depends, Path, Query, Request, Response, status

from tamoss.api.dependencies import get_flow_use_cases, get_object_use_cases
from tamoss.api.presenters import head_response, object_response, with_page_headers
from tamoss.api.query_params import (
    flow_tag_filter_parameters,
    parse_flow_tag_filters,
    parse_get_url_labels,
    parse_storage_backend_tag_filters,
    parse_storage_ids,
    storage_backend_tag_filter_parameters,
    validate_query_params,
)
from tamoss.application.contexts.flows import FlowUseCases
from tamoss.application.contexts.objects import ObjectUseCases
from tamoss.contract.generated import contract_models
from tamoss.contract.serialization import contract_dump

router = APIRouter(tags=["Objects"])


@router.post(
    "/objects/{objectId:path}/instances",
    status_code=status.HTTP_201_CREATED,
    response_class=Response,
    responses={
        400: {"description": "Bad request. Invalid request JSON."},
        403: {"description": "Forbidden."},
        404: {"description": "The Media Object does not exist."},
    },
)
def post_object_instance(
    object_id: Annotated[str, Path(alias="objectId")],
    registration: contract_models.ObjectsInstancesPost,
    objects: ObjectUseCases = Depends(get_object_use_cases),
) -> Response:
    objects.register_object_instance(
        object_id=unquote(object_id),
        registration=contract_dump(registration),
    )
    return Response(status_code=status.HTTP_201_CREATED)


@router.delete(
    "/objects/{objectId:path}/instances",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        400: {"description": "Bad request. Invalid query options."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Object ID in the path is invalid."},
    },
)
def delete_object_instance(
    object_id: Annotated[str, Path(alias="objectId")],
    request: Request,
    storage_id: UUID | None = None,
    label: str | None = None,
    objects: ObjectUseCases = Depends(get_object_use_cases),
) -> Response:
    validate_query_params(request, {"storage_id", "label"})
    objects.delete_object_instance(
        object_id=unquote(object_id),
        storage_id=storage_id,
        label=label,
    )
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.get(
    "/objects/{objectId:path}",
    responses={
        400: {"description": "Bad request. Invalid query options."},
        404: {"description": "The requested Media Object does not exist."},
    },
    dependencies=[
        Depends(flow_tag_filter_parameters),
        Depends(storage_backend_tag_filter_parameters),
    ],
)
@router.head(
    "/objects/{objectId:path}",
    responses={
        400: {"description": "Bad request. Invalid query options."},
        404: {"description": "The requested Media Object does not exist."},
    },
    dependencies=[
        Depends(flow_tag_filter_parameters),
        Depends(storage_backend_tag_filter_parameters),
    ],
)
def get_object(
    object_id: Annotated[str, Path(alias="objectId")],
    request: Request,
    response: Response,
    verbose_storage: bool = False,
    accept_get_urls: str | None = None,
    accept_storage_ids: str | None = None,
    presigned: bool | None = None,
    page: str | None = None,
    limit: int | None = Query(default=None, gt=0),
    objects: ObjectUseCases = Depends(get_object_use_cases),
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Any:
    validate_query_params(
        request,
        {
            "verbose_storage",
            "accept_get_urls",
            "accept_storage_ids",
            "presigned",
            "page",
            "limit",
        },
        allowed_prefixes=(
            "flow_tag.",
            "flow_tag_exists.",
            "storage_backend_tag.",
            "storage_backend_tag_exists.",
        ),
    )
    media_object = objects.get_object(object_id)
    accepted_labels = parse_get_url_labels(accept_get_urls)
    accepted_storage_ids = parse_storage_ids(accept_storage_ids)
    tag_values, tag_exists = parse_flow_tag_filters(request)
    storage_tag_values, storage_tag_exists = parse_storage_backend_tag_filters(request)
    flow_page = flows.referenced_flows_matching_tags_page(
        media_object,
        tag_values,
        tag_exists,
        page=page,
        limit=limit,
    )
    with_page_headers(response, request, flow_page)
    if head := head_response(request, response):
        return head
    return object_response(
        media_object,
        flow_page.items,
        get_urls=objects.object_get_urls(
            media_object,
            accept_get_urls=accepted_labels,
            accept_storage_ids=accepted_storage_ids,
            presigned=presigned,
            verbose_storage=verbose_storage,
            storage_tag_values=storage_tag_values,
            storage_tag_exists=storage_tag_exists,
        ),
    )
