from __future__ import annotations

from uuid import UUID

from fastapi import Request, Response
from fastapi.responses import JSONResponse

from tamoss.api.schemas import (
    DeletionRequestResponse,
    FlowSegmentResponse,
    ObjectResponse,
    SourceResponse,
    StorageBackendResponse,
    WebhookResponse,
)
from tamoss.domain.model import (
    DeletionRequestRecord,
    FlowRecord,
    MediaObjectRecord,
    SegmentRecord,
    SourceRecord,
    StorageBackend,
    WebhookRecord,
)
from tamoss.domain.pagination import Page
from tamoss.errors import normalize_error_payload


def with_page_headers(response: Response, request: Request, page: Page[object]) -> None:
    response.headers["X-Paging-Limit"] = str(page.limit)
    response.headers["X-Paging-Count"] = str(len(page.items))
    if page.next_page is not None:
        response.headers["X-Paging-NextKey"] = page.next_page
        response.headers["Link"] = (
            f'<{request.url.include_query_params(page=page.next_page)}>; rel="next"'
        )


def head_response(
    request: Request, response: Response | None = None
) -> Response | None:
    if request.method != "HEAD":
        return None
    headers = dict(response.headers) if response is not None else None
    return Response(status_code=200, headers=headers)


def storage_backend_response(backend: StorageBackend) -> dict:
    return StorageBackendResponse(
        id=backend.id,
        label=backend.label,
        default_storage=backend.default_storage,
        store_type=backend.store_type,
        provider=backend.provider,
        region=backend.region,
        store_product=backend.store_product,
    ).model_dump(exclude_none=True, mode="json")


def source_response(
    source: SourceRecord,
    *,
    source_collection: list[dict] | None = None,
    collected_by: list[UUID] | None = None,
) -> dict:
    return SourceResponse(
        id=source.id,
        format=source.format,
        label=source.label,
        description=source.description,
        tags=source.tags,
        source_collection=source_collection,
        collected_by=collected_by,
    ).model_dump(exclude_none=True, mode="json")


def webhook_response(webhook: WebhookRecord) -> dict:
    payload = {
        key: value
        for key, value in webhook.data.items()
        if key not in {"api_key_value", "id", "status", "tags"}
    }
    if "error" in payload:
        payload["error"] = normalize_error_payload(payload["error"])
    return WebhookResponse(
        **payload,
        id=webhook.id,
        status=webhook.status,
        tags=webhook.tags,
    ).model_dump(exclude_none=True, mode="json")


def deletion_request_response(request: DeletionRequestRecord) -> dict:
    return DeletionRequestResponse(
        id=request.id,
        flow_id=request.flow_id,
        timerange_to_delete=request.timerange_to_delete,
        timerange_remaining=request.timerange_remaining,
        delete_flow=request.delete_flow,
        created=request.created.isoformat(),
        created_by=request.created_by,
        updated=request.updated.isoformat(),
        status=request.status,
        error=normalize_error_payload(request.error),
    ).model_dump(exclude_none=True, mode="json")


def deletion_request_accepted_response(
    delete_request: DeletionRequestRecord, request: Request
) -> JSONResponse:
    path = f"/flow-delete-requests/{delete_request.id}"
    location = f"{str(request.base_url).rstrip('/')}{path}"
    return JSONResponse(
        status_code=202,
        headers={"Location": location},
        content=deletion_request_response(delete_request),
    )


def flow_response(flow: FlowRecord, *, timerange: str | None = None) -> dict:
    payload = dict(flow.data)
    payload["id"] = str(flow.id)
    if flow.source_id is not None:
        payload["source_id"] = str(flow.source_id)
    if flow.format is not None:
        payload["format"] = flow.format
    if flow.container is not None:
        payload["container"] = flow.container
    payload["read_only"] = flow.read_only
    payload["tags"] = flow.tags
    if timerange is not None:
        payload["timerange"] = timerange
    if flow.segments_updated is not None:
        payload["segments_updated"] = flow.segments_updated.isoformat()
    return {key: value for key, value in payload.items() if value is not None}


def segment_response(
    segment: SegmentRecord,
    get_urls: list[dict],
    *,
    include_object_timerange: bool = False,
) -> dict:
    return FlowSegmentResponse(
        object_id=segment.object_id,
        timerange=segment.timerange,
        ts_offset=segment.ts_offset,
        last_duration=segment.last_duration,
        object_timerange=segment.object_timerange if include_object_timerange else None,
        sample_offset=segment.sample_offset,
        sample_count=segment.sample_count,
        get_urls=get_urls,
        key_frame_count=segment.key_frame_count,
    ).model_dump(exclude_none=True, mode="json")


def object_response(
    media_object: MediaObjectRecord,
    referenced_by_flows: list,
    *,
    get_urls: list[dict],
) -> dict:
    timerange = media_object.timerange or "()"
    return ObjectResponse(
        id=media_object.id,
        referenced_by_flows=referenced_by_flows,
        first_referenced_by_flow=media_object.first_referenced_by_flow,
        timerange=timerange,
        get_urls=get_urls,
        key_frame_count=media_object.key_frame_count,
    ).model_dump(exclude_none=True, mode="json")
