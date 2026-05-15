from __future__ import annotations

from typing import Any
from uuid import UUID

from fastapi import APIRouter, Depends, status

from tamoss.api.dependencies import get_use_cases
from tamoss.api.schemas import FlowStoragePost, FlowStorageResponse
from tamoss.application.use_cases import TamossUseCases

router = APIRouter(tags=["MediaStorage"])


@router.post(
    "/flows/{flowId}/storage",
    status_code=status.HTTP_201_CREATED,
    responses={
        400: {"description": "Bad request. Invalid storage request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow does not exist."},
    },
)
def allocate_flow_storage(
    flowId: UUID,
    storage_request: FlowStoragePost | None = None,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    allocations = use_cases.allocate_flow_storage(
        flow_id=flowId, request=storage_request or FlowStoragePost()
    )
    return FlowStorageResponse(media_objects=allocations).model_dump(
        by_alias=True, mode="json"
    )
