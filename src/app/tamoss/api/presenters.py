from __future__ import annotations

from datetime import datetime, timedelta
from uuid import UUID

from fastapi import Request, Response
from fastapi.responses import JSONResponse

from tamoss.contract.generated import contract_models
from tamoss.contract.payloads import flow_payload, source_payload
from tamoss.contract.serialization import contract_dump
from tamoss.domain.model import (
    DeletionRequestRecord,
    FlowRecord,
    MediaObjectRecord,
    SegmentRecord,
    SourceRecord,
    SourceRelationships,
    StorageBackend,
    WebhookRecord,
)
from tamoss.domain.pagination import Page
from tamoss.errors import normalize_error_payload

JsonPayload = dict[str, object]


def with_page_headers(
    response: Response,
    request: Request,
    page: Page[object],
    *,
    reverse_order: bool | None = None,
) -> None:
    response.headers["X-Paging-Limit"] = str(page.limit)
    response.headers["X-Paging-Count"] = str(len(page.items))
    if reverse_order is not None:
        response.headers["X-Paging-Reverse-Order"] = str(reverse_order).lower()
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


def storage_backend_response(backend: StorageBackend) -> JsonPayload:
    return contract_dump(
        contract_models.StorageBackendsListItem(
            id=str(backend.id),
            label=backend.label,
            default_storage=backend.default_storage,
            store_type=backend.store_type,
            provider=backend.provider,
            region=backend.region,
            store_product=backend.store_product,
            tags=backend.tags,
        )
    )


def source_response_with_relationships(
    source: SourceRecord, *, relationships: dict[UUID, SourceRelationships]
) -> JsonPayload:
    relationship = relationships.get(source.id)
    return source_payload(
        source,
        source_collection=relationship.source_collection if relationship else None,
        collected_by=relationship.collected_by if relationship else None,
    )


def webhook_response(webhook: WebhookRecord) -> JsonPayload:
    payload = {
        key: value
        for key, value in webhook.data.items()
        if key not in {"api_key_value", "id", "status", "tags"}
    }
    if "error" in payload:
        payload["error"] = normalize_error_payload(payload["error"])
    return contract_dump(
        contract_models.WebhookGet(
            **payload,
            id=str(webhook.id),
            status=webhook.status,
            tags=webhook.tags,
        )
    )


def deletion_request_response(
    request: DeletionRequestRecord,
    *,
    retention_seconds: int = 0,
) -> JsonPayload:
    return contract_dump(
        contract_models.DeletionRequest(
            id=str(request.id),
            flow_id=str(request.flow_id),
            timerange_to_delete=request.timerange_to_delete,
            timerange_remaining=request.timerange_remaining,
            delete_flow=request.delete_flow,
            created=request.created,
            created_by=request.created_by,
            updated=request.updated,
            expiry=_deletion_request_expiry(request, retention_seconds),
            status=request.status,
            error=normalize_error_payload(request.error),
        )
    )


def _deletion_request_expiry(
    request: DeletionRequestRecord,
    retention_seconds: int,
) -> datetime | None:
    # Completed requests are removed by the worker retention purge; surface
    # that window through the spec's optional expiry field so clients know
    # how long the record remains queryable.
    if retention_seconds <= 0 or request.status != "done":
        return None
    return request.updated + timedelta(seconds=retention_seconds)


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


def flow_response(flow: FlowRecord, *, timerange: str | None = None) -> JsonPayload:
    return flow_payload(flow, timerange=timerange)


def segment_response(
    segment: SegmentRecord,
    get_urls: list[dict[str, object]],
    *,
    include_object_timerange: bool = False,
) -> JsonPayload:
    return contract_dump(
        contract_models.FlowSegment(
            object_id=segment.object_id,
            timerange=segment.timerange,
            ts_offset=segment.ts_offset,
            last_duration=segment.last_duration,
            object_timerange=segment.object_timerange
            if include_object_timerange
            else None,
            sample_offset=segment.sample_offset,
            sample_count=segment.sample_count,
            get_urls=get_urls,
            key_frame_count=segment.key_frame_count,
        )
    )


def object_response(
    media_object: MediaObjectRecord,
    referenced_by_flows: list[UUID],
    *,
    get_urls: list[dict[str, object]],
) -> JsonPayload:
    timerange = media_object.timerange or "()"
    return contract_dump(
        contract_models.Object(
            id=media_object.id,
            referenced_by_flows=[str(flow_id) for flow_id in referenced_by_flows],
            first_referenced_by_flow=str(media_object.first_referenced_by_flow)
            if media_object.first_referenced_by_flow is not None
            else None,
            timerange=timerange,
            get_urls=get_urls,
            key_frame_count=media_object.key_frame_count,
        )
    )
