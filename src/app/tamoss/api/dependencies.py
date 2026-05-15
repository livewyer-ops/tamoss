from __future__ import annotations

from fastapi import Request

from tamoss.application.use_cases import TamossUseCases


def get_use_cases(request: Request) -> TamossUseCases:
    return request.app.state.tamoss_use_cases
