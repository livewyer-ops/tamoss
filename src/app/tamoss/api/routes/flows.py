from __future__ import annotations

from typing import Annotated, Any
from uuid import UUID

from fastapi import APIRouter, Body, Depends, Path, Query, Request, Response, status

from tamoss.api.dependencies import (
    get_deletion_use_cases,
    get_flow_use_cases,
    require_json_body,
)
from tamoss.api.presenters import (
    deletion_request_accepted_response,
    flow_response,
    head_response,
    with_page_headers,
)
from tamoss.api.query_params import tag_filter_parameters, validate_query_params
from tamoss.api.routes.scalar_properties import register_scalar_property_routes
from tamoss.application.contexts.deletion import DeletionUseCases
from tamoss.application.contexts.flows import FlowUseCases
from tamoss.auth import identify_request
from tamoss.contract.generated import contract_models
from tamoss.contract.serialization import contract_dump
from tamoss.contract.validation import strict_contract_model
from tamoss.domain.listings import FlowSortBy, parse_collected_by_ids
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
    profile_id: UUID | None = None,
    label: str | None = None,
    reverse_order: bool = False,
    sort_by: FlowSortBy = FlowSortBy.CREATED,
    status: contract_models.FlowStatus | None = None,
    frame_width: int | None = None,
    frame_height: int | None = None,
    init_segments: bool | None = None,
    collected_by_ids: str | None = None,
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
            "profile_id",
            "label",
            "reverse_order",
            "sort_by",
            "status",
            "frame_width",
            "frame_height",
            "init_segments",
            "collected_by_ids",
            "page",
            "limit",
        },
        allowed_prefixes=("tag.", "tag_exists."),
    )
    try:
        tag_values, tag_exists = parse_tag_filters(request.query_params)
    except ValueError as exc:
        raise BadRequest("Bad request. Invalid query options.") from exc
    collected_ids, top_level_only = parse_collected_by_ids(collected_by_ids)
    flow_page = flows.list_flows(
        source_id=source_id,
        timerange=timerange,
        format=format,
        profile_id=profile_id,
        status=status,
        init_segments=init_segments,
        collected_by_ids=collected_ids,
        top_level_only=top_level_only,
        sort_by=sort_by,
        reverse_order=reverse_order,
        codec=codec,
        label=label,
        frame_width=frame_width,
        frame_height=frame_height,
        tag_values=tag_values,
        tag_exists=tag_exists,
        page=page,
        limit=limit,
    )
    with_page_headers(response, request, flow_page, reverse_order=reverse_order)
    if head := head_response(request, response):
        return head
    timeranges = (
        flows.flow_timeranges(
            (flow.id for flow in flow_page.items), seed_flows=flow_page.items
        )
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
    dependencies=[Depends(require_json_body)],
    responses={
        204: {"description": "No content. The Flow has been updated."},
        400: {"description": "Bad request. Invalid Flow JSON."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow ID in the path is invalid."},
    },
)
def put_flow(
    flow_id: Annotated[UUID, Path(alias="flowId")],
    request: Request,
    flow: dict[str, Any] = Body(...),
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Any:
    identity = identify_request(request)
    stored, created = flows.put_flow(
        flow_id=flow_id,
        flow=flow,
        supplied_fields=set(flow),
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
    request: Request,
    collection: object = Body(...),
    flows: FlowUseCases = Depends(get_flow_use_cases),
) -> Response:
    try:
        validated_collection = strict_contract_model(
            contract_models.FlowCollection,
            collection,
            recursive_non_nullable_fields=(
                contract_models.FlowCollectionItem.model_fields
            ),
        )
    except (TypeError, ValueError) as exc:
        raise BadRequest("Bad request. Invalid Flow collection.") from exc
    identity = identify_request(request)
    flows.set_flow_collection(
        flow_id=flow_id,
        collection=contract_dump(validated_collection),
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


def _register_flow_property_routes(
    property_name: str,
    *,
    body_param: str,
    body_type: type,
    body: Any,
    include_delete: bool = True,
    put_forbidden_description: str = "Forbidden.",
) -> None:
    def set_value(
        flows: FlowUseCases, flow_id: UUID, value: Any, request: Request
    ) -> None:
        flows.set_flow_property(
            flow_id,
            property_name,  # type: ignore[arg-type]
            value,
            identity=identify_request(request),
        )

    def delete_value(flows: FlowUseCases, flow_id: UUID, request: Request) -> None:
        flows.delete_flow_property(
            flow_id,
            property_name,  # type: ignore[arg-type]
            identity=identify_request(request),
        )

    register_scalar_property_routes(
        router,
        path=f"/flows/{{flowId}}/{property_name}",
        path_alias="flowId",
        name=f"flow_{property_name}",
        use_cases_dependency=get_flow_use_cases,
        body_param=body_param,
        body_type=body_type,
        body=body,
        get_value=lambda flows, flow_id: flows.get_flow_property(
            flow_id, property_name
        ),
        set_value=set_value,
        delete_value=delete_value if include_delete else None,
        read_responses={404: {"description": "The requested Flow does not exist."}},
        put_responses={
            400: {"description": "Bad request."},
            403: {"description": put_forbidden_description},
            404: {"description": "The requested Flow does not exist."},
        },
        delete_responses={
            403: {"description": "Forbidden."},
            404: {"description": "The requested Flow ID in the path is invalid."},
        }
        if include_delete
        else None,
    )


_register_flow_property_routes(
    "label", body_param="label", body_type=str, body=Body(...)
)
_register_flow_property_routes(
    "description", body_param="description", body_type=str, body=Body(...)
)
_register_flow_property_routes(
    "avg_bit_rate", body_param="value", body_type=int, body=Body(..., ge=0)
)
_register_flow_property_routes(
    "max_bit_rate", body_param="value", body_type=int, body=Body(..., ge=0)
)
_register_flow_property_routes(
    "read_only",
    body_param="read_only",
    body_type=bool,
    body=Body(...),
    include_delete=False,
    put_forbidden_description=(
        "Forbidden. You do not have permission to modify this Flow."
    ),
)


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
    dependencies=[Depends(require_json_body)],
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
