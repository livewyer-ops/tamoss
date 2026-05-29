from __future__ import annotations

from dataclasses import dataclass
from uuid import UUID

from mediatimestamp import TimeRange, Timestamp

from tamoss.domain.model import SegmentRecord
from tamoss.domain.timeranges import (
    finite_normalized_timerange_bounds,
    normalized_timerange_bounds,
    parse_timerange,
    timerange_union_strings,
)


@dataclass(frozen=True)
class SegmentTimerangeBounds:
    start: int
    end: int
    is_point: bool = False


@dataclass(frozen=True)
class SegmentDeleteFilter:
    flow_id: UUID
    timerange_start: int | None = None
    timerange_end: int | None = None
    timerange_is_empty: bool = False
    object_id: str | None = None


def segment_bounds(segment: SegmentRecord) -> tuple[int, int]:
    parsed = TimeRange.from_str(segment.timerange)
    bounds = finite_normalized_timerange_bounds(parsed)
    assert bounds.start is not None
    assert bounds.end is not None
    return bounds.start, bounds.end


def segment_overlaps_bounds(
    segment: SegmentRecord,
    *,
    start: int | None,
    end: int | None,
    requested_is_point: bool,
) -> bool:
    segment_start, segment_end = segment_bounds(segment)
    query_end = timerange_query_end(start, end, requested_is_point)

    if query_end is not None and segment_start >= query_end:
        return False
    return not (start is not None and segment_end <= start)


def flow_timerange_matches(
    segments: list[SegmentRecord],
    *,
    start: int | None,
    end: int | None,
    requested_is_point: bool,
) -> bool:
    if not segments:
        return False
    starts: list[int] = []
    ends: list[int] = []
    for segment in segments:
        segment_start, segment_end = segment_bounds(segment)
        starts.append(segment_start)
        ends.append(segment_end)
    flow_start = min(starts)
    flow_end = max(ends)
    query_end = timerange_query_end(start, end, requested_is_point)

    if query_end is not None and flow_start >= query_end:
        return False
    return not (start is not None and flow_end <= start)


def segment_sort_key(segment: SegmentRecord) -> tuple[int, int, str]:
    start, end = segment_bounds(segment)
    return end, start, segment.object_id


def timerange_query_end(
    start: int | None, end: int | None, is_point: bool
) -> int | None:
    if is_point and start is not None and end == start:
        return start + 1
    return end


def timerange_union(segments: list[SegmentRecord]) -> str:
    return timerange_union_strings(segment.timerange for segment in segments) or "()"


def segment_delete_filter(
    *,
    flow_id: UUID,
    timerange: str | None,
    object_id: str | None,
) -> SegmentDeleteFilter:
    requested_range = parse_segment_delete_timerange(timerange)
    if requested_range is None:
        return SegmentDeleteFilter(flow_id=flow_id, object_id=object_id)
    bounds = normalized_timerange_bounds(requested_range)
    return SegmentDeleteFilter(
        flow_id=flow_id,
        timerange_start=bounds.start,
        timerange_end=bounds.end,
        timerange_is_empty=bounds.is_empty,
        object_id=object_id,
    )


def parse_segment_delete_timerange(timerange: str | None) -> TimeRange | None:
    if timerange is None or timerange in {"", "_"}:
        return None
    return parse_timerange(timerange, field_name="timerange")


def segment_object_timerange(segment: SegmentRecord) -> str:
    return object_timerange_from_segment_fields(
        timerange=segment.timerange,
        ts_offset=segment.ts_offset,
        object_timerange=segment.object_timerange,
    )


def object_timerange_from_segment_fields(
    *,
    timerange: str,
    ts_offset: str | None,
    object_timerange: str | None,
) -> str:
    if object_timerange:
        return object_timerange
    if not ts_offset:
        return timerange
    offset = Timestamp.from_str(ts_offset)
    parsed = TimeRange.from_str(timerange)
    if parsed.start is None or parsed.end is None:
        return timerange
    return str(
        TimeRange(
            parsed.start - offset,
            parsed.end - offset,
            parsed.inclusivity,
        )
    )
