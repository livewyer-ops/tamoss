from __future__ import annotations

from dataclasses import replace
from decimal import Decimal, localcontext
from uuid import uuid4

import pytest
from tamoss.adapters.postgres import PostgresRepository
from tamoss.adapters.postgres_repository.mappers import _timerange_from_bounds
from tamoss.application.contexts.flows import query_timerange
from tamoss.domain.exceptions import SegmentOverlapError
from tamoss.domain.model import FlowRecord, MediaObjectRecord, SegmentRecord
from tamoss.domain.segments import (
    SegmentTimerangeBounds,
    segment_bounds,
    segment_delete_filter,
)

from tests.support.bbc_contract import bbc_validator

pytestmark = pytest.mark.needs_db


def test_numeric_bounds_preserve_nanoseconds_with_low_decimal_precision() -> None:
    with localcontext() as context:
        context.prec = 8
        assert (
            _timerange_from_bounds(
                Decimal("9999999998000000001"), Decimal("10000000002000000002")
            )
            == "[9999999998:1_10000000002:2)"
        )


@pytest.mark.parametrize(
    "timerange,normalised_range",
    [
        ("[9999999998:1_10000000002:2)", "[9999999998:1_10000000002:2)"),
        ("[-10000000002:2_-9999999998:1)", "[-10000000002:2_-9999999998:1)"),
        ("[9223372036:854775807]", "[9223372036:854775807_9223372036:854775808)"),
        ("[-9223372036:854775809]", "[-9223372036:854775809_-9223372036:854775808)"),
        ("(9999999998:1_10000000002:2]", "[9999999998:2_10000000002:3)"),
        (
            "[281474976710000:1_281474976710004:2)",
            "[281474976710000:1_281474976710004:2)",
        ),
    ],
)
def test_wide_segment_bounds_cover_repository_operations(
    postgres_repo: PostgresRepository, timerange: str, normalised_range: str
) -> None:
    flow = FlowRecord(
        id=uuid4(),
        source_id=uuid4(),
        data={},
        format="urn:x-nmos:format:data",
        container=None,
    )
    segment = SegmentRecord(
        flow_id=flow.id, object_id=str(uuid4()), timerange=timerange
    )
    bbc_validator("timerange.json").validate(timerange)
    repo = postgres_repo.segment_repository
    repo.save_registered_segments(
        flow=flow,
        media_objects=[MediaObjectRecord(id=segment.object_id)],
        segments=[segment],
    )
    start, end = segment_bounds(segment)
    assert repo.list_segments(flow.id) == [segment]
    assert repo.list_segments_overlapping(
        flow_id=flow.id,
        timeranges=[SegmentTimerangeBounds(start=start, end=end)],
    ) == [segment]
    with pytest.raises(SegmentOverlapError):
        repo.save_registered_segments(
            flow=flow,
            media_objects=[],
            segments=[replace(segment, object_id=str(uuid4()))],
        )
    delete_filter = segment_delete_filter(
        flow_id=flow.id, timerange=normalised_range, object_id=None
    )
    with localcontext() as context:
        context.prec = 8
        assert postgres_repo.flow_repository.flow_timeranges([flow.id]) == {
            flow.id: normalised_range
        }
        assert postgres_repo.flow_repository.flow_timeranges(
            [flow.id], timerange_start=start, timerange_end=start + 1
        ) == {flow.id: normalised_range}
        assert repo.segment_delete_timerange(delete_filter) == normalised_range

    ordinary = replace(segment, object_id=str(uuid4()), timerange="[0:0_1:0)")
    repo.append_segment(ordinary)
    query = {
        "flow_id": flow.id,
        "object_id": None,
        "timerange_start": None,
        "timerange_end": None,
        "timerange_is_empty": False,
        "timerange_is_point": False,
        "limit": 1,
    }
    for reverse in (False, True):
        expected = [segment, ordinary] if start < 0 else [ordinary, segment]
        if reverse:
            expected.reverse()
        page = None
        for item in expected:
            result = repo.list_segments_page(**query, reverse_order=reverse, page=page)
            assert result.items == [item]
            assert result.timerange == item.timerange
            page = result.next_page
        assert page is None
    filtered = repo.list_segments_page(
        **{**query, "timerange_start": start, "timerange_end": end},
        reverse_order=False,
        page=None,
    )
    assert filtered.items == [segment]
    assert repo.delete_segment_batch(delete_filter=delete_filter, limit=1) == [segment]
    assert repo.list_segments(flow.id) == [ordinary]


@pytest.mark.parametrize(
    "query,expected",
    [
        ("[9:0_10:0)", "[8:0_12:0)"),
        ("[11:0_19:0)", "[8:0_22:0)"),
        ("[14:0_15:0)", "()"),
        ("[12:0]", "()"),
        ("[22:0]", "[22:0_22:1)"),
        ("(21:999999999_22:0)", "()"),
        ("[7:0_8:0)", "()"),
        ("[7:0_8:0]", "[8:0_12:0)"),
        ("_9:0)", "[8:0_12:0)"),
        ("[22:0_", "[22:0_22:1)"),
        ("()", "()"),
        ("_", "[8:0_22:1)"),
    ],
)
def test_flow_timeranges_filter_segments_before_aggregation(
    postgres_repo: PostgresRepository, query: str, expected: str
) -> None:
    flow = FlowRecord(
        id=uuid4(),
        source_id=uuid4(),
        data={},
        format="urn:x-nmos:format:data",
        container=None,
    )
    empty_id = uuid4()
    postgres_repo.flow_repository.save_flow(flow)
    for timerange in ("[8:0_12:0)", "[18:0_22:0)", "[22:0]"):
        postgres_repo.segment_repository.append_segment(
            SegmentRecord(flow_id=flow.id, object_id=str(uuid4()), timerange=timerange)
        )
    bounds = query_timerange(query)
    assert postgres_repo.flow_repository.flow_timeranges(
        [flow.id, empty_id],
        timerange_start=bounds.start,
        timerange_end=bounds.end,
        timerange_is_empty=bounds.is_empty,
        timerange_is_point=bounds.is_point,
    ) == {flow.id: expected, empty_id: "()"}
