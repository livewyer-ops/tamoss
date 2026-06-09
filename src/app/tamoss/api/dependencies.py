from __future__ import annotations

import json

from fastapi import Request

from tamoss.application.contexts.deletion import DeletionUseCases
from tamoss.application.contexts.flows import FlowUseCases
from tamoss.application.contexts.objects import ObjectUseCases
from tamoss.application.contexts.segments import SegmentUseCases
from tamoss.application.contexts.service import ServiceUseCases
from tamoss.application.contexts.sources import SourceUseCases
from tamoss.application.contexts.storage import StorageUseCases
from tamoss.application.contexts.webhooks import WebhookUseCases
from tamoss.application.use_cases import TamossUseCases
from tamoss.errors import BadRequest


async def require_json_body(request: Request) -> None:
    try:
        json.loads(await request.body())
    except (UnicodeDecodeError, ValueError):
        raise BadRequest("Bad request. Body must be valid JSON.") from None


def get_use_cases(request: Request) -> TamossUseCases:
    return request.app.state.tamoss_use_cases


def get_service_use_cases(request: Request) -> ServiceUseCases:
    return get_use_cases(request).service


def get_webhook_use_cases(request: Request) -> WebhookUseCases:
    return get_use_cases(request).webhooks


def get_deletion_use_cases(request: Request) -> DeletionUseCases:
    return get_use_cases(request).deletion


def get_source_use_cases(request: Request) -> SourceUseCases:
    return get_use_cases(request).sources


def get_flow_use_cases(request: Request) -> FlowUseCases:
    return get_use_cases(request).flows


def get_storage_use_cases(request: Request) -> StorageUseCases:
    return get_use_cases(request).storage


def get_segment_use_cases(request: Request) -> SegmentUseCases:
    return get_use_cases(request).segments


def get_object_use_cases(request: Request) -> ObjectUseCases:
    return get_use_cases(request).objects
