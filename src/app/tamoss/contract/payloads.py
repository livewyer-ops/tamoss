from __future__ import annotations

from collections.abc import Iterable, Mapping
from typing import Any, cast
from uuid import UUID

from tamoss.contract.generated import contract_models
from tamoss.contract.serialization import contract_dump
from tamoss.domain.model import FlowRecord, SourceRecord

JsonPayload = dict[str, Any]


def flow_payload(flow: FlowRecord, *, timerange: str | None = None) -> JsonPayload:
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
    payload["created"] = flow.created.isoformat()
    payload["metadata_updated"] = flow.metadata_updated.isoformat()
    if timerange is not None:
        payload["timerange"] = timerange
    if flow.segments_updated is not None:
        payload["segments_updated"] = flow.segments_updated.isoformat()
    return cast(
        JsonPayload, contract_dump(contract_models.FlowGet.model_validate(payload))
    )


def source_payload(
    source: SourceRecord,
    *,
    source_collection: list[JsonPayload] | None = None,
    collected_by: Iterable[UUID | str] | None = None,
) -> JsonPayload:
    payload: JsonPayload = {
        "id": str(source.id),
        "format": source.format,
        "label": source.label,
        "description": source.description,
        "tags": source.tags,
        "created": source.created.isoformat(),
        "updated": source.metadata_updated.isoformat(),
    }
    if source_collection is not None:
        payload["source_collection"] = source_collection
    if collected_by is not None:
        payload["collected_by"] = [str(source_id) for source_id in collected_by]
    payload = without_none(payload)
    return cast(
        JsonPayload, contract_dump(contract_models.Source.model_validate(payload))
    )


def without_none[PayloadValue](
    payload: Mapping[str, PayloadValue | None],
) -> dict[str, PayloadValue]:
    return {
        key: cast(PayloadValue, value)
        for key, value in payload.items()
        if value is not None
    }
