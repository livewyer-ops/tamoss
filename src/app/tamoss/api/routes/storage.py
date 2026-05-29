from __future__ import annotations

from typing import Annotated, Any
from uuid import UUID

from fastapi import APIRouter, Depends, Path, status

from tamoss.api.dependencies import get_storage_use_cases
from tamoss.application.contexts.storage import StorageUseCases
from tamoss.contract.generated import contract_models
from tamoss.contract.serialization import contract_dump

router = APIRouter(tags=["MediaStorage"])


@router.post(
    "/flows/{flowId}/storage",
    status_code=status.HTTP_201_CREATED,
    response_model=contract_models.FlowStorage,
    responses={
        400: {"description": "Bad request. Invalid storage request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow does not exist."},
    },
)
def allocate_flow_storage(
    flow_id: Annotated[UUID, Path(alias="flowId")],
    storage_request: contract_models.FlowStoragePost | None = None,
    storage: StorageUseCases = Depends(get_storage_use_cases),
) -> Any:
    allocations = storage.allocate_flow_storage(
        flow_id=flow_id,
        request=contract_dump(storage_request) if storage_request is not None else {},
    )
    return contract_dump(
        contract_models.FlowStorage(media_objects=allocations),
    )
