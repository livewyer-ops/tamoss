from __future__ import annotations

from dataclasses import dataclass

from fastapi import FastAPI
from tamoss.application.use_cases import TamossUseCases
from tamoss.domain.model import WebhookDeliveryRecord


@dataclass(frozen=True)
class WebhookResponse:
    status_code: int
    reason: str


def route_worker_to_app(tamoss_app: FastAPI) -> TamossUseCases:
    return tamoss_app.state.tamoss_use_cases


def only_delivery(use_cases: TamossUseCases) -> WebhookDeliveryRecord:
    deliveries = use_cases.repository.list_webhook_deliveries()
    assert len(deliveries) == 1
    return deliveries[0]
