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
    cache: dict[UUID, str] = {}

    def resolved_timerange(flow_id: UUID, visiting: set[UUID]) -> str:
        if flow_id in cache:
            return cache[flow_id]
        if flow_id in visiting:
            return _direct_timerange(direct_timeranges.get(flow_id)) or "()"

        visiting.add(flow_id)
        timeranges: list[str | None] = [
            _direct_timerange(direct_timeranges.get(flow_id))
        ]
        flow = flows_by_id.get(flow_id)
        if flow is not None:
            for item in flow_collection(flow):
                child_id = collection_child_id(item)
                if child_id is not None:
                    timeranges.append(resolved_timerange(child_id, visiting))
        visiting.remove(flow_id)

        merged = timerange_union_strings(timeranges) or "()"
        cache[flow_id] = merged
        return merged

    return {
        flow_id: resolved_timerange(flow_id, set())
        for flow_id in list(dict.fromkeys(flow_ids))
    }


def _direct_timerange(timerange: str | None) -> str | None:
    if timerange in {None, "()"}:
        return None
    return timerange
