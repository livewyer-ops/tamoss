from __future__ import annotations

from collections.abc import Iterable
from dataclasses import dataclass
from typing import Any

from mediatimestamp import TimeRange, Timestamp


@dataclass(frozen=True)
class NormalizedTimerangeBounds:
    start: int | None
    end: int | None
    is_empty: bool = False
    is_point: bool = False


def normalized_timerange_bounds(timerange: TimeRange) -> NormalizedTimerangeBounds:
    start = int(timerange.start.to_nanosec()) if timerange.start is not None else None
    end = int(timerange.end.to_nanosec()) if timerange.end is not None else None
    is_point = (
        not timerange.is_empty()
        and timerange.start is not None
        and timerange.end is not None
        and timerange.start == timerange.end
        and timerange.includes_start()
        and timerange.includes_end()
    )

    if timerange.is_empty():
        return NormalizedTimerangeBounds(start=start, end=end, is_empty=True)

    if start is not None and not timerange.includes_start():
        start += 1
    if end is not None and timerange.includes_end():
        end += 1

    is_effectively_empty = start is not None and end is not None and start >= end
    return NormalizedTimerangeBounds(
        start=start,
        end=end,
        is_empty=is_effectively_empty,
        is_point=is_point,
    )


def finite_normalized_timerange_bounds(
    timerange: TimeRange,
) -> NormalizedTimerangeBounds:
    bounds = normalized_timerange_bounds(timerange)
    if bounds.start is None or bounds.end is None:
        raise ValueError("timerange must have finite start and end timestamps")
    if bounds.is_empty:
        raise ValueError("timerange must not be empty")
    return bounds


def parse_timerange(
    value: Any, *, field_name: str = "timerange", finite: bool = False
) -> TimeRange:
    if not isinstance(value, str) or not value:
        raise ValueError(f"{field_name} must be a timerange string")
    try:
        parsed = TimeRange.from_str(value)
    except Exception as exc:
        raise ValueError(f"{field_name} is invalid") from exc
    if finite and (parsed.start is None or parsed.end is None):
        raise ValueError(f"{field_name} must have finite start and end timestamps")
    return parsed


def parse_timestamp(value: Any, *, field_name: str) -> Timestamp:
    if not isinstance(value, str) or not value:
        raise ValueError(f"{field_name} must be a timestamp string")
    try:
        return Timestamp.from_str(value)
    except Exception as exc:
        raise ValueError(f"{field_name} is invalid") from exc


def timerange_union_strings(timeranges: Iterable[str | None]) -> str | None:
    ranges: list[TimeRange] = []
    for timerange_value in timeranges:
        if timerange_value is None:
            continue
        try:
            parsed = TimeRange.from_str(timerange_value)
        except Exception as exc:
            raise ValueError("timerange is invalid") from exc
        if not parsed.is_empty():
            ranges.append(parsed)
    if not ranges:
        return None
    merged = ranges[0]
    for parsed_timerange in ranges[1:]:
        merged = merged.extend_to_encompass_timerange(parsed_timerange)
    return str(merged)
