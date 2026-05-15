from __future__ import annotations

from typing import Any
from uuid import UUID

from fastapi import APIRouter, Body, Depends, Query, Request, Response, status

from tamoss.api.dependencies import get_use_cases
from tamoss.api.presenters import head_response, source_response, with_page_headers
from tamoss.api.query_params import validate_query_params
from tamoss.application.use_cases import SourceRelationships, TamossUseCases
from tamoss.domain.model import SourceRecord
from tamoss.domain.tags import TagValue, parse_tag_filters

router = APIRouter(tags=["Sources"])


def _source_response(
    source: SourceRecord, *, relationships: dict[UUID, SourceRelationships]
) -> dict:
    relationship = relationships.get(source.id)
    return source_response(
        source,
        source_collection=relationship.source_collection if relationship else None,
        collected_by=relationship.collected_by if relationship else None,
    )


@router.get(
    "/sources",
    responses={400: {"description": "Bad request. Invalid query options."}},
)
@router.head(
    "/sources",
    responses={400: {"description": "Bad request. Invalid query options."}},
)
def list_sources(
    request: Request,
    response: Response,
    label: str | None = None,
    format: str | None = None,
    tag_name: str | None = Query(default=None, alias="tag.{name}"),
    tag_exists_name: bool | None = Query(default=None, alias="tag_exists.{name}"),
    page: str | None = None,
    limit: int | None = Query(default=None, gt=0),
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(
        request,
        {"label", "format", "page", "limit"},
        allowed_prefixes=("tag.", "tag_exists."),
    )
    tag_values, tag_exists = parse_tag_filters(request.query_params)
    source_page = use_cases.list_sources(
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
    relationships = use_cases.source_relationships(
        source.id for source in source_page.items
    )
    return [
        _source_response(source, relationships=relationships)
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
    sourceId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    source = use_cases.get_source(sourceId)
    if head := head_response(request):
        return head
    return _source_response(
        source, relationships=use_cases.source_relationships([source.id])
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
    sourceId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    label = use_cases.get_source_property(sourceId, "label")
    if head := head_response(request):
        return head
    return label


@router.put(
    "/sources/{sourceId}/label",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        400: {"description": "Bad request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Source does not exist."},
    },
)
def put_source_label(
    sourceId: UUID,
    label: str = Body(...),
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    use_cases.set_source_property(sourceId, "label", label)
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
    sourceId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    validate_query_params(request, set())
    use_cases.delete_source_property(sourceId, "label")
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
    sourceId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    description = use_cases.get_source_property(sourceId, "description")
    if head := head_response(request):
        return head
    return description


@router.put(
    "/sources/{sourceId}/description",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        400: {"description": "Bad request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Source does not exist."},
    },
)
def put_source_description(
    sourceId: UUID,
    description: str = Body(...),
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    use_cases.set_source_property(sourceId, "description", description)
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
    sourceId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    validate_query_params(request, set())
    use_cases.delete_source_property(sourceId, "description")
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
    sourceId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    tags = use_cases.get_source_tags(sourceId)
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
    sourceId: UUID,
    name: str,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    tag = use_cases.get_source_tag(sourceId, name)
    if head := head_response(request):
        return head
    return tag


@router.put(
    "/sources/{sourceId}/tags/{name:path}",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        400: {"description": "Bad request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Source does not exist."},
    },
)
def put_source_tag(
    sourceId: UUID,
    name: str,
    value: TagValue = Body(...),
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    use_cases.set_source_tag(sourceId, name, value)
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
    sourceId: UUID,
    name: str,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    validate_query_params(request, set())
    use_cases.delete_source_tag(sourceId, name)
    return Response(status_code=status.HTTP_204_NO_CONTENT)
