from __future__ import annotations

from typing import Annotated, Any
from uuid import UUID

from fastapi import APIRouter, Body, Depends, Path, Query, Request, Response, status

from tamoss.api.dependencies import get_source_use_cases, require_json_body
from tamoss.api.presenters import (
    head_response,
    source_response_with_relationships,
    with_page_headers,
)
from tamoss.api.query_params import tag_filter_parameters, validate_query_params
from tamoss.application.contexts.sources import SourceUseCases
from tamoss.domain.tags import TagValue, parse_tag_filters
from tamoss.errors import BadRequest

router = APIRouter(tags=["Sources"])


@router.get(
    "/sources",
    responses={400: {"description": "Bad request. Invalid query options."}},
    dependencies=[Depends(tag_filter_parameters)],
)
@router.head(
    "/sources",
    responses={400: {"description": "Bad request. Invalid query options."}},
    dependencies=[Depends(tag_filter_parameters)],
)
def list_sources(
    request: Request,
    response: Response,
    label: str | None = None,
    format: str | None = None,
    page: str | None = None,
    limit: int | None = Query(default=None, gt=0),
    sources: SourceUseCases = Depends(get_source_use_cases),
) -> Any:
    validate_query_params(
        request,
        {"label", "format", "page", "limit"},
        allowed_prefixes=("tag.", "tag_exists."),
    )
    try:
        tag_values, tag_exists = parse_tag_filters(request.query_params)
    except ValueError as exc:
        raise BadRequest("Bad request. Invalid query options.") from exc
    source_page = sources.list_sources(
        label=label,
        format=format,
        tag_values=tag_values,
        tag_exists=tag_exists,
        page=page,
        limit=limit,
    )
    with_page_headers(response, request, source_page)
    if head := head_response(request, response):
        return head
    relationships = sources.source_relationships(
        source.id for source in source_page.items
    )
    return [
        source_response_with_relationships(source, relationships=relationships)
        for source in source_page.items
    ]


@router.get(
    "/sources/{sourceId}",
    responses={404: {"description": "The requested Source does not exist."}},
)
@router.head(
    "/sources/{sourceId}",
    responses={404: {"description": "The requested Source does not exist."}},
)
def get_source(
    source_id: Annotated[UUID, Path(alias="sourceId")],
    request: Request,
    sources: SourceUseCases = Depends(get_source_use_cases),
) -> Any:
    validate_query_params(request, set())
    source = sources.get_source(source_id)
    if head := head_response(request):
        return head
    return source_response_with_relationships(
        source, relationships=sources.source_relationships([source.id])
    )


@router.get(
    "/sources/{sourceId}/label",
    responses={404: {"description": "The requested Source does not exist."}},
)
@router.head(
    "/sources/{sourceId}/label",
    responses={404: {"description": "The requested Source does not exist."}},
)
def get_source_label(
    source_id: Annotated[UUID, Path(alias="sourceId")],
    request: Request,
    sources: SourceUseCases = Depends(get_source_use_cases),
) -> Any:
    validate_query_params(request, set())
    label = sources.get_source_property(source_id, "label")
    if head := head_response(request):
        return head
    return label


@router.put(
    "/sources/{sourceId}/label",
    dependencies=[Depends(require_json_body)],
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        400: {"description": "Bad request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Source does not exist."},
    },
)
def put_source_label(
    source_id: Annotated[UUID, Path(alias="sourceId")],
    label: str = Body(...),
    sources: SourceUseCases = Depends(get_source_use_cases),
) -> Response:
    sources.set_source_property(source_id, "label", label)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.delete(
    "/sources/{sourceId}/label",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        403: {"description": "Forbidden."},
        404: {"description": "The requested Source does not exist."},
    },
)
def delete_source_label(
    source_id: Annotated[UUID, Path(alias="sourceId")],
    request: Request,
    sources: SourceUseCases = Depends(get_source_use_cases),
) -> Response:
    validate_query_params(request, set())
    sources.delete_source_property(source_id, "label")
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.get(
    "/sources/{sourceId}/description",
    responses={404: {"description": "The requested Source does not exist."}},
)
@router.head(
    "/sources/{sourceId}/description",
    responses={404: {"description": "The requested Source does not exist."}},
)
def get_source_description(
    source_id: Annotated[UUID, Path(alias="sourceId")],
    request: Request,
    sources: SourceUseCases = Depends(get_source_use_cases),
) -> Any:
    validate_query_params(request, set())
    description = sources.get_source_property(source_id, "description")
    if head := head_response(request):
        return head
    return description


@router.put(
    "/sources/{sourceId}/description",
    dependencies=[Depends(require_json_body)],
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        400: {"description": "Bad request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Source does not exist."},
    },
)
def put_source_description(
    source_id: Annotated[UUID, Path(alias="sourceId")],
    description: str = Body(...),
    sources: SourceUseCases = Depends(get_source_use_cases),
) -> Response:
    sources.set_source_property(source_id, "description", description)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.delete(
    "/sources/{sourceId}/description",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        403: {"description": "Forbidden."},
        404: {"description": "The requested Source does not exist."},
    },
)
def delete_source_description(
    source_id: Annotated[UUID, Path(alias="sourceId")],
    request: Request,
    sources: SourceUseCases = Depends(get_source_use_cases),
) -> Response:
    validate_query_params(request, set())
    sources.delete_source_property(source_id, "description")
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.get(
    "/sources/{sourceId}/tags",
    responses={404: {"description": "The requested Source does not exist."}},
)
@router.head(
    "/sources/{sourceId}/tags",
    responses={404: {"description": "The requested Source does not exist."}},
)
def get_source_tags(
    source_id: Annotated[UUID, Path(alias="sourceId")],
    request: Request,
    sources: SourceUseCases = Depends(get_source_use_cases),
) -> Any:
    validate_query_params(request, set())
    tags = sources.get_source_tags(source_id)
    if head := head_response(request):
        return head
    return tags


@router.get(
    "/sources/{sourceId}/tags/{name:path}",
    responses={404: {"description": "The requested Source tag does not exist."}},
)
@router.head(
    "/sources/{sourceId}/tags/{name:path}",
    responses={404: {"description": "The requested Source tag does not exist."}},
)
def get_source_tag(
    source_id: Annotated[UUID, Path(alias="sourceId")],
    name: str,
    request: Request,
    sources: SourceUseCases = Depends(get_source_use_cases),
) -> Any:
    validate_query_params(request, set())
    tag = sources.get_source_tag(source_id, name)
    if head := head_response(request):
        return head
    return tag


@router.put(
    "/sources/{sourceId}/tags/{name:path}",
    dependencies=[Depends(require_json_body)],
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        400: {"description": "Bad request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Source does not exist."},
    },
)
def put_source_tag(
    source_id: Annotated[UUID, Path(alias="sourceId")],
    name: str,
    value: TagValue = Body(...),
    sources: SourceUseCases = Depends(get_source_use_cases),
) -> Response:
    sources.set_source_tag(source_id, name, value)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.delete(
    "/sources/{sourceId}/tags/{name:path}",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        403: {"description": "Forbidden."},
        404: {"description": "The requested Source ID in the path is invalid."},
    },
)
def delete_source_tag(
    source_id: Annotated[UUID, Path(alias="sourceId")],
    name: str,
    request: Request,
    sources: SourceUseCases = Depends(get_source_use_cases),
) -> Response:
    validate_query_params(request, set())
    sources.delete_source_tag(source_id, name)
    return Response(status_code=status.HTTP_204_NO_CONTENT)
