from __future__ import annotations

from collections.abc import Iterable, Mapping
from dataclasses import replace
from uuid import UUID

from tamoss.domain.model import FlowRecord
from tamoss.domain.timeranges import timerange_union_strings


def flow_data_value(flow: FlowRecord, name: str) -> object:
    if name in flow.data:
        return flow.data[name]
    essence_parameters = flow.data.get("essence_parameters")
    if isinstance(essence_parameters, dict):
        return essence_parameters.get(name)
    return None


def flow_collection(flow: FlowRecord) -> list[dict]:
    collection = flow.data.get("flow_collection")
    if not isinstance(collection, list):
        return []
    return [dict(item) for item in collection if isinstance(item, dict)]


def collected_by_by_flow_id(flows: list[FlowRecord]) -> dict[UUID, list[str]]:
    collected_by: dict[UUID, list[str]] = {}
    for parent in flows:
        parent_id = str(parent.id)
        for item in flow_collection(parent):
            child_id = collection_child_id(item)
            if child_id is None:
                continue
            parent_ids = collected_by.setdefault(child_id, [])
            if parent_id not in parent_ids:
                parent_ids.append(parent_id)
    return collected_by


def flow_with_collected_by(flow: FlowRecord, collected_by: list[str]) -> FlowRecord:
    data = dict(flow.data)
    if collected_by:
        data["collected_by"] = collected_by
    else:
        data.pop("collected_by", None)
    return replace(flow, data=data)


def collection_child_id(item: object) -> UUID | None:
    if not isinstance(item, dict):
        return None
    raw_id = item.get("id")
    if raw_id is None:
        return None
    try:
        return UUID(str(raw_id))
    except ValueError:
        return None


def collection_role(item: object) -> str | None:
    if not isinstance(item, dict):
        return None
    raw_role = item.get("role")
    if raw_role is None:
        return None
    return str(raw_role)


def collection_aware_flow_timeranges(
    flows: Iterable[FlowRecord],
    direct_timeranges: Mapping[UUID, str],
    flow_ids: Iterable[UUID],
) -> dict[UUID, str]:
    flows_by_id = {flow.id: flow for flow in flows}
    timeranges: dict[UUID, str] = {}
    for root_id in dict.fromkeys(flow_ids):
        pending = [root_id]
        visited: set[UUID] = set()
        while pending:
            flow_id = pending.pop()
            if flow_id in visited:
                continue
            visited.add(flow_id)
            flow = flows_by_id.get(flow_id)
            if flow is not None:
                pending.extend(
                    child_id
                    for item in flow_collection(flow)
                    if (child_id := collection_child_id(item)) is not None
                )
        timeranges[root_id] = (
            timerange_union_strings(
                _direct_timerange(direct_timeranges.get(flow_id)) for flow_id in visited
            )
            or "()"
        )
    return timeranges


def _direct_timerange(timerange: str | None) -> str | None:
    if timerange in {None, "()"}:
        return None
    return timerange
