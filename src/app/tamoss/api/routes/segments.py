from __future__ import annotations

from typing import Annotated, Any
from uuid import UUID

from fastapi import APIRouter, Depends, Path, Query, Request, Response, status
from fastapi.responses import JSONResponse
from mediatimestamp import TimeRange, Timestamp

from tamoss import metrics
from tamoss.api.dependencies import get_deletion_use_cases, get_segment_use_cases
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
from tamoss.application.contexts.deletion import DeletionUseCases
from tamoss.application.contexts.segments import SegmentUseCases
from tamoss.auth import identify_request
from tamoss.contract.generated import contract_models
from tamoss.domain.model import SegmentRecord
from tamoss.domain.timeranges import finite_normalized_timerange_bounds
from tamoss.errors import BadRequest, NotFound, error_payload

router = APIRouter(tags=["FlowSegments"])


def _flow_id_or_404(value: str, message: str) -> UUID:
    # The spec documents only 404 for this path parameter, so malformed
    # Flow IDs resolve to 404 rather than the 400 used elsewhere.
    try:
        return UUID(value)
    except ValueError:
        raise NotFound(message) from None


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
    flow_id_path: Annotated[str, Path(alias="flowId")],
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
    segments: SegmentUseCases = Depends(get_segment_use_cases),
) -> Any:
    flow_id = _flow_id_or_404(flow_id_path, "The Flow ID in the path is invalid.")
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
    segment_page = segments.list_segments(
        flow_id=flow_id,
        object_id=object_id,
        timerange=timerange,
        reverse_order=reverse_order,
        page=page,
        limit=limit,
    )
    with_page_headers(response, request, segment_page)
    response.headers["X-Paging-Reverse-Order"] = str(reverse_order).lower()
    response.headers["X-Paging-Timerange"] = segment_page.timerange or "()"
    coverage_gaps = segment_coverage_gaps(segment_page.items)
    if coverage_gaps:
        response.headers["X-TAMOSS-Coverage-Gaps"] = ",".join(coverage_gaps)
    if head := head_response(request, response):
        return head
    get_urls_by_object_id = segments.segment_get_urls(
        segment_page.items,
        accept_get_urls=accepted_labels,
        accept_storage_ids=accepted_storage_ids,
        presigned=presigned,
        verbose_storage=verbose_storage,
    )
    return [
        segment_response(
            segment,
            get_urls_by_object_id.get(segment.object_id, []),
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
            "model": contract_models.FlowSegmentBulkFailure,
        },
        400: {"description": "Bad request. Invalid Flow Segment JSON."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow does not exist."},
    },
)
def post_segments(
    flow_id_path: Annotated[str, Path(alias="flowId")],
    body: contract_models.FlowSegmentPost | list[contract_models.FlowSegmentPost],
    segments: SegmentUseCases = Depends(get_segment_use_cases),
) -> Any:
    flow_id = _flow_id_or_404(flow_id_path, "The requested Flow does not exist.")
    # The validated models are handed to the use case directly so the payload
    # is not re-validated against the contract a second time.
    if isinstance(body, list):
        metrics.observe_segment_ingest_batch(len(body))
        failed: list[contract_models.FailedSegment] = []
        for segment, result in zip(
            body,
            segments.register_segments(flow_id=flow_id, segment_posts=body),
            strict=True,
        ):
            if result.error:
                failed.append(
                    contract_models.FailedSegment(
                        object_id=segment.object_id,
                        timerange=segment.timerange,
                        error=error_payload("TAMSError", result.error),
                    )
                )
        metrics.record_segment_ingest_failures(len(failed))
        metrics.record_segments_ingested(len(body) - len(failed))
        if failed:
            return JSONResponse(
                status_code=status.HTTP_200_OK,
                content=contract_models.FlowSegmentBulkFailure(
                    failed_segments=failed
                ).model_dump(exclude_none=True, mode="json"),
            )
        return Response(status_code=status.HTTP_201_CREATED)

    metrics.observe_segment_ingest_batch(1)
    result = segments.register_segment(flow_id=flow_id, segment_post=body)
    if result.error:
        metrics.record_segment_ingest_failures(1)
        raise BadRequest(result.error)
    metrics.record_segments_ingested(1)
    return Response(status_code=status.HTTP_201_CREATED)


@router.delete(
    "/flows/{flowId}/segments",
    status_code=status.HTTP_204_NO_CONTENT,
    responses={
        202: {
            "description": "Deletion accepted.",
            "model": contract_models.DeletionRequest,
        },
        400: {"description": "Bad request."},
        403: {"description": "Forbidden."},
        404: {"description": "The requested Flow ID in the path is invalid."},
    },
)
def delete_segments(
    flow_id_path: Annotated[str, Path(alias="flowId")],
    request: Request,
    timerange: str | None = None,
    object_id: str | None = None,
    deletion: DeletionUseCases = Depends(get_deletion_use_cases),
) -> Response:
    flow_id = _flow_id_or_404(
        flow_id_path, "The requested Flow ID in the path is invalid."
    )
    validate_query_params(request, {"timerange", "object_id"})
    identity = identify_request(request)
    delete_request = deletion.delete_segments(
        flow_id=flow_id,
        timerange=timerange,
        object_id=object_id,
        identity=identity,
    )
    if delete_request is not None:
        return deletion_request_accepted_response(delete_request, request)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


def segment_coverage_gaps(segments: list[SegmentRecord]) -> list[str]:
    bounds: list[tuple[int, int]] = []
    for segment in segments:
        try:
            parsed = TimeRange.from_str(segment.timerange)
            normalized = finite_normalized_timerange_bounds(parsed)
        except Exception as exc:
            raise BadRequest("Bad request. Invalid stored Segment timerange.") from exc
        assert normalized.start is not None
        assert normalized.end is not None
        bounds.append((normalized.start, normalized.end))

    if not bounds:
        return []

    bounds.sort()
    gaps: list[str] = []
    covered_end = bounds[0][1]
    for start, end in bounds[1:]:
        if start > covered_end:
            gaps.append(
                f"[{Timestamp.from_nanosec(covered_end)}_"
                f"{Timestamp.from_nanosec(start)})"
            )
        if end > covered_end:
            covered_end = end
    return gaps
