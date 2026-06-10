"""Registration factory for the scalar property routes on Flows and Sources.

The ``label`` / ``description`` / ``avg_bit_rate`` / ``max_bit_rate`` /
``read_only`` endpoints all follow the same GET+HEAD / PUT / optional DELETE
shape. This module registers those routes from a single template while
preserving the exact per-route metadata (names, response descriptions and
body validation) of the previously hand-written endpoints.

This module intentionally avoids ``from __future__ import annotations`` so
endpoint annotations are evaluated eagerly against the factory's closure.
"""

import inspect
from collections.abc import Callable
from typing import Annotated, Any
from uuid import UUID

from fastapi import APIRouter, Depends, Path, Request, Response, status

from tamoss.api.dependencies import require_json_body
from tamoss.api.presenters import head_response
from tamoss.api.query_params import validate_query_params

GetValue = Callable[[Any, UUID], Any]
SetValue = Callable[[Any, UUID, Any, Request], None]
DeleteValue = Callable[[Any, UUID, Request], None]
Responses = dict[int | str, dict[str, Any]]


def register_scalar_property_routes(
    router: APIRouter,
    *,
    path: str,
    path_alias: str,
    name: str,
    use_cases_dependency: Callable[..., Any],
    body_param: str,
    body_type: type,
    body: Any,
    get_value: GetValue,
    set_value: SetValue,
    delete_value: DeleteValue | None,
    read_responses: Responses,
    put_responses: Responses,
    delete_responses: Responses | None,
) -> None:
    """Register GET/HEAD, PUT and optional DELETE routes for one property."""

    def read_property(
        entity_id: Annotated[UUID, Path(alias=path_alias)],
        request: Request,
        use_cases: Any = Depends(use_cases_dependency),
    ) -> Any:
        validate_query_params(request, set())
        value = get_value(use_cases, entity_id)
        if head := head_response(request):
            return head
        return value

    read_property.__name__ = f"get_{name}"
    router.head(path, responses=read_responses)(read_property)
    router.get(path, responses=read_responses)(read_property)

    def put_property(**kwargs: Any) -> Response:
        set_value(
            kwargs["use_cases"],
            kwargs["entity_id"],
            kwargs[body_param],
            kwargs["request"],
        )
        return Response(status_code=status.HTTP_204_NO_CONTENT)

    # The body parameter keeps its property-specific name (it titles the
    # request-body schema), so the published signature is built explicitly
    # and the endpoint receives the arguments via **kwargs.
    put_property.__signature__ = inspect.Signature(  # type: ignore[attr-defined]
        [
            inspect.Parameter(
                "entity_id",
                inspect.Parameter.POSITIONAL_OR_KEYWORD,
                annotation=Annotated[UUID, Path(alias=path_alias)],
            ),
            inspect.Parameter(
                "request",
                inspect.Parameter.POSITIONAL_OR_KEYWORD,
                annotation=Request,
            ),
            inspect.Parameter(
                body_param,
                inspect.Parameter.POSITIONAL_OR_KEYWORD,
                annotation=body_type,
                default=body,
            ),
            inspect.Parameter(
                "use_cases",
                inspect.Parameter.POSITIONAL_OR_KEYWORD,
                annotation=Any,
                default=Depends(use_cases_dependency),
            ),
        ],
        return_annotation=Response,
    )
    put_property.__name__ = f"put_{name}"
    router.put(
        path,
        dependencies=[Depends(require_json_body)],
        status_code=status.HTTP_204_NO_CONTENT,
        responses=put_responses,
    )(put_property)

    if delete_value is None:
        return

    def delete_property(
        entity_id: Annotated[UUID, Path(alias=path_alias)],
        request: Request,
        use_cases: Any = Depends(use_cases_dependency),
    ) -> Response:
        validate_query_params(request, set())
        delete_value(use_cases, entity_id, request)
        return Response(status_code=status.HTTP_204_NO_CONTENT)

    delete_property.__name__ = f"delete_{name}"
    router.delete(
        path,
        status_code=status.HTTP_204_NO_CONTENT,
        responses=delete_responses,
    )(delete_property)
