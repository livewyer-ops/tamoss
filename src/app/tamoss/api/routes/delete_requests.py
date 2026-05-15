from __future__ import annotations

from typing import Any
from uuid import UUID

from fastapi import APIRouter, Depends, Request

from tamoss.api.dependencies import get_use_cases
from tamoss.api.presenters import deletion_request_response, head_response
from tamoss.api.query_params import validate_query_params
from tamoss.application.use_cases import TamossUseCases

router = APIRouter(tags=["FlowDeleteRequests"])


@router.get("/flow-delete-requests")
@router.head("/flow-delete-requests")
def list_delete_requests(
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    if head := head_response(request):
        return head
    return [
        deletion_request_response(delete_request)
        for delete_request in use_cases.list_delete_requests()
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
    request_id: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    delete_request = use_cases.get_delete_request(request_id)
    if head := head_response(request):
        return head
    return deletion_request_response(delete_request)
