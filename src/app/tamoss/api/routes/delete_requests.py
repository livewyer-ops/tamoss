from __future__ import annotations

from typing import Any
from uuid import UUID

from fastapi import APIRouter, Depends, Query, Request, Response

from tamoss.api.dependencies import get_app_settings, get_deletion_use_cases
from tamoss.api.presenters import (
    deletion_request_response,
    head_response,
    with_page_headers,
)
from tamoss.api.query_params import validate_query_params
from tamoss.application.contexts.deletion import DeletionUseCases
from tamoss.domain.listings import DeleteRequestSortBy
from tamoss.errors import NotFound
from tamoss.settings import Settings

router = APIRouter(tags=["FlowDeleteRequests"])


@router.get("/flow-delete-requests")
@router.head("/flow-delete-requests")
def list_delete_requests(
    request: Request,
    response: Response,
    reverse_order: bool = False,
    sort_by: DeleteRequestSortBy = DeleteRequestSortBy.CREATED,
    page: str | None = None,
    limit: int | None = Query(default=None, gt=0),
    deletion: DeletionUseCases = Depends(get_deletion_use_cases),
    settings: Settings = Depends(get_app_settings),
) -> Any:
    validate_query_params(request, {"reverse_order", "sort_by", "page", "limit"})
    delete_request_page = deletion.list_delete_requests_page(
        sort_by=sort_by,
        reverse_order=reverse_order,
        page=page,
        limit=limit,
    )
    with_page_headers(
        response,
        request,
        delete_request_page,
        reverse_order=reverse_order,
    )
    if head := head_response(request, response):
        return head
    return [
        deletion_request_response(
            delete_request,
            retention_seconds=settings.worker_queue_retention_seconds,
        )
        for delete_request in delete_request_page.items
    ]


@router.get(
    "/flow-delete-requests/{request_id}",
    responses={
        404: {"description": "The requested Flow delete request does not exist."}
    },
)
@router.head(
    "/flow-delete-requests/{request_id}",
    responses={
        404: {"description": "The requested Flow delete request does not exist."}
    },
)
def get_delete_request(
    request_id: str,
    request: Request,
    deletion: DeletionUseCases = Depends(get_deletion_use_cases),
    settings: Settings = Depends(get_app_settings),
) -> Any:
    validate_query_params(request, set())
    try:
        request_uuid = UUID(request_id)
    except ValueError:
        raise NotFound("The requested flow delete request does not exist.") from None
    delete_request = deletion.get_delete_request(request_uuid)
    if head := head_response(request):
        return head
    return deletion_request_response(
        delete_request,
        retention_seconds=settings.worker_queue_retention_seconds,
    )
