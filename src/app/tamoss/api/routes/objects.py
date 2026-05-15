from __future__ import annotations

from typing import Any
from uuid import UUID

from fastapi import APIRouter, Depends, Query, Request, Response, status

from tamoss.api.dependencies import get_use_cases
from tamoss.api.presenters import head_response, object_response, with_page_headers
from tamoss.api.query_params import (
    parse_get_url_labels,
    parse_storage_ids,
    validate_query_params,
)
from tamoss.api.schemas import MediaObjectRegistration
from tamoss.application.use_cases import TamossUseCases

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
    objectId: str,
    registration: MediaObjectRegistration,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    use_cases.register_object_instance(object_id=objectId, registration=registration)
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
    objectId: str,
    request: Request,
    storage_id: UUID | None = None,
    label: str | None = None,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    validate_query_params(request, {"storage_id", "label"})
    use_cases.delete_object_instance(
        object_id=objectId, storage_id=storage_id, label=label
    )
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.get(
    "/objects/{objectId:path}",
    responses={
        400: {"description": "Bad request. Invalid query options."},
        404: {"description": "The requested Media Object does not exist."},
    },
)
@router.head(
    "/objects/{objectId:path}",
    responses={
        400: {"description": "Bad request. Invalid query options."},
        404: {"description": "The requested Media Object does not exist."},
    },
)
def get_object(
    objectId: str,
    request: Request,
    response: Response,
    flow_tag_name: str | None = Query(default=None, alias="flow_tag.{name}"),
    flow_tag_exists_name: bool | None = Query(
        default=None, alias="flow_tag_exists.{name}"
    ),
    verbose_storage: bool = False,
    accept_get_urls: str | None = None,
    accept_storage_ids: str | None = None,
    presigned: bool | None = None,
    page: str | None = None,
    limit: int | None = Query(default=None, gt=0),
    use_cases: TamossUseCases = Depends(get_use_cases),
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
        allowed_prefixes=("flow_tag.", "flow_tag_exists."),
    )
    media_object = use_cases.get_object(objectId)
    accepted_labels = parse_get_url_labels(accept_get_urls)
    accepted_storage_ids = parse_storage_ids(accept_storage_ids)
    tag_values, tag_exists = _parse_flow_tag_filters(request)
    flow_page = use_cases.referenced_flows_matching_tags_page(
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
        get_urls=use_cases.object_get_urls(
            media_object,
            accept_get_urls=accepted_labels,
            accept_storage_ids=accepted_storage_ids,
            presigned=presigned,
            verbose_storage=verbose_storage,
        ),
    )


def _parse_flow_tag_filters(
    request: Request,
) -> tuple[dict[str, set[str]], dict[str, bool]]:
    values: dict[str, set[str]] = {}
    exists: dict[str, bool] = {}
    for key, value in request.query_params.items():
        if key.startswith("flow_tag."):
            values[key.removeprefix("flow_tag.")] = {
                part for part in value.split(",") if part
            }
        elif key.startswith("flow_tag_exists."):
            exists[key.removeprefix("flow_tag_exists.")] = value.lower() == "true"
    return values, exists
