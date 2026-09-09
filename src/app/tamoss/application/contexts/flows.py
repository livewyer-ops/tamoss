from __future__ import annotations

from collections.abc import Iterable, Iterator
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import datetime
from typing import Any, Literal
from uuid import UUID, uuid4

from mediatimestamp import TimeRange
from pydantic import BaseModel, ValidationError

from tamoss.application import webhooks as webhooking
from tamoss.auth import Identity
from tamoss.contract.generated import contract_models
from tamoss.contract.serialization import contract_dump
from tamoss.contract.validation import (
    reject_explicit_nulls,
    reject_model_explicit_nulls,
    strict_contract_model,
)
from tamoss.domain.flow_collections import (
    collected_by_by_flow_id,
    collection_aware_flow_timeranges,
    collection_child_id,
    flow_collection,
    flow_with_collected_by,
)
from tamoss.domain.listings import FlowSortBy
from tamoss.domain.model import FlowRecord, MediaObjectRecord, SourceRecord, utc_now
from tamoss.domain.pagination import Page
from tamoss.domain.tags import TagValue, valid_tag_value
from tamoss.domain.timeranges import normalized_timerange_bounds
from tamoss.errors import BadRequest, Forbidden, NotFound
from tamoss.ports.repositories import (
    FlowCollectionRepository,
    FlowLookupRepository,
    FlowRepository,
    ProfileRepository,
    WebhookEventRepository,
)

FlowPropertyName = Literal[
    "label",
    "description",
    "avg_bit_rate",
    "max_bit_rate",
    "read_only",
]
FlowDeletablePropertyName = Literal[
    "label",
    "description",
    "avg_bit_rate",
    "max_bit_rate",
]
_SERVER_MANAGED_FLOW_FIELDS = {
    "timerange",
    "collected_by",
    "created",
    "metadata_updated",
    "segments_updated",
    "created_by",
    "updated_by",
}
VALID_FLOW_FORMATS = {item.value for item in contract_models.ContentFormat}
INIT_SEGMENTS_FLOW_FORMATS = {
    "urn:x-nmos:format:video",
    "urn:x-nmos:format:audio",
    "urn:x-nmos:format:data",
    "urn:x-nmos:format:multi",
}
_CLOSED_ESSENCE_FIELDS = {
    "urn:x-nmos:format:video": frozenset(
        contract_models.EssenceParameters.model_fields
    ),
    "urn:x-nmos:format:audio": frozenset(
        contract_models.EssenceParameters1.model_fields
    ),
    "urn:x-tam:format:image": frozenset(
        contract_models.EssenceParameters2.model_fields
    ),
    "urn:x-nmos:format:data": frozenset(
        contract_models.EssenceParameters3.model_fields
    ),
    "urn:x-nmos:format:multi": frozenset(
        contract_models.EssenceParameters4.model_fields
    ),
}
_FLOW_TECHNICAL_MODELS: dict[str, type[BaseModel]] = {
    "urn:x-nmos:format:video": contract_models.FlowVideo,
    "urn:x-nmos:format:audio": contract_models.FlowAudio,
    "urn:x-tam:format:image": contract_models.FlowImage,
    "urn:x-nmos:format:data": contract_models.FlowData,
    "urn:x-nmos:format:multi": contract_models.FlowMulti,
}


@dataclass(frozen=True)
class QueryTimerange:
    start: int | None = None
    end: int | None = None
    is_empty: bool = False
    is_point: bool = False


def parse_query_timerange(timerange: str | None) -> TimeRange | None:
    if timerange is None or timerange in {"", "_"}:
        return None
    try:
        return TimeRange.from_str(timerange)
    except Exception as exc:
        raise BadRequest("Bad request. Invalid query options.") from exc


def query_timerange(timerange: str | None) -> QueryTimerange:
    requested_range = parse_query_timerange(timerange)
    if requested_range is None:
        return QueryTimerange()
    bounds = normalized_timerange_bounds(requested_range)
    if bounds.is_empty:
        return QueryTimerange(is_empty=True)

    return QueryTimerange(
        start=bounds.start,
        end=bounds.end,
        is_point=bounds.is_point,
    )


def strip_server_managed_flow_fields(
    data: dict[str, Any], *, preserve_metadata_version: bool = False
) -> None:
    for field_name in _SERVER_MANAGED_FLOW_FIELDS:
        data.pop(field_name, None)
    if not preserve_metadata_version:
        data.pop("metadata_version", None)


def touch_flow_metadata(
    flow: FlowRecord,
    *,
    identity: Identity | None = None,
    when: datetime | None = None,
) -> None:
    timestamp = when or utc_now()
    flow.metadata_updated = timestamp
    flow.data["metadata_updated"] = timestamp.isoformat()
    flow.data["metadata_version"] = str(uuid4())
    if identity is not None:
        flow.data["updated_by"] = identity.subject


def validate_content_format_filter(value: str | None) -> None:
    if value is not None and value not in VALID_FLOW_FORMATS:
        raise ValueError("format must be a supported BBC content format")


_TECHNICAL_FLOW_FIELDS = {
    "format",
    "codec",
    "container",
    "avg_bit_rate",
    "segment_duration",
    "container_mapping",
    "essence_parameters",
}
_FLOW_CONTRACT_FIELDS = (
    frozenset(contract_models.FlowCommon.model_fields)
    | _TECHNICAL_FLOW_FIELDS
    | {"profile_id"}
)


def validate_flow_payload(payload: dict[str, Any]) -> dict[str, Any]:
    validate_flow_technical_metadata(payload)
    try:
        flow = strict_contract_model(
            contract_models.FlowGet,
            payload,
            recursive_non_nullable_fields=_FLOW_CONTRACT_FIELDS,
        )
    except (TypeError, ValidationError) as exc:
        raise ValueError("flow payload does not match the BBC TAMS contract") from exc
    return contract_dump(flow, exclude_unset=True)


def validate_flow_technical_metadata(payload: dict[str, Any]) -> None:
    reject_explicit_nulls(payload, _TECHNICAL_FLOW_FIELDS)
    format_value = payload.get("format")
    technical_model = (
        _FLOW_TECHNICAL_MODELS.get(format_value)
        if isinstance(format_value, str)
        else None
    )
    if technical_model is not None:
        reject_model_explicit_nulls(
            technical_model,
            payload,
            field_names=_TECHNICAL_FLOW_FIELDS,
        )
    allowed_fields = (
        _CLOSED_ESSENCE_FIELDS.get(format_value)
        if isinstance(format_value, str)
        else None
    )
    essence_parameters = payload.get("essence_parameters")
    if (
        "essence_parameters" in payload
        and essence_parameters is None
        and allowed_fields is not None
    ):
        raise ValueError("essence_parameters must not be null")
    if not isinstance(essence_parameters, dict):
        return

    if allowed_fields is not None and not essence_parameters.keys() <= allowed_fields:
        raise ValueError("essence_parameters contains unsupported fields")

    if any(value is None for value in essence_parameters.values()):
        raise ValueError("essence_parameters fields must not be null")
    if "init_segments" in essence_parameters and (
        not isinstance(format_value, str)
        or format_value not in INIT_SEGMENTS_FLOW_FORMATS
    ):
        raise ValueError("init_segments is not supported by this Flow format")

    if format_value == "urn:x-nmos:format:video":
        variable_frame_rate = essence_parameters.get("vfr", False)
        has_frame_rate = "frame_rate" in essence_parameters
        if variable_frame_rate is True and has_frame_rate:
            raise ValueError("frame_rate must not be set when vfr is true")
        if variable_frame_rate is not True and not has_frame_rate:
            raise ValueError("frame_rate is required when vfr is false or omitted")


def ensure_flow_writable(flow: FlowRecord) -> None:
    if flow.read_only:
        raise Forbidden(
            "Forbidden. You do not have permission to modify this Flow. "
            "It may be marked read-only."
        )


def require_flow(repository: FlowLookupRepository, flow_id: UUID) -> FlowRecord:
    flow = repository.get_flow(flow_id)
    if flow is None:
        raise NotFound("The requested Flow does not exist.")
    return flow


def unlink_flow_collection_references(
    repository: FlowCollectionRepository,
    flow: FlowRecord,
) -> None:
    for parent in repository.list_flows_collecting([flow.id]):
        if parent.id == flow.id:
            continue
        collection = flow_collection(parent)
        remaining = [
            dict(item) for item in collection if collection_child_id(item) != flow.id
        ]
        if len(remaining) == len(collection):
            continue
        if remaining:
            parent.data["flow_collection"] = remaining
        else:
            parent.data.pop("flow_collection", None)
        repository.save_flow(parent)


def _optional_uuid_value(value: object) -> UUID | None:
    return UUID(str(value)) if value is not None else None


def _requested_profile_id(data: dict[str, Any]) -> UUID | Literal[""] | None:
    if "profile_id" not in data:
        return None
    value = data["profile_id"]
    if value == "":
        return ""
    try:
        return UUID(str(value))
    except (TypeError, ValueError) as exc:
        raise BadRequest("Bad request. Invalid Profile ID.") from exc


def _flow_init_segments(data: dict[str, Any]) -> bool:
    essence = data.get("essence_parameters")
    if not isinstance(essence, dict):
        return False
    return bool(essence.get("init_segments", False))


class FlowUseCases:
    repository: FlowRepository
    webhook_repository: WebhookEventRepository
    profile_repository: ProfileRepository

    def __init__(
        self,
        *,
        repository: FlowRepository,
        profile_repository: ProfileRepository,
        webhook_repository: WebhookEventRepository,
    ) -> None:
        self.repository = repository
        self.profile_repository = profile_repository
        self.webhook_repository = webhook_repository

    def list_flows(
        self,
        *,
        source_id: UUID | None,
        timerange: str | None,
        format: str | None,
        profile_id: UUID | None,
        status: contract_models.FlowStatus | None,
        init_segments: bool | None,
        collected_by_ids: set[UUID] | None,
        top_level_only: bool,
        sort_by: FlowSortBy,
        reverse_order: bool,
        codec: str | None,
        label: str | None,
        frame_width: int | None,
        frame_height: int | None,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[FlowRecord]:
        try:
            validate_content_format_filter(format)
        except ValueError as exc:
            raise BadRequest("Bad request. Invalid query options.") from exc
        requested_timerange = query_timerange(timerange)
        return self.repository.list_flows_page(
            source_id=source_id,
            timerange_start=requested_timerange.start,
            timerange_end=requested_timerange.end,
            timerange_is_empty=requested_timerange.is_empty,
            timerange_is_point=requested_timerange.is_point,
            format=format,
            profile_id=profile_id,
            status=status.value if status is not None else None,
            init_segments=init_segments,
            collected_by_ids=collected_by_ids,
            top_level_only=top_level_only,
            sort_by=sort_by,
            reverse_order=reverse_order,
            codec=codec,
            label=label,
            frame_width=frame_width,
            frame_height=frame_height,
            tag_values=tag_values,
            tag_exists=tag_exists,
            page=page,
            limit=limit,
        )

    def flow_timerange(self, flow_id: UUID, timerange: str | None = None) -> str:
        flow = self.get_flow(flow_id)
        try:
            flow_range = TimeRange.from_str(
                self._flow_timeranges([flow_id], seed_flows=[flow])[flow_id]
            )
        except ValueError as exc:
            raise BadRequest("Bad request. Invalid stored Segment timerange.") from exc
        requested_range = parse_query_timerange(timerange)
        if requested_range is None:
            return str(flow_range)
        return str(flow_range.intersect_with(requested_range))

    def flow_timeranges(
        self, flow_ids: Iterable[UUID], *, seed_flows: Iterable[FlowRecord] = ()
    ) -> dict[UUID, str]:
        return self._flow_timeranges(flow_ids, seed_flows=seed_flows)

    def _flow_timeranges(
        self,
        flow_ids: Iterable[UUID],
        *,
        seed_flows: Iterable[FlowRecord] = (),
    ) -> dict[UUID, str]:
        requested_ids = list(dict.fromkeys(flow_ids))
        if not requested_ids:
            return {}
        flows = self._flows_for_timerange_resolution(requested_ids, seed_flows)
        direct_timeranges = self.repository.flow_timeranges(flow.id for flow in flows)
        try:
            return collection_aware_flow_timeranges(
                flows,
                direct_timeranges,
                requested_ids,
            )
        except ValueError as exc:
            raise BadRequest("Bad request. Invalid stored Segment timerange.") from exc

    def _flows_for_timerange_resolution(
        self,
        flow_ids: Iterable[UUID],
        seed_flows: Iterable[FlowRecord] = (),
    ) -> list[FlowRecord]:
        flows_by_id = {flow.id: flow for flow in seed_flows}
        pending = list(dict.fromkeys(flow_ids))
        index = 0
        while index < len(pending):
            flow_id = pending[index]
            index += 1
            flow = flows_by_id.get(flow_id)
            if flow is None:
                flow = self.repository.get_flow(flow_id)
                if flow is None:
                    continue
                flows_by_id[flow.id] = flow
            for item in flow_collection(flow):
                child_id = collection_child_id(item)
                if child_id is not None and child_id not in flows_by_id:
                    pending.append(child_id)
        return list(flows_by_id.values())

    def get_flow(
        self, flow_id: UUID, *, include_collected_by: bool = False
    ) -> FlowRecord:
        flow = require_flow(self.repository, flow_id)
        if include_collected_by:
            return self._flow_with_collected_by(flow)
        return flow

    def get_flow_collection(self, flow_id: UUID) -> list[dict]:
        flow = self.get_flow(flow_id)
        return flow_collection(flow)

    def set_flow_collection(
        self,
        *,
        flow_id: UUID,
        collection: list[dict[str, Any]],
        identity: Identity,
    ) -> None:
        with self._edit_flow(flow_id, identity) as flow:
            ensure_flow_writable(flow)
            self._replace_flow_collection(flow, collection)

    @contextmanager
    def _edit_flow(
        self, flow_id: UUID, identity: Identity | None
    ) -> Iterator[FlowRecord]:
        with self.repository.unit_of_work():
            self.repository.lock_flow_segments(flow_id)
            flow = self.get_flow(flow_id)
            yield flow
            touch_flow_metadata(flow, identity=identity)
            self.repository.save_flow(flow)
            webhooking.publish_flow_event(
                repository=self.webhook_repository,
                resource_repository=self.repository,
                event_type="flows/updated",
                flow=flow,
            )

    def delete_flow_collection(self, *, flow_id: UUID, identity: Identity) -> None:
        self.set_flow_collection(flow_id=flow_id, collection=[], identity=identity)

    def get_flow_property(
        self, flow_id: UUID, property_name: FlowPropertyName
    ) -> str | int | bool | None:
        flow = self.get_flow(flow_id)
        if property_name == "read_only":
            return flow.read_only
        value = flow.data.get(property_name)
        if value is None:
            # Unset properties are readable once the Flow exists: empty string
            # for the string properties, null for the numeric ones (the spec
            # reserves 404 for a missing Flow and defines no unset form).
            if property_name in {"label", "description"}:
                return ""
            return None
        if property_name in {"label", "description"}:
            if not isinstance(value, str):
                raise BadRequest("Bad request. Invalid Flow property value.")
            return value
        if not isinstance(value, int) or isinstance(value, bool):
            raise BadRequest("Bad request. Invalid Flow property value.")
        return value

    def set_flow_property(
        self,
        flow_id: UUID,
        property_name: FlowPropertyName,
        value: str | int | bool,
        *,
        identity: Identity | None = None,
    ) -> None:
        with self._edit_flow(flow_id, identity) as flow:
            if property_name in {"label", "description"}:
                ensure_flow_writable(flow)
                if not isinstance(value, str):
                    raise BadRequest("Bad request. Invalid Flow property value.")
            elif property_name in {"avg_bit_rate", "max_bit_rate"}:
                ensure_flow_writable(flow)
                if property_name == "avg_bit_rate" and flow.profile_id is not None:
                    raise BadRequest(
                        "Bad request. Profile-backed Flows cannot override "
                        "average bit rate."
                    )
                if not isinstance(value, int) or isinstance(value, bool) or value < 0:
                    raise BadRequest("Bad request. Invalid Flow bit rate.")
            else:
                if not isinstance(value, bool):
                    raise BadRequest("Bad request. Invalid Flow read_only value.")
                flow.read_only = value
            flow.data[property_name] = value

    def delete_flow_property(
        self,
        flow_id: UUID,
        property_name: FlowDeletablePropertyName,
        *,
        identity: Identity | None = None,
    ) -> None:
        with self._edit_flow(flow_id, identity) as flow:
            ensure_flow_writable(flow)
            if property_name == "avg_bit_rate" and flow.profile_id is not None:
                raise BadRequest(
                    "Bad request. Profile-backed Flows cannot override "
                    "average bit rate."
                )
            flow.data.pop(property_name, None)

    def get_flow_tags(self, flow_id: UUID) -> dict[str, TagValue]:
        return self.get_flow(flow_id).tags

    def get_flow_tag(self, flow_id: UUID, name: str) -> TagValue:
        flow = self.get_flow(flow_id)
        if name not in flow.tags:
            raise NotFound("The requested Flow tag does not exist.")
        return flow.tags[name]

    def set_flow_tag(
        self,
        flow_id: UUID,
        name: str,
        value: TagValue,
        *,
        identity: Identity | None = None,
    ) -> None:
        if not valid_tag_value(value):
            raise BadRequest("Bad request. Invalid Flow tag value.")
        with self._edit_flow(flow_id, identity) as flow:
            ensure_flow_writable(flow)
            flow.tags[name] = value
            flow.data["tags"] = flow.tags

    def delete_flow_tag(
        self,
        flow_id: UUID,
        name: str,
        *,
        identity: Identity | None = None,
    ) -> None:
        with self._edit_flow(flow_id, identity) as flow:
            ensure_flow_writable(flow)
            flow.tags.pop(name, None)
            flow.data["tags"] = flow.tags

    def _replace_flow_collection(
        self, flow: FlowRecord, collection: list[dict[str, Any]] | None
    ) -> None:
        payload = self._validate_flow_collection(flow, collection or [])
        if payload:
            flow.data["flow_collection"] = payload
        else:
            flow.data.pop("flow_collection", None)

    def _validate_flow_collection(
        self, flow: FlowRecord, collection: list[dict[str, Any]]
    ) -> list[dict]:
        payload: list[dict] = []
        seen: set[UUID] = set()
        for item in collection:
            child_id = UUID(str(item["id"]))
            if child_id == flow.id:
                raise BadRequest("Bad request. Invalid flow collection.")
            if child_id in seen:
                raise BadRequest("Bad request. Invalid flow collection.")
            seen.add(child_id)

            child = self.repository.get_flow(child_id)
            if child is None:
                raise BadRequest("Bad request. Invalid flow collection.")

            payload.append(dict(item))
        return payload

    def put_flow(
        self,
        *,
        flow_id: UUID,
        flow: dict[str, Any],
        supplied_fields: set[str] | None = None,
        identity: Identity,
    ) -> tuple[FlowRecord, bool]:
        with self.repository.unit_of_work():
            # Segment registration takes the same per-Flow lock. Keeping the
            # capability check and Flow save inside it prevents a concurrent
            # first Segment from racing an init_segments transition.
            self.repository.lock_flow_segments(flow_id)
            return self._put_flow_locked(
                flow_id=flow_id,
                flow=flow,
                supplied_fields=supplied_fields,
                identity=identity,
            )

    def _put_flow_locked(
        self,
        *,
        flow_id: UUID,
        flow: dict[str, Any],
        supplied_fields: set[str] | None,
        identity: Identity,
    ) -> tuple[FlowRecord, bool]:
        try:
            body_flow_id = UUID(str(flow.get("id")))
        except TypeError, ValueError:
            raise NotFound("The requested Flow ID in the path is invalid.") from None
        if body_flow_id != flow_id:
            raise NotFound("The requested Flow ID in the path is invalid.")
        supplied_fields = supplied_fields or set(flow)
        flow_collection_supplied = "flow_collection" in supplied_fields
        existing = self.repository.get_flow(flow_id)
        if existing is not None:
            ensure_flow_writable(existing)

        data = dict(flow)
        tags_supplied = "tags" in data
        strip_server_managed_flow_fields(
            data,
            preserve_metadata_version=existing is None,
        )

        requested_profile_id = _requested_profile_id(data)
        unlinking_profile = False
        inherited_profile_fields: set[str] = set()
        if existing is None and requested_profile_id == "":
            raise BadRequest("Bad request. A new Flow cannot unlink a Profile.")

        if existing is None and isinstance(requested_profile_id, UUID):
            profile_id = requested_profile_id
            self.profile_repository.lock_profile(profile_id)
            profile = self.profile_repository.get_profile(profile_id)
            if profile is None:
                raise BadRequest("Bad request. Profile does not exist.")
            supplied_technical = (
                _TECHNICAL_FLOW_FIELDS | set(profile.flow_metadata)
            ).intersection(supplied_fields)
            if supplied_technical:
                raise BadRequest(
                    "Bad request. Profile-backed Flows cannot override "
                    "technical metadata."
                )
            data = {**data, **profile.flow_metadata, "profile_id": str(profile_id)}
        elif existing is not None and existing.profile_id is None:
            if requested_profile_id == "":
                raise BadRequest("Bad request. The Flow is not Profile-backed.")
            if isinstance(requested_profile_id, UUID):
                raise BadRequest(
                    "Bad request. A Profile cannot be attached to an existing Flow."
                )
        elif existing is not None and existing.profile_id is not None:
            if (
                isinstance(requested_profile_id, UUID)
                and requested_profile_id != existing.profile_id
            ):
                raise BadRequest(
                    "Bad request. A Flow cannot be re-pointed to another Profile."
                )
            profile = self.profile_repository.get_profile(existing.profile_id)
            if profile is None:
                raise BadRequest("Bad request. Stored Flow Profile does not exist.")
            if requested_profile_id == "":
                unlinking_profile = True
                inherited_profile_fields = set(profile.flow_metadata)
                data.pop("profile_id", None)
            else:
                supplied_technical = (
                    _TECHNICAL_FLOW_FIELDS | set(profile.flow_metadata)
                ).intersection(supplied_fields)
                if supplied_technical:
                    raise BadRequest(
                        "Bad request. Profile-backed Flows cannot override "
                        "technical metadata."
                    )
                data = {
                    **data,
                    **profile.flow_metadata,
                    "profile_id": str(existing.profile_id),
                }

        if existing is not None and not unlinking_profile:
            data = self._flow_update_payload(existing, data)

        try:
            data = validate_flow_payload(data)
        except ValueError as exc:
            raise BadRequest("Bad request. Invalid Flow JSON.") from exc

        if (
            existing is not None
            and _flow_init_segments(data) != existing.init_segments
            and self.repository.has_segments(flow_id)
        ):
            raise BadRequest(
                "Bad request. init_segments cannot change after Segments exist."
            )

        flow_collection = data.pop("flow_collection", None)
        replacement_tags = dict(data.get("tags") or {})
        source_id = UUID(str(data["source_id"]))
        format_value = data["format"]
        source_was_created = False
        source = self.repository.get_source(source_id)
        if source is None:
            source = SourceRecord(
                id=source_id,
                format=format_value,
                label=data.get("label"),
                description=data.get("description"),
                tags=replacement_tags,
            )
            source_was_created = True
        else:
            if source.format != format_value:
                raise BadRequest("Bad request. Invalid Flow JSON.")
            source.metadata_updated = utc_now()
        self.repository.save_source(source)

        now = utc_now()
        if existing is None:
            data.setdefault("created_by", identity.subject)
            data.setdefault("created", now.isoformat())
            data.setdefault("metadata_version", str(uuid4()))
            data["metadata_updated"] = now.isoformat()
            record = FlowRecord(
                id=flow_id,
                data=data,
                source_id=source_id,
                format=format_value,
                container=data.get("container"),
                profile_id=_optional_uuid_value(data.get("profile_id")),
                status=data.get("status"),
                init_segments=_flow_init_segments(data),
                read_only=bool(data.get("read_only")),
                tags=replacement_tags,
                created=now,
                metadata_updated=now,
            )
            created = True
        else:
            data.setdefault(
                "created",
                existing.data.get("created") or existing.created.isoformat(),
            )
            data["metadata_updated"] = now.isoformat()
            data["metadata_version"] = str(uuid4())
            data["updated_by"] = identity.subject
            existing_data = dict(existing.data)
            if unlinking_profile:
                for field_name in (
                    inherited_profile_fields | _TECHNICAL_FLOW_FIELDS | {"profile_id"}
                ):
                    existing_data.pop(field_name, None)
            stored_data = {**existing_data, **data}
            stored_data.pop("collected_by", None)
            if not tags_supplied:
                stored_data.pop("tags", None)
            else:
                stored_data["tags"] = replacement_tags
            record = FlowRecord(
                id=flow_id,
                data=stored_data,
                source_id=source_id,
                format=format_value,
                container=stored_data.get("container"),
                profile_id=_optional_uuid_value(stored_data.get("profile_id")),
                status=stored_data.get("status"),
                init_segments=_flow_init_segments(stored_data),
                read_only=bool(data.get("read_only"))
                if data.get("read_only") is not None
                else existing.read_only,
                tags=replacement_tags,
                created=existing.created,
                metadata_updated=now,
                segments_updated=existing.segments_updated,
            )
            created = False

        if flow_collection_supplied:
            self._replace_flow_collection(record, flow_collection)

        self.repository.save_flow(record)
        if source_was_created:
            webhooking.publish_source_event(
                repository=self.webhook_repository,
                resource_repository=self.repository,
                event_type="sources/created",
                source=source,
            )
        webhooking.publish_flow_event(
            repository=self.webhook_repository,
            resource_repository=self.repository,
            event_type="flows/created" if created else "flows/updated",
            flow=record,
        )
        return record, created

    @staticmethod
    def _flow_update_payload(
        existing: FlowRecord, data: dict[str, Any]
    ) -> dict[str, Any]:
        payload = dict(data)
        if "source_id" not in payload and existing.source_id is not None:
            payload["source_id"] = str(existing.source_id)
        if "format" not in payload and existing.format is not None:
            payload["format"] = existing.format
        if "container" not in payload and existing.container is not None:
            payload["container"] = existing.container
        for field_name in ("codec", "essence_parameters"):
            if field_name not in payload and field_name in existing.data:
                payload[field_name] = existing.data[field_name]
        return payload

    def referenced_flows_matching_tags_page(
        self,
        media_object: MediaObjectRecord,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        *,
        page: str | None,
        limit: int | None,
    ) -> Page[UUID]:
        return self.repository.list_flow_ids_matching_tags_page(
            flow_ids=media_object.referenced_by_flows,
            tag_values=tag_values,
            tag_exists=tag_exists,
            page=page,
            limit=limit,
        )

    def _flow_with_collected_by(self, flow: FlowRecord) -> FlowRecord:
        collected_by_ids = collected_by_by_flow_id(
            self.repository.list_flows_collecting([flow.id])
        ).get(flow.id, [])
        return flow_with_collected_by(flow, collected_by_ids)
