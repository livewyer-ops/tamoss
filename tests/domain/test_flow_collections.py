from uuid import UUID

from tamoss.domain.flow_collections import collection_aware_flow_timeranges
from tamoss.domain.model import FlowRecord


def flow(index: int, *children: int) -> FlowRecord:
    return FlowRecord(
        id=UUID(int=index),
        data={"flow_collection": [{"id": str(UUID(int=child))} for child in children]},
        source_id=None,
        format=None,
        container=None,
    )


def test_collection_cycles_have_order_independent_timeranges() -> None:
    flows = [flow(1, 2, 3), flow(2, 1), flow(3)]
    ranges = {UUID(int=3): "[20:0_30:0)"}
    expected = {record.id: "[20:0_30:0)" for record in flows}
    for requested in (
        [record.id for record in flows],
        [record.id for record in reversed(flows)],
    ):
        assert collection_aware_flow_timeranges(flows, ranges, requested) == expected


def test_deep_collection_does_not_depend_on_python_recursion_limit() -> None:
    flows = [flow(index, index + 1) for index in range(1, 1100)] + [flow(1100)]
    ranges = {UUID(int=1100): "[20:0_30:0)"}
    assert collection_aware_flow_timeranges(flows, ranges, [UUID(int=1)]) == {
        UUID(int=1): "[20:0_30:0)"
    }
