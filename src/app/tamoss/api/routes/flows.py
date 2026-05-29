from __future__ import annotations

from typing import Annotated, Any
from uuid import UUID

from fastapi import APIRouter, Body, Depends, Path, Query, Request, Response, status

from tamoss.api.dependencies import get_deletion_use_cases, get_flow_use_cases
from tamoss.api.presenters import (
    deletion_request_accepted_response,
    flow_response,
    head_response,
    with_page_headers,
)
from tamoss.api.query_params import tag_filter_parameters, validate_query_params
from tamoss.application.contexts.deletion import DeletionUseCases
from tamoss.application.contexts.flows import FlowUseCases
from tamoss.auth import identify_request
from tamoss.contract.generated import contract_models
from tamoss.contract.serialization import contract_dump
from tamoss.domain.tags import TagValue, parse_tag_filters
from tamoss.errors import BadRequest

router = APIRouter(tags=["Flows"])


@router.get(
    "/flows",
    responses={400: {"description": "Bad request. Invalid query options."}},
    dependencies=[Depends(tag_filter_parameters)],
)
@router.head(
    "/flows",
    responses={400: {"description": "Bad request. Invalid query options."}},
    dependencies=[Depends(tag_filter_parameters)],
)
def list_flows(
    request: Request,
    response: Response,
    source_id: UUID | None = None,
    timerange: str | None = None,
    include_timerange: bool = Query(
        default=False,
        description="Include each listed Flow's computed content timerange.",
    ),
    format: str | None = None,
    codec: str | None = None,
    label: str | None = None,
    frame_width: int | None = None,
    frame_height: int | None = None,
    page: str | None = None,
    limit: int | None = Query(default=None, gt=0),
    flows: FlowUseCases = Depends(get_flow_use_cases),
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
    try:
        tag_values, tag_exists = parse_tag_filters(request.query_params)
    except ValueError as exc:
        raise BadRequest("Bad request. Invalid query options.") from exc
    flow_page = flows.list_flows(
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
        flows.flow_timeranges(flow.id for flow in flow_page.items)
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    include_timerange: bool = False,
    timerange: str | None = None,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Any:
    validate_query_params(request, {"include_timerange", "timerange"})
    flow = flows.get_flow(flow_id, include_collected_by=True)
    flow_timerange = None
    if include_timerange or timerange is not None:
        computed_timerange = flows.flow_timerange(flow_id, timerange=timerange)
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    flow: contract_models.Flow,
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Any:
    identity = identify_request(request)
    stored, created = flows.put_flow(
        flow_id=flow_id,
        flow=contract_dump(flow),
        supplied_fields=set(flow.root.model_fields_set),
        identity=identity,
    )
    if not created:
        return Response(status_code=status.HTTP_204_NO_CONTENT)
    return flow_response(stored)


@router.delete(
    "/flows/{flowId}",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        202: {
            "description": "Deletion accepted.",
            "model": contract_models.DeletionRequest,
        },
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow ID in the path is invalid."},
    },
)
def delete_flow(
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    deletion: DeletionUseCases = Depends(get_deletion_use_cases),
) -> Response:
    validate_query_params(request, set())
    identity = identify_request(request)
    delete_request = deletion.delete_flow(flow_id=flow_id, identity=identity)
    if delete_request is not None:
        return deletion_request_accepted_response(delete_request, request)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


@router.get(
    "/flows/{flowId}/flow_collection",
    response_model=list[contract_models.FlowCollectionItem],
    response_model_exclude_none=True,
    responses={404: {"description": "The requested Flow does not exist."}},
)
@router.head(
    "/flows/{flowId}/flow_collection",
    responses={404: {"description": "The requested Flow does not exist."}},
)
def get_flow_collection(
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Any:
    validate_query_params(request, set())
    collection = flows.get_flow_collection(flow_id)
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    collection: list[contract_models.FlowCollectionItem],
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Response:
    identity = identify_request(request)
    flows.set_flow_collection(
        flow_id=flow_id,
        collection=[contract_dump(item) for item in collection],
        identity=identity,
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Response:
    validate_query_params(request, set())
    identity = identify_request(request)
    flows.delete_flow_collection(flow_id=flow_id, identity=identity)
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Any:
    validate_query_params(request, set())
    label = flows.get_flow_property(flow_id, "label")
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    label: str = Body(...),
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Response:
    flows.set_flow_property(
        flow_id,
        "label",
        label,
        identity=identify_request(request),
    )
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Response:
    validate_query_params(request, set())
    flows.delete_flow_property(flow_id, "label", identity=identify_request(request))
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Any:
    validate_query_params(request, set())
    description = flows.get_flow_property(flow_id, "description")
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    description: str = Body(...),
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Response:
    flows.set_flow_property(
        flow_id,
        "description",
        description,
        identity=identify_request(request),
    )
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Response:
    validate_query_params(request, set())
    flows.delete_flow_property(
        flow_id,
        "description",
        identity=identify_request(request),
    )
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Any:
    validate_query_params(request, set())
    avg_bit_rate = flows.get_flow_property(flow_id, "avg_bit_rate")
    if head := head_response(request):
        return head
    return avg_bit_rate


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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    value: int = Body(..., ge=0),
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Response:
    flows.set_flow_property(
        flow_id,
        "avg_bit_rate",
        value,
        identity=identify_request(request),
    )
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Response:
    validate_query_params(request, set())
    flows.delete_flow_property(
        flow_id,
        "avg_bit_rate",
        identity=identify_request(request),
    )
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Any:
    validate_query_params(request, set())
    max_bit_rate = flows.get_flow_property(flow_id, "max_bit_rate")
    if head := head_response(request):
        return head
    return max_bit_rate


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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    value: int = Body(..., ge=0),
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Response:
    flows.set_flow_property(
        flow_id,
        "max_bit_rate",
        value,
        identity=identify_request(request),
    )
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Response:
    validate_query_params(request, set())
    flows.delete_flow_property(
        flow_id,
        "max_bit_rate",
        identity=identify_request(request),
    )
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Any:
    validate_query_params(request, set())
    read_only = flows.get_flow_property(flow_id, "read_only")
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    read_only: bool = Body(...),
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Response:
    flows.set_flow_property(
        flow_id,
        "read_only",
        read_only,
        identity=identify_request(request),
    )
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Any:
    validate_query_params(request, set())
    tags = flows.get_flow_tags(flow_id)
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    name: str,
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Any:
    validate_query_params(request, set())
    tag = flows.get_flow_tag(flow_id, name)
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    name: str,
    request: Request,
    value: TagValue = Body(...),
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Response:
    flows.set_flow_tag(flow_id, name, value, identity=identify_request(request))
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
    flow_id: Annotated[UUID, Path(alias="flowId")],
    name: str,
    request: Request,
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Response:
    validate_query_params(request, set())
    flows.delete_flow_tag(flow_id, name, identity=identify_request(request))
    return Response(status_code=status.HTTP_204_NO_CONTENT)
