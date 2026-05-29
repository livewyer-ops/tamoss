from __future__ import annotations

from collections.abc import Iterable
from uuid import UUID, uuid4

import pytest
from tamoss.application.use_cases import TamossUseCases
from tamoss.auth import Identity
from tamoss.domain.model import (
    FlowRecord,
    MediaObjectRecord,
    SegmentRecord,
    StorageBackend,
)
from tamoss.settings import Settings

from tests.support.memory_repository import FakeTamossRepository
from tests.support.object_storage import InMemoryObjectStorage

pytestmark = pytest.mark.architecture


def test_delete_refresh_uses_one_remaining_segment_scan_for_many_objects() -> None:
    flow_id = uuid4()
    storage_backend = _storage_backend()
    repository = _CountingRepository(storage_backend)
    use_cases = TamossUseCases(
        repository=repository,
        object_storage=InMemoryObjectStorage(),
        settings=Settings(auth_required=False),
    )
    repository.save_flow(
        FlowRecord(
            id=flow_id,
            data={},
            source_id=uuid4(),
            format="urn:x-nmos:format:video",
            container="video/mp2t",
        )
    )
    for index in range(20):
        object_id = f"bbc/deleted-{index}.ts"
        repository.save_object(
            MediaObjectRecord(id=object_id, referenced_by_flows={flow_id})
        )
        repository.append_segment(
            SegmentRecord(
                flow_id=flow_id,
                object_id=object_id,
                timerange=f"[{index}:0_{index}:1)",
            )
        )
    shared_object_id = "bbc/shared.ts"
    repository.save_object(
        MediaObjectRecord(id=shared_object_id, referenced_by_flows={flow_id})
    )
    repository.append_segment(
        SegmentRecord(
            flow_id=flow_id,
            object_id=shared_object_id,
            timerange="[20:0_20:1)",
        )
    )
    repository.append_segment(
        SegmentRecord(
            flow_id=flow_id,
            object_id=shared_object_id,
            timerange="[30:0_30:1)",
        )
    )

    request = use_cases.deletion.delete_segments(
        flow_id=flow_id,
        timerange="[0:0_21:0)",
        object_id=None,
        identity=Identity(subject="tester", method="test"),
    )
    assert request is not None
    repository.reset_counters()

    assert use_cases.deletion.process_pending_delete_requests() == 1

    assert repository.list_segments_calls_by_flow_id == {}
    assert repository.list_segments_for_objects_calls == 1
    assert repository.list_segments_for_objects_calls_by_flow_id == {flow_id: 1}
    assert repository.get_objects_calls == 1
    assert repository.get_object_calls == 0
    assert repository.get_object(shared_object_id) is not None


def test_delete_refresh_filters_segments_for_other_referencing_flows() -> None:
    deleted_flow_id = uuid4()
    retained_flow_id = uuid4()
    storage_backend = _storage_backend()
    repository = _CountingRepository(storage_backend)
    use_cases = TamossUseCases(
        repository=repository,
        object_storage=InMemoryObjectStorage(),
        settings=Settings(auth_required=False),
    )
    for flow_id in (deleted_flow_id, retained_flow_id):
        repository.save_flow(
            FlowRecord(
                id=flow_id,
                data={},
                source_id=uuid4(),
                format="urn:x-nmos:format:video",
                container="video/mp2t",
            )
        )
    shared_object_id = "bbc/shared.ts"
    repository.save_object(
        MediaObjectRecord(
            id=shared_object_id,
            referenced_by_flows={deleted_flow_id, retained_flow_id},
        )
    )
    repository.append_segment(
        SegmentRecord(
            flow_id=deleted_flow_id,
            object_id=shared_object_id,
            timerange="[0:0_10:0)",
        )
    )
    repository.append_segment(
        SegmentRecord(
            flow_id=retained_flow_id,
            object_id=shared_object_id,
            timerange="[20:0_30:0)",
        )
    )
    for index in range(100):
        repository.append_segment(
            SegmentRecord(
                flow_id=retained_flow_id,
                object_id=f"bbc/noise-{index}.ts",
                timerange=f"[{index}:0_{index}:1)",
            )
        )

    request = use_cases.deletion.delete_segments(
        flow_id=deleted_flow_id,
        timerange="[0:0_10:0)",
        object_id=None,
        identity=Identity(subject="tester", method="test"),
    )
    assert request is not None
    repository.reset_counters()

    assert use_cases.deletion.process_pending_delete_requests() == 1

    media_object = repository.get_object(shared_object_id)
    assert media_object is not None
    assert media_object.referenced_by_flows == {retained_flow_id}
    assert media_object.timerange == "[20:0_30:0)"
    assert repository.list_segments_calls_by_flow_id == {}
    assert repository.list_segments_for_objects_calls == 2
    assert repository.list_segments_for_objects_calls_by_flow_id == {
        deleted_flow_id: 1,
        retained_flow_id: 1,
    }


class _CountingRepository(FakeTamossRepository):
    def __init__(self, storage_backend: StorageBackend):
        super().__init__(storage_backend)
        self.list_segments_calls_by_flow_id: dict[UUID, int] = {}
        self.list_segments_for_objects_calls = 0
        self.list_segments_for_objects_calls_by_flow_id: dict[UUID, int] = {}
        self.get_objects_calls = 0
        self.get_object_calls = 0

    def reset_counters(self) -> None:
        self.list_segments_calls_by_flow_id = {}
        self.list_segments_for_objects_calls = 0
        self.list_segments_for_objects_calls_by_flow_id = {}
        self.get_objects_calls = 0
        self.get_object_calls = 0

    def list_segments(self, flow_id: UUID) -> list[SegmentRecord]:
        self.list_segments_calls_by_flow_id[flow_id] = (
            self.list_segments_calls_by_flow_id.get(flow_id, 0) + 1
        )
        return super().list_segments(flow_id)

    def get_objects(self, object_ids: Iterable[str]) -> dict[str, MediaObjectRecord]:
        self.get_objects_calls += 1
        return super().get_objects(object_ids)

    def list_segments_for_objects(
        self, *, flow_id: UUID, object_ids: Iterable[str]
    ) -> list[SegmentRecord]:
        self.list_segments_for_objects_calls += 1
        self.list_segments_for_objects_calls_by_flow_id[flow_id] = (
            self.list_segments_for_objects_calls_by_flow_id.get(flow_id, 0) + 1
        )
        return super().list_segments_for_objects(
            flow_id=flow_id,
            object_ids=object_ids,
        )

    def get_object(self, object_id: str) -> MediaObjectRecord | None:
        self.get_object_calls += 1
        return super().get_object(object_id)


def _storage_backend() -> StorageBackend:
    return StorageBackend(
        id=uuid4(),
        label="tamoss.storage.primary",
        provider="tamoss",
        region="us-east-1",
        store_product="s3",
        bucket_name="tamoss-primary",
    )
