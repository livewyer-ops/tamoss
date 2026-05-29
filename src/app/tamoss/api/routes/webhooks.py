from __future__ import annotations

from typing import Annotated, Any
from uuid import UUID

from fastapi import APIRouter, Depends, Path, Query, Request, Response, status

from tamoss.api.dependencies import get_webhook_use_cases
from tamoss.api.presenters import head_response, webhook_response, with_page_headers
from tamoss.api.query_params import tag_filter_parameters, validate_query_params
from tamoss.application.contexts.webhooks import WebhookUseCases
from tamoss.contract.generated import contract_models
from tamoss.contract.serialization import contract_dump
from tamoss.domain.tags import parse_tag_filters
from tamoss.errors import BadRequest

router = APIRouter(tags=["Webhooks"])


@router.get(
    "/service/webhooks",
    responses={404: {"description": "Webhooks are not supported."}},
    dependencies=[Depends(tag_filter_parameters)],
)
@router.head(
    "/service/webhooks",
    responses={404: {"description": "Webhooks are not supported."}},
    dependencies=[Depends(tag_filter_parameters)],
)
def list_webhooks(
    request: Request,
    response: Response,
    page: str | None = None,
    limit: int | None = Query(default=None, gt=0),
    webhooks: WebhookUseCases = Depends(get_webhook_use_cases),
) -> Any:
    validate_query_params(
        request,
        {"page", "limit"},
        allowed_prefixes=("tag.", "tag_exists."),
    )
    try:
        tag_values, tag_exists = parse_tag_filters(request.query_params)
    except ValueError as exc:
        raise BadRequest("Bad request. Invalid query options.") from exc
    webhook_page = webhooks.list_webhooks(
        tag_values=tag_values,
        tag_exists=tag_exists,
        page=page,
        limit=limit,
    )
    with_page_headers(response, request, webhook_page)
    if head := head_response(request, response):
        return head
    return [webhook_response(webhook) for webhook in webhook_page.items]


@router.post(
    "/service/webhooks",
    status_code=status.HTTP_201_CREATED,
    responses={
        400: {"description": "Bad request."},
        404: {"description": "Webhooks are not supported."},
    },
)
def post_webhook(
    webhook: contract_models.WebhookPost,
    webhooks: WebhookUseCases = Depends(get_webhook_use_cases),
) -> Any:
    return webhook_response(webhooks.create_webhook(contract_dump(webhook)))


@router.get(
    "/service/webhooks/{webhookId}",
    responses={404: {"description": "The requested Webhook ID is invalid."}},
)
@router.head(
    "/service/webhooks/{webhookId}",
    responses={404: {"description": "The requested Webhook ID is invalid."}},
)
def get_webhook(
    webhook_id: Annotated[UUID, Path(alias="webhookId")],
    request: Request,
    webhooks: WebhookUseCases = Depends(get_webhook_use_cases),
) -> Any:
    validate_query_params(request, set())
    webhook = webhooks.get_webhook(webhook_id)
    if head := head_response(request):
        return head
    return webhook_response(webhook)


@router.put(
    "/service/webhooks/{webhookId}",
    status_code=status.HTTP_201_CREATED,
    responses={
        400: {"description": "Bad request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Webhook ID is invalid."},
    },
)
def put_webhook(
    webhook_id: Annotated[UUID, Path(alias="webhookId")],
    webhook: contract_models.WebhookPut,
    webhooks: WebhookUseCases = Depends(get_webhook_use_cases),
) -> Any:
    return webhook_response(
        webhooks.put_webhook(
            webhook_id=webhook_id,
            webhook=contract_dump(webhook),
        )
    )


@router.delete(
    "/service/webhooks/{webhookId}",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        403: {"description": "Forbidden."},
        404: {"description": "The requested Webhook ID is invalid."},
    },
)
def delete_webhook(
    webhook_id: Annotated[UUID, Path(alias="webhookId")],
    request: Request,
    webhooks: WebhookUseCases = Depends(get_webhook_use_cases),
) -> Response:
    validate_query_params(request, set())
    webhooks.delete_webhook(webhook_id)
    return Response(status_code=status.HTTP_204_NO_CONTENT)
