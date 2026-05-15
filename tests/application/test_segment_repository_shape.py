from __future__ import annotations

from collections.abc import Iterable
from contextlib import nullcontext
from uuid import UUID, uuid4

from fastapi.testclient import TestClient
from tamoss.api.schemas import FlowSegmentPost
from tamoss.app import create_app
from tamoss.application.use_cases import TamossUseCases
from tamoss.domain.model import (
    FlowRecord,
    MediaObjectRecord,
    ObjectInstance,
    SegmentRecord,
    SourceRecord,
    SourceRelationships,
    StorageBackend,
    WebhookRecord,
)
from tamoss.domain.pagination import Page
from tamoss.ports.repositories import SegmentTimerangeBounds
from tamoss.settings import Settings, StorageBackendSettings

SEGMENT_COUNT = 300
BACKEND_ID = UUID("11111111-1111-4111-8111-111111111111")
BACKEND_LABEL = "tamoss.performance.primary"


def test_segment_listing_uses_bulk_repository_shape() -> None:
    backend = _storage_backend()
    repository = CountingRepository(backend)
    use_cases = _use_cases(repository)
    flow_id = uuid4()
    repository.save_flow(_flow(flow_id))
    for index in range(SEGMENT_COUNT):
        object_id = _object_id(index)
        repository.save_object(
            MediaObjectRecord(
                id=object_id,
                timerange=_timerange(index),
                first_referenced_by_flow=flow_id,
                referenced_by_flows={flow_id},
                instances=[
                    ObjectInstance(
                        storage_backend=backend,
                        url=f"https://objects.example.test/{object_id}",
                        label=backend.label,
                        controlled=True,
                        presigned=True,
                    )
                ],
            )
        )
        repository.append_segment(
            SegmentRecord(
                flow_id=flow_id,
                object_id=object_id,
                timerange=_timerange(index),
            )
        )
    repository.reset_counts()

    app = create_app(_settings(), use_cases=use_cases)
    with TestClient(app) as client:
        response = client.get(
            f"/flows/{flow_id}/segments",
            params={
                "limit": str(SEGMENT_COUNT),
                "accept_get_urls": BACKEND_LABEL,
                "presigned": "true",
            },
        )

    assert response.status_code == 200
    assert len(response.json()) == SEGMENT_COUNT
    assert repository.list_segments_page_calls == 1
    assert repository.get_objects_calls == 1
    assert repository.get_object_calls == 0
    assert repository.list_segments_calls == 0


def test_segment_registration_uses_bounded_repository_shape() -> None:
    backend = _storage_backend()
    repository = CountingRepository(backend)
    use_cases = _use_cases(repository)
    flow_id = uuid4()
    repository.save_flow(_flow(flow_id))
    for index in range(SEGMENT_COUNT):
        repository.save_object(MediaObjectRecord(id=_object_id(index)))
    repository.reset_counts()

    results = use_cases.register_segments(
        flow_id=flow_id,
        segment_posts=[
            FlowSegmentPost(
                object_id=_object_id(index),
                timerange=_timerange(index),
            )
            for index in range(SEGMENT_COUNT)
        ],
    )

    assert all(result.error is None for result in results)
    assert repository.list_segments_overlapping_calls == 1
    assert repository.lock_flow_segments_calls == 1
    assert repository.get_objects_calls == 1
    assert repository.save_registered_segments_calls == 1
    assert repository.list_segments_calls == 0
    assert repository.get_object_calls == 0
    assert repository.save_object_calls == 0
    assert repository.append_segment_calls == 0


class CountingRepository:
    def __init__(self, storage_backend: StorageBackend):
        self._storage_backend = storage_backend
        self._flows: dict[UUID, FlowRecord] = {}
        self._objects: dict[str, MediaObjectRecord] = {}
        self._segments: dict[UUID, list[SegmentRecord]] = {}
        self.reset_counts()

    def reset_counts(self) -> None:
        self.get_object_calls = 0
        self.get_objects_calls = 0
        self.save_object_calls = 0
        self.list_segments_calls = 0
        self.list_segments_page_calls = 0
        self.list_segments_overlapping_calls = 0
        self.append_segment_calls = 0
        self.save_registered_segments_calls = 0
        self.lock_flow_segments_calls = 0

    def list_storage_backends(self) -> list[StorageBackend]:
        return [self._storage_backend]

    def unit_of_work(self):
        return nullcontext(self)

    def lock_flow_segments(self, flow_id: UUID) -> None:
        self.lock_flow_segments_calls += 1

    def default_storage_backend(self) -> StorageBackend:
        return self._storage_backend

    def get_storage_backend(self, storage_id: UUID) -> StorageBackend | None:
        if storage_id == self._storage_backend.id:
            return self._storage_backend
        return None

    def list_webhooks(self) -> list[WebhookRecord]:
        return []

    def list_webhooks_page(self, **kwargs) -> Page[WebhookRecord]:
        limit = kwargs.get("limit") or 100
        return Page(items=[], limit=limit)

    def save_flow(self, flow: FlowRecord) -> None:
        self._flows[flow.id] = flow

    def list_flows(self) -> list[FlowRecord]:
        return list(self._flows.values())

    def list_flows_page(self, **kwargs) -> Page[FlowRecord]:
        flows = sorted(self._flows.values(), key=lambda flow: str(flow.id))
        limit = kwargs.get("limit") or 100
        return Page(items=flows[:limit], limit=limit)

    def flow_timeranges(self, flow_ids: Iterable[UUID]) -> dict[UUID, str]:
        return {flow_id: "()" for flow_id in flow_ids}

    def get_flow(self, flow_id: UUID) -> FlowRecord | None:
        return self._flows.get(flow_id)

    def get_source(self, source_id: UUID) -> SourceRecord | None:
        return None

    def list_sources(self) -> list[SourceRecord]:
        return []

    def list_sources_page(self, **kwargs) -> Page[SourceRecord]:
        limit = kwargs.get("limit") or 100
        return Page(items=[], limit=limit)

    def source_relationships_for(
        self, source_ids: Iterable[UUID]
    ) -> dict[UUID, SourceRelationships]:
        return {
            source_id: SourceRelationships(source_collection=[], collected_by=[])
            for source_id in source_ids
        }

    def list_flow_ids_matching_tags_page(self, **kwargs) -> Page[UUID]:
        limit = kwargs.get("limit") or 100
        return Page(items=[], limit=limit)

    def save_object(self, media_object: MediaObjectRecord) -> None:
        self.save_object_calls += 1
        self._objects[media_object.id] = media_object

    def create_object(self, media_object: MediaObjectRecord) -> bool:
        if media_object.id in self._objects:
            return False
        self._objects[media_object.id] = media_object
        return True

    def append_segment(self, segment: SegmentRecord) -> None:
        self.append_segment_calls += 1
        self._segments.setdefault(segment.flow_id, []).append(segment)

    def get_object(self, object_id: str) -> MediaObjectRecord | None:
        self.get_object_calls += 1
        return self._objects.get(object_id)

    def get_objects(self, object_ids: Iterable[str]) -> dict[str, MediaObjectRecord]:
        self.get_objects_calls += 1
        requested_ids = set(object_ids)
        return {
            object_id: media_object
            for object_id, media_object in self._objects.items()
            if object_id in requested_ids
        }

    def list_segments(self, flow_id: UUID) -> list[SegmentRecord]:
        self.list_segments_calls += 1
        return list(self._segments.get(flow_id, []))

    def list_segments_page(self, **kwargs) -> Page[SegmentRecord]:
        self.list_segments_page_calls += 1
        flow_id = kwargs["flow_id"]
        segments = list(self._segments.get(flow_id, []))
        if kwargs.get("reverse_order"):
            segments.reverse()
        limit = kwargs.get("limit")
        if limit is not None:
            segments = segments[:limit]
        return Page(items=segments, limit=limit, next_page=None, timerange="[0:0_1:0)")

    def list_segments_overlapping(
        self,
        *,
        flow_id: UUID,
        timeranges: Iterable[SegmentTimerangeBounds],
    ) -> list[SegmentRecord]:
        self.list_segments_overlapping_calls += 1
        return []

    def save_registered_segments(
        self,
        *,
        flow: FlowRecord,
        media_objects: Iterable[MediaObjectRecord],
        segments: Iterable[SegmentRecord],
    ) -> None:
        self.save_registered_segments_calls += 1
        self._flows[flow.id] = flow
        for media_object in media_objects:
            self._objects[media_object.id] = media_object
        for segment in segments:
            self._segments.setdefault(segment.flow_id, []).append(segment)


class DummyObjectStorage:
    def build_put_request(
        self, *, object_id: str, flow_container: str, backend: StorageBackend
    ) -> dict:
        return {}

    def build_get_url(self, *, object_id: str, backend: StorageBackend) -> str:
        return f"https://objects.example.test/{object_id}"

    def build_get_urls(self, *, object_id: str, backend: StorageBackend) -> list[dict]:
        return []

    def write(
        self, object_id: str, data: bytes, *, backend: StorageBackend | None = None
    ) -> None:
        return None

    def read(
        self, object_id: str, *, backend: StorageBackend | None = None
    ) -> bytes | None:
        return None

    def iter_chunks(
        self,
        object_id: str,
        *,
        backend: StorageBackend | None = None,
        chunk_size: int = 1024 * 1024,
    ):
        return None

    def delete(self, object_id: str, *, backend: StorageBackend | None = None) -> None:
        return None


def _use_cases(repository: CountingRepository) -> TamossUseCases:
    return TamossUseCases(
        repository=repository,
        object_storage=DummyObjectStorage(),
        settings=_settings(),
    )


def _settings() -> Settings:
    return Settings(
        auth_required=False,
        database_url=None,
        storage_backend=StorageBackendSettings(
            id=BACKEND_ID,
            label=BACKEND_LABEL,
            provider="tamoss",
            region="us-east-1",
            store_product="s3",
            default_storage=True,
            bucket_name="tamoss-performance",
            endpoint_url="https://objects.internal.example.test",
            public_endpoint_url="https://objects.example.test",
            access_key="access",
            secret_key="secret",
        ),
    )


def _storage_backend() -> StorageBackend:
    return StorageBackend(
        id=BACKEND_ID,
        label=BACKEND_LABEL,
        provider="tamoss",
        region="us-east-1",
        store_product="s3",
        default_storage=True,
        bucket_name="tamoss-performance",
        endpoint_url="https://objects.internal.example.test",
        public_endpoint_url="https://objects.example.test",
        access_key="access",
        secret_key="secret",
    )


def _flow(flow_id: UUID) -> FlowRecord:
    return FlowRecord(
        id=flow_id,
        data={"label": "Performance shape"},
        source_id=uuid4(),
        format="urn:x-nmos:format:video",
        container="video/mp2t",
    )


def _object_id(index: int) -> str:
    return f"bbc/performance/segment-{index:04d}.ts"


def _timerange(index: int) -> str:
    return f"[{index}:0_{index + 1}:0)"
