from __future__ import annotations

from typing import Any
from uuid import UUID

from fastapi import APIRouter, Depends, Query, Request, Response, status

from tamoss.api.dependencies import get_use_cases
from tamoss.api.presenters import head_response, webhook_response, with_page_headers
from tamoss.api.query_params import validate_query_params
from tamoss.api.schemas import WebhookPost, WebhookPut
from tamoss.application.use_cases import TamossUseCases
from tamoss.domain.tags import parse_tag_filters

router = APIRouter(tags=["Webhooks"])


@router.get(
    "/service/webhooks",
    responses={404: {"description": "Webhooks are not supported."}},
)
@router.head(
    "/service/webhooks",
    responses={404: {"description": "Webhooks are not supported."}},
)
def list_webhooks(
    request: Request,
    response: Response,
    tag_name: str | None = Query(default=None, alias="tag.{name}"),
    tag_exists_name: bool | None = Query(default=None, alias="tag_exists.{name}"),
    page: str | None = None,
    limit: int | None = Query(default=None, gt=0),
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(
        request,
        {"page", "limit"},
        allowed_prefixes=("tag.", "tag_exists."),
    )
    tag_values, tag_exists = parse_tag_filters(request.query_params)
    webhook_page = use_cases.list_webhooks(
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
    webhook: WebhookPost,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    return webhook_response(use_cases.create_webhook(webhook))


@router.get(
    "/service/webhooks/{webhookId}",
    responses={404: {"description": "The requested Webhook ID is invalid."}},
)
@router.head(
    "/service/webhooks/{webhookId}",
    responses={404: {"description": "The requested Webhook ID is invalid."}},
)
def get_webhook(
    webhookId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    webhook = use_cases.get_webhook(webhookId)
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
    webhookId: UUID,
    webhook: WebhookPut,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    return webhook_response(
        use_cases.put_webhook(webhook_id=webhookId, webhook_put=webhook)
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
    webhookId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    validate_query_params(request, set())
    use_cases.delete_webhook(webhookId)
    return Response(status_code=status.HTTP_204_NO_CONTENT)
