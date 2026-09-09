from __future__ import annotations

from typing import Annotated, Any
from uuid import UUID

from fastapi import APIRouter, Body, Depends, Path, Request, status

from tamoss.api.dependencies import get_storage_use_cases
from tamoss.application.contexts.storage import StorageUseCases
from tamoss.contract.generated import contract_models
from tamoss.contract.serialization import contract_dump
from tamoss.contract.validation import strict_contract_model
from tamoss.errors import BadRequest

router = APIRouter(tags=["MediaStorage"])


async def _reject_explicit_null_storage_body(request: Request) -> None:
    if (await request.body()).strip() == b"null":
        raise BadRequest("Bad request. Invalid storage request.")


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
    storage_request: dict[str, Any] | None = Body(default=None),
    _null_body: None = Depends(_reject_explicit_null_storage_body),
    storage: StorageUseCases = Depends(get_storage_use_cases),
) -> Any:
    try:
        validated_request = (
            strict_contract_model(
                contract_models.FlowStoragePost,
                storage_request,
                recursive_non_nullable_fields=(
                    contract_models.FlowStoragePost.model_fields
                ),
            )
            if storage_request is not None
            else None
        )
    except (TypeError, ValueError) as exc:
        raise BadRequest("Bad request. Invalid storage request.") from exc
    allocations = storage.allocate_flow_storage(
        flow_id=flow_id,
        request=contract_dump(validated_request)
        if validated_request is not None
        else {},
    )
    return contract_dump(
        contract_models.FlowStorage(media_objects=allocations),
    )
