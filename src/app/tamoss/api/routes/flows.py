from __future__ import annotations

from typing import Any
from uuid import UUID

from fastapi import APIRouter, Body, Depends, Query, Request, Response, status

from tamoss.api.dependencies import get_use_cases
from tamoss.api.presenters import (
    deletion_request_accepted_response,
    flow_response,
    head_response,
    with_page_headers,
)
from tamoss.api.query_params import validate_query_params
from tamoss.api.schemas import DeletionRequestResponse, FlowCollectionItem, FlowWrite
from tamoss.application.use_cases import TamossUseCases
from tamoss.auth import identify_request
from tamoss.domain.tags import TagValue, parse_tag_filters

router = APIRouter(tags=["Flows"])


@router.get(
    "/flows",
    responses={400: {"description": "Bad request. Invalid query options."}},
)
@router.head(
    "/flows",
    responses={400: {"description": "Bad request. Invalid query options."}},
)
def list_flows(
    request: Request,
    response: Response,
    source_id: UUID | None = None,
    timerange: str | None = None,
    include_timerange: bool = Query(
        default=False,
        description=(
            "TAMOSS extension. Include each listed Flow's computed content "
            "timerange in the response."
        ),
    ),
    format: str | None = None,
    codec: str | None = None,
    label: str | None = None,
    frame_width: int | None = None,
    frame_height: int | None = None,
    tag_name: str | None = Query(default=None, alias="tag.{name}"),
    tag_exists_name: bool | None = Query(default=None, alias="tag_exists.{name}"),
    page: str | None = None,
    limit: int | None = Query(default=None, gt=0),
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(
        request,
        {
            "source_id",
            "timerange",
            "include_timerange",
            "format",
            "codec",
            "label",
            "frame_width",
            "frame_height",
            "page",
            "limit",
        },
        allowed_prefixes=("tag.", "tag_exists."),
    )
    tag_values, tag_exists = parse_tag_filters(request.query_params)
    flow_page = use_cases.list_flows(
        source_id=source_id,
        timerange=timerange,
        format=format,
        codec=codec,
        label=label,
        frame_width=frame_width,
        frame_height=frame_height,
        tag_values=tag_values,
        tag_exists=tag_exists,
        page=page,
        limit=limit,
    )
    with_page_headers(response, request, flow_page)
    if head := head_response(request, response):
        return head
    timeranges = (
        use_cases.flow_timeranges(flow.id for flow in flow_page.items)
        if include_timerange
        else {}
    )
    return [
        flow_response(flow, timerange=timeranges.get(flow.id))
        for flow in flow_page.items
    ]


@router.get(
    "/flows/{flowId}",
    responses={
        400: {"description": "Bad request. Invalid query options."},
        404: {"description": "The requested Flow does not exist."},
    },
)
@router.head(
    "/flows/{flowId}",
    responses={
        400: {"description": "Bad request. Invalid query options."},
        404: {"description": "The requested Flow does not exist."},
    },
)
def get_flow(
    flowId: UUID,
    request: Request,
    include_timerange: bool = False,
    timerange: str | None = None,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, {"include_timerange", "timerange"})
    flow = use_cases.get_flow(flowId, include_collected_by=True)
    flow_timerange = None
    if include_timerange or timerange is not None:
        computed_timerange = use_cases.flow_timerange(flowId, timerange=timerange)
        if include_timerange:
            flow_timerange = computed_timerange
    if head := head_response(request):
        return head
    return flow_response(flow, timerange=flow_timerange)


@router.put(
    "/flows/{flowId}",
    status_code=status.HTTP_201_CREATED,
    responses={
        204: {"description": "No content. The Flow has been updated."},
        400: {"description": "Bad request. Invalid Flow JSON."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow ID in the path is invalid."},
    },
)
def put_flow(
    flowId: UUID,
    flow: FlowWrite,
    request: Request,
    response: Response,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    identity = identify_request(request)
    stored, created = use_cases.put_flow(
        flow_id=flowId, flow_write=flow, identity=identity
    )
    if not created:
        response.status_code = status.HTTP_204_NO_CONTENT
        return Response(status_code=status.HTTP_204_NO_CONTENT)
    return flow_response(stored)


@router.delete(
    "/flows/{flowId}",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        202: {
            "description": "Deletion accepted.",
            "model": DeletionRequestResponse,
        },
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow ID in the path is invalid."},
    },
)
def delete_flow(
    flowId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    validate_query_params(request, set())
    identity = identify_request(request)
    delete_request = use_cases.delete_flow(flow_id=flowId, identity=identity)
    if delete_request is not None:
        return deletion_request_accepted_response(delete_request, request)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.get(
    "/flows/{flowId}/flow_collection",
    response_model=list[FlowCollectionItem],
    response_model_exclude_none=True,
    responses={404: {"description": "The requested Flow does not exist."}},
)
@router.head(
    "/flows/{flowId}/flow_collection",
    responses={404: {"description": "The requested Flow does not exist."}},
)
def get_flow_collection(
    flowId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    collection = use_cases.get_flow_collection(flowId)
    if head := head_response(request):
        return head
    return collection


@router.put(
    "/flows/{flowId}/flow_collection",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        400: {"description": "Bad request. Invalid Flow collection."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow does not exist."},
    },
)
def put_flow_collection(
    flowId: UUID,
    collection: list[FlowCollectionItem],
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    identity = identify_request(request)
    use_cases.set_flow_collection(
        flow_id=flowId, collection=collection, identity=identity
    )
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.delete(
    "/flows/{flowId}/flow_collection",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow ID in the path is invalid."},
    },
)
def delete_flow_collection(
    flowId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    validate_query_params(request, set())
    identity = identify_request(request)
    use_cases.delete_flow_collection(flow_id=flowId, identity=identity)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.get(
    "/flows/{flowId}/label",
    responses={404: {"description": "The requested Flow does not exist."}},
)
@router.head(
    "/flows/{flowId}/label",
    responses={404: {"description": "The requested Flow does not exist."}},
)
def get_flow_label(
    flowId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    label = use_cases.get_flow_property(flowId, "label")
    if head := head_response(request):
        return head
    return label


@router.put(
    "/flows/{flowId}/label",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        400: {"description": "Bad request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow does not exist."},
    },
)
def put_flow_label(
    flowId: UUID,
    label: str = Body(...),
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    use_cases.set_flow_property(flowId, "label", label)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.delete(
    "/flows/{flowId}/label",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow ID in the path is invalid."},
    },
)
def delete_flow_label(
    flowId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    validate_query_params(request, set())
    use_cases.delete_flow_property(flowId, "label")
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.get(
    "/flows/{flowId}/description",
    responses={404: {"description": "The requested Flow does not exist."}},
)
@router.head(
    "/flows/{flowId}/description",
    responses={404: {"description": "The requested Flow does not exist."}},
)
def get_flow_description(
    flowId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    description = use_cases.get_flow_property(flowId, "description")
    if head := head_response(request):
        return head
    return description


@router.put(
    "/flows/{flowId}/description",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        400: {"description": "Bad request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow does not exist."},
    },
)
def put_flow_description(
    flowId: UUID,
    description: str = Body(...),
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    use_cases.set_flow_property(flowId, "description", description)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.delete(
    "/flows/{flowId}/description",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow ID in the path is invalid."},
    },
)
def delete_flow_description(
    flowId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    validate_query_params(request, set())
    use_cases.delete_flow_property(flowId, "description")
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.get(
    "/flows/{flowId}/avg_bit_rate",
    responses={404: {"description": "The requested Flow does not exist."}},
)
@router.head(
    "/flows/{flowId}/avg_bit_rate",
    responses={404: {"description": "The requested Flow does not exist."}},
)
def get_flow_avg_bit_rate(
    flowId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    value = use_cases.get_flow_int_property(flowId, "avg_bit_rate")
    if head := head_response(request):
        return head
    return value


@router.put(
    "/flows/{flowId}/avg_bit_rate",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        400: {"description": "Bad request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow does not exist."},
    },
)
def put_flow_avg_bit_rate(
    flowId: UUID,
    value: int = Body(..., ge=0),
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    use_cases.set_flow_int_property(flowId, "avg_bit_rate", value)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.delete(
    "/flows/{flowId}/avg_bit_rate",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow ID in the path is invalid."},
    },
)
def delete_flow_avg_bit_rate(
    flowId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    validate_query_params(request, set())
    use_cases.delete_flow_property(flowId, "avg_bit_rate")
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.get(
    "/flows/{flowId}/max_bit_rate",
    responses={404: {"description": "The requested Flow does not exist."}},
)
@router.head(
    "/flows/{flowId}/max_bit_rate",
    responses={404: {"description": "The requested Flow does not exist."}},
)
def get_flow_max_bit_rate(
    flowId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    value = use_cases.get_flow_int_property(flowId, "max_bit_rate")
    if head := head_response(request):
        return head
    return value


@router.put(
    "/flows/{flowId}/max_bit_rate",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        400: {"description": "Bad request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow does not exist."},
    },
)
def put_flow_max_bit_rate(
    flowId: UUID,
    value: int = Body(..., ge=0),
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    use_cases.set_flow_int_property(flowId, "max_bit_rate", value)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.delete(
    "/flows/{flowId}/max_bit_rate",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow ID in the path is invalid."},
    },
)
def delete_flow_max_bit_rate(
    flowId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    validate_query_params(request, set())
    use_cases.delete_flow_property(flowId, "max_bit_rate")
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.get(
    "/flows/{flowId}/read_only",
    responses={404: {"description": "The requested Flow does not exist."}},
)
@router.head(
    "/flows/{flowId}/read_only",
    responses={404: {"description": "The requested Flow does not exist."}},
)
def get_flow_read_only(
    flowId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    read_only = use_cases.get_flow(flowId).read_only
    if head := head_response(request):
        return head
    return read_only


@router.put(
    "/flows/{flowId}/read_only",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        400: {"description": "Bad request."},
        403: {
            "description": "Forbidden. You do not have permission to modify this Flow."
        },
        404: {"description": "The requested Flow does not exist."},
    },
)
def put_flow_read_only(
    flowId: UUID,
    read_only: bool = Body(...),
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    use_cases.set_flow_read_only(flowId, read_only)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.get(
    "/flows/{flowId}/tags",
    responses={404: {"description": "The requested Flow does not exist."}},
)
@router.head(
    "/flows/{flowId}/tags",
    responses={404: {"description": "The requested Flow does not exist."}},
)
def get_flow_tags(
    flowId: UUID,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    tags = use_cases.get_flow_tags(flowId)
    if head := head_response(request):
        return head
    return tags


@router.get(
    "/flows/{flowId}/tags/{name:path}",
    responses={404: {"description": "The requested Flow tag does not exist."}},
)
@router.head(
    "/flows/{flowId}/tags/{name:path}",
    responses={404: {"description": "The requested Flow tag does not exist."}},
)
def get_flow_tag(
    flowId: UUID,
    name: str,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(request, set())
    tag = use_cases.get_flow_tag(flowId, name)
    if head := head_response(request):
        return head
    return tag


@router.put(
    "/flows/{flowId}/tags/{name:path}",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        400: {"description": "Bad request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow does not exist."},
    },
)
def put_flow_tag(
    flowId: UUID,
    name: str,
    value: TagValue = Body(...),
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    use_cases.set_flow_tag(flowId, name, value)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.delete(
    "/flows/{flowId}/tags/{name:path}",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow ID in the path is invalid."},
    },
)
def delete_flow_tag(
    flowId: UUID,
    name: str,
    request: Request,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    validate_query_params(request, set())
    use_cases.delete_flow_tag(flowId, name)
    return Response(status_code=status.HTTP_204_NO_CONTENT)
