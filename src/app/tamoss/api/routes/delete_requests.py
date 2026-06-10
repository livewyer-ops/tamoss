from __future__ import annotations

from typing import Any
from uuid import UUID

from fastapi import APIRouter, Depends, Request

from tamoss.api.dependencies import get_app_settings, get_deletion_use_cases
from tamoss.api.presenters import deletion_request_response, head_response
from tamoss.api.query_params import validate_query_params
from tamoss.application.contexts.deletion import DeletionUseCases
from tamoss.errors import NotFound
from tamoss.settings import Settings

router = APIRouter(tags=["FlowDeleteRequests"])


@router.get("/flow-delete-requests")
@router.head("/flow-delete-requests")
def list_delete_requests(
    request: Request,
    deletion: DeletionUseCases = Depends(get_deletion_use_cases),
    settings: Settings = Depends(get_app_settings),
) -> Any:
    validate_query_params(request, set())
    if head := head_response(request):
        return head
    return [
        deletion_request_response(
            delete_request,
            retention_seconds=settings.worker_queue_retention_seconds,
        )
        for delete_request in deletion.list_delete_requests()
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
