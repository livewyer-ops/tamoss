from __future__ import annotations

from typing import Any
from uuid import UUID

from fastapi import APIRouter, Depends, Query, Request, Response, status
from fastapi.responses import JSONResponse

from tamoss.api.dependencies import get_use_cases
from tamoss.api.presenters import (
    deletion_request_accepted_response,
    head_response,
    segment_response,
    with_page_headers,
)
from tamoss.api.query_params import (
    parse_get_url_labels,
    parse_storage_ids,
    validate_query_params,
)
from tamoss.api.schemas import (
    DeletionRequestResponse,
    FailedSegment,
    FailedSegmentsResponse,
    FlowSegmentPost,
)
from tamoss.application.use_cases import TamossUseCases
from tamoss.auth import identify_request
from tamoss.errors import BadRequest, error_payload

router = APIRouter(tags=["FlowSegments"])


@router.get(
    "/flows/{flowId}/segments",
    responses={
        400: {"description": "Bad request. Invalid query options."},
        404: {"description": "The Flow ID in the path is invalid."},
    },
)
@router.head(
    "/flows/{flowId}/segments",
    responses={
        400: {"description": "Bad request. Invalid query options."},
        404: {"description": "The Flow ID in the path is invalid."},
    },
)
def list_segments(
    flowId: UUID,
    request: Request,
    response: Response,
    object_id: str | None = None,
    timerange: str | None = None,
    reverse_order: bool = False,
    verbose_storage: bool = False,
    accept_get_urls: str | None = None,
    accept_storage_ids: str | None = None,
    presigned: bool | None = None,
    include_object_timerange: bool = False,
    page: str | None = None,
    limit: int | None = Query(default=None, gt=0),
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    validate_query_params(
        request,
        {
            "object_id",
            "timerange",
            "reverse_order",
            "verbose_storage",
            "accept_get_urls",
            "accept_storage_ids",
            "presigned",
            "include_object_timerange",
            "page",
            "limit",
        },
    )
    accepted_labels = parse_get_url_labels(accept_get_urls)
    accepted_storage_ids = parse_storage_ids(accept_storage_ids)
    segment_page = use_cases.list_segments(
        flow_id=flowId,
        object_id=object_id,
        timerange=timerange,
        reverse_order=reverse_order,
        page=page,
        limit=limit,
    )
    with_page_headers(response, request, segment_page)
    response.headers["X-Paging-Reverse-Order"] = str(reverse_order).lower()
    response.headers["X-Paging-Timerange"] = segment_page.timerange or "()"
    if head := head_response(request, response):
        return head
    include_get_urls = accepted_labels != set()
    objects_by_id = (
        use_cases.repository.get_objects(
            segment.object_id for segment in segment_page.items
        )
        if include_get_urls
        else {}
    )
    return [
        segment_response(
            segment,
            use_cases.object_get_urls(
                media_object,
                accept_get_urls=accepted_labels,
                accept_storage_ids=accepted_storage_ids,
                presigned=presigned,
                verbose_storage=verbose_storage,
            )
            if (media_object := objects_by_id.get(segment.object_id)) is not None
            else [],
            include_object_timerange=include_object_timerange,
        )
        for segment in segment_page.items
    ]


@router.post(
    "/flows/{flowId}/segments",
    status_code=status.HTTP_201_CREATED,
    response_class=Response,
    responses={
        200: {
            "description": "Some Flow Segments failed validation.",
            "model": FailedSegmentsResponse,
        },
        400: {"description": "Bad request. Invalid Flow Segment JSON."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow does not exist."},
    },
)
def post_segments(
    flowId: UUID,
    body: FlowSegmentPost | list[FlowSegmentPost],
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Any:
    if isinstance(body, list):
        failed: list[FailedSegment] = []
        for segment, result in zip(
            body, use_cases.register_segments(flow_id=flowId, segment_posts=body)
        ):
            if result.error:
                failed.append(
                    FailedSegment(
                        object_id=segment.object_id,
                        timerange=segment.timerange,
                        error=error_payload("TAMSError", result.error),
                    )
                )
        if failed:
            return JSONResponse(
                status_code=status.HTTP_200_OK,
                content=FailedSegmentsResponse(failed_segments=failed).model_dump(
                    exclude_none=True, mode="json"
                ),
            )
        return Response(status_code=status.HTTP_201_CREATED)

    result = use_cases.register_segment(flow_id=flowId, segment_post=body)
    if result.error:
        raise BadRequest(result.error)
    return Response(status_code=status.HTTP_201_CREATED)


@router.delete(
    "/flows/{flowId}/segments",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        202: {
            "description": "Deletion accepted.",
            "model": DeletionRequestResponse,
        },
        400: {"description": "Bad request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow ID in the path is invalid."},
    },
)
def delete_segments(
    flowId: UUID,
    request: Request,
    timerange: str | None = None,
    object_id: str | None = None,
    use_cases: TamossUseCases = Depends(get_use_cases),
) -> Response:
    validate_query_params(request, {"timerange", "object_id"})
    identity = identify_request(request)
    delete_request = use_cases.delete_segments(
        flow_id=flowId,
        timerange=timerange,
        object_id=object_id,
        identity=identity,
    )
    if delete_request is not None:
        return deletion_request_accepted_response(delete_request, request)
    return Response(status_code=status.HTTP_204_NO_CONTENT)
