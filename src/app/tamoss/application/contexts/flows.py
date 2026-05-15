from __future__ import annotations

from tamoss.application.contexts._shared import (
    UUID,
    Any,
    BadRequest,
    FlowCollectionItem,
    FlowRecord,
    FlowWrite,
    Forbidden,
    Identity,
    Iterable,
    MediaObjectRecord,
    NotFound,
    Page,
    SourceRecord,
    TagValue,
    TimeRange,
    UseCaseContext,
    _collected_by_by_flow_id,
    _collection_child_id,
    _flow_collection,
    _flow_with_collected_by,
    _parse_query_timerange,
    _query_timerange,
    _strip_server_managed_flow_fields,
    _valid_tag_value,
    utc_now,
    validation,
)


class FlowUseCases(UseCaseContext):
    def list_flows(
        self,
        *,
        source_id: UUID | None,
        timerange: str | None,
        format: str | None,
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
            validation.validate_content_format_filter(format)
        except ValueError as exc:
            raise BadRequest("Bad request. Invalid query options.") from exc
        query_timerange = _query_timerange(timerange)
        return self.repository.list_flows_page(
            source_id=source_id,
            timerange_start=query_timerange.start,
            timerange_end=query_timerange.end,
            timerange_is_empty=query_timerange.is_empty,
            timerange_is_point=query_timerange.is_point,
            format=format,
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
        self.get_flow(flow_id)
        flow_range = self._flow_timerange(flow_id)
        requested_range = _parse_query_timerange(timerange)
        if requested_range is None:
            return str(flow_range)
        return str(flow_range.intersect_with(requested_range))

    def flow_timeranges(self, flow_ids: Iterable[UUID]) -> dict[UUID, str]:
        requested_ids = list(dict.fromkeys(flow_ids))
        if not requested_ids:
            return {}
        timeranges = self.repository.flow_timeranges(requested_ids)
        return {flow_id: timeranges.get(flow_id, "()") for flow_id in requested_ids}

    def get_flow(
        self, flow_id: UUID, *, include_collected_by: bool = False
    ) -> FlowRecord:
        flow = self.repository.get_flow(flow_id)
        if flow is None:
            raise NotFound("The requested Flow does not exist.")
        if include_collected_by:
            return self._flow_with_collected_by(flow)
        return flow

    def _flow_timerange(self, flow_id: UUID) -> TimeRange:
        segments = self.repository.list_segments(flow_id)
        ranges: list[TimeRange] = []
        for segment in segments:
            try:
                parsed = TimeRange.from_str(segment.timerange)
            except Exception as exc:
                raise BadRequest(
                    "Bad request. Invalid stored Segment timerange."
                ) from exc
            if parsed.start is not None and parsed.end is not None:
                ranges.append(parsed)
        if not ranges:
            return TimeRange.never()
        start = min(
            timerange.start for timerange in ranges if timerange.start is not None
        )
        end = max(timerange.end for timerange in ranges if timerange.end is not None)
        return TimeRange.from_str(f"[{start}_{end})")

    def _flow_timerange_matches(
        self, flow_id: UUID, requested_range: TimeRange
    ) -> bool:
        flow_range = self._flow_timerange(flow_id)
        if requested_range.is_empty():
            return flow_range.is_empty()
        return not flow_range.intersect_with(requested_range).is_empty()

    def get_flow_collection(self, flow_id: UUID) -> list[dict]:
        flow = self.get_flow(flow_id)
        return _flow_collection(flow)

    def set_flow_collection(
        self,
        *,
        flow_id: UUID,
        collection: list[FlowCollectionItem],
        identity: Identity,
    ) -> None:
        flow = self.get_flow(flow_id)
        self._ensure_flow_writable(flow)
        self._replace_flow_collection(flow, collection)
        flow.metadata_updated = utc_now()
        flow.data["metadata_updated"] = flow.metadata_updated.isoformat()
        flow.data["updated_by"] = identity.subject
        self.repository.save_flow(flow)
        self._publish_flow_event("flows/updated", flow)

    def delete_flow_collection(self, *, flow_id: UUID, identity: Identity) -> None:
        self.set_flow_collection(flow_id=flow_id, collection=[], identity=identity)

    def get_flow_property(self, flow_id: UUID, property_name: str) -> str:
        flow = self.get_flow(flow_id)
        value = flow.data.get(property_name)
        if value is None:
            raise NotFound("The requested Flow property does not exist.")
        return value

    def set_flow_property(self, flow_id: UUID, property_name: str, value: str) -> None:
        if not isinstance(value, str):
            raise BadRequest("Bad request. Invalid Flow property value.")
        flow = self.get_flow(flow_id)
        self._ensure_flow_writable(flow)
        flow.data[property_name] = value
        flow.metadata_updated = utc_now()
        self.repository.save_flow(flow)
        self._publish_flow_event("flows/updated", flow)

    def delete_flow_property(self, flow_id: UUID, property_name: str) -> None:
        flow = self.get_flow(flow_id)
        self._ensure_flow_writable(flow)
        flow.data.pop(property_name, None)
        flow.metadata_updated = utc_now()
        self.repository.save_flow(flow)
        self._publish_flow_event("flows/updated", flow)

    def get_flow_int_property(self, flow_id: UUID, property_name: str) -> int:
        flow = self.get_flow(flow_id)
        value = flow.data.get(property_name)
        if value is None:
            raise NotFound("The requested Flow property does not exist.")
        if not isinstance(value, int) or isinstance(value, bool):
            raise BadRequest("Bad request. Invalid Flow property value.")
        return value

    def set_flow_int_property(
        self, flow_id: UUID, property_name: str, value: int
    ) -> None:
        if not isinstance(value, int) or isinstance(value, bool) or value < 0:
            raise BadRequest("Bad request. Invalid Flow bit rate.")
        flow = self.get_flow(flow_id)
        self._ensure_flow_writable(flow)
        flow.data[property_name] = value
        flow.metadata_updated = utc_now()
        self.repository.save_flow(flow)
        self._publish_flow_event("flows/updated", flow)

    def get_flow_tags(self, flow_id: UUID) -> dict[str, TagValue]:
        return self.get_flow(flow_id).tags

    def get_flow_tag(self, flow_id: UUID, name: str) -> TagValue:
        flow = self.get_flow(flow_id)
        if name not in flow.tags:
            raise NotFound("The requested Flow tag does not exist.")
        return flow.tags[name]

    def set_flow_tag(self, flow_id: UUID, name: str, value: TagValue) -> None:
        if not _valid_tag_value(value):
            raise BadRequest("Bad request. Invalid Flow tag value.")
        flow = self.get_flow(flow_id)
        self._ensure_flow_writable(flow)
        flow.tags[name] = value
        flow.data["tags"] = flow.tags
        flow.metadata_updated = utc_now()
        self.repository.save_flow(flow)
        self._publish_flow_event("flows/updated", flow)

    def delete_flow_tag(self, flow_id: UUID, name: str) -> None:
        flow = self.get_flow(flow_id)
        self._ensure_flow_writable(flow)
        flow.tags.pop(name, None)
        flow.data["tags"] = flow.tags
        flow.metadata_updated = utc_now()
        self.repository.save_flow(flow)
        self._publish_flow_event("flows/updated", flow)

    def set_flow_read_only(self, flow_id: UUID, read_only: bool) -> None:
        if not isinstance(read_only, bool):
            raise BadRequest("Bad request. Invalid Flow read_only value.")
        flow = self.get_flow(flow_id)
        flow.read_only = read_only
        flow.data["read_only"] = read_only
        flow.metadata_updated = utc_now()
        self.repository.save_flow(flow)
        self._publish_flow_event("flows/updated", flow)

    def _ensure_flow_writable(self, flow: FlowRecord) -> None:
        if flow.read_only:
            raise Forbidden(
                "Forbidden. You do not have permission to modify this Flow. "
                "It may be marked read-only."
            )

    def _replace_flow_collection(
        self, flow: FlowRecord, collection: list[FlowCollectionItem] | None
    ) -> None:
        payload = self._validate_flow_collection(flow, collection or [])
        if payload:
            flow.data["flow_collection"] = payload
        else:
            flow.data.pop("flow_collection", None)

    def _unlink_flow_collection_references(self, flow: FlowRecord) -> None:
        for parent in self.repository.list_flows():
            if parent.id == flow.id:
                continue
            collection = _flow_collection(parent)
            remaining = [
                dict(item)
                for item in collection
                if _collection_child_id(item) != flow.id
            ]
            if len(remaining) == len(collection):
                continue
            if remaining:
                parent.data["flow_collection"] = remaining
            else:
                parent.data.pop("flow_collection", None)
            self.repository.save_flow(parent)

    def _validate_flow_collection(
        self, flow: FlowRecord, collection: list[FlowCollectionItem]
    ) -> list[dict]:
        payload: list[dict] = []
        seen: set[UUID] = set()
        for item in collection:
            child_id = item.id
            if child_id == flow.id:
                raise BadRequest("Bad request. Invalid flow collection.")
            if child_id in seen:
                raise BadRequest("Bad request. Invalid flow collection.")
            seen.add(child_id)

            child = self.repository.get_flow(child_id)
            if child is None:
                raise BadRequest("Bad request. Invalid flow collection.")

            payload.append(item.model_dump(exclude_none=True, mode="json"))
        return payload

    def put_flow(
        self, *, flow_id: UUID, flow_write: FlowWrite, identity: Identity
    ) -> tuple[FlowRecord, bool]:
        if flow_write.id != flow_id:
            raise BadRequest("Bad request. Invalid Flow JSON.")
        flow_collection_supplied = "flow_collection" in flow_write.model_fields_set
        existing = self.repository.get_flow(flow_id)
        if existing is not None and existing.read_only:
            raise Forbidden(
                "Forbidden. You do not have permission to modify this Flow. "
                "It may be marked read-only."
            )

        data = flow_write.model_dump(by_alias=True, exclude_none=True, mode="json")
        data.pop("flow_collection", None)
        _strip_server_managed_flow_fields(data)
        replacement_tags = dict(flow_write.tags or {})

        if existing is not None:
            data = self._flow_update_payload(existing, data)

        try:
            validation.validate_flow_payload(data)
        except ValueError as exc:
            raise BadRequest("Bad request. Invalid Flow JSON.") from exc

        source_id = UUID(str(data["source_id"]))
        format_value = data["format"]
        source: SourceRecord | None = None
        source_was_created = False
        source = self.repository.get_source(source_id)
        if source is None:
            source = SourceRecord(
                id=source_id,
                format=format_value,
                label=data.get("source_label") or data.get("label"),
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
            record = FlowRecord(
                id=flow_id,
                data=data,
                source_id=source_id,
                format=format_value,
                container=data.get("container"),
                read_only=bool(flow_write.read_only),
                tags=replacement_tags,
                created=now,
                metadata_updated=now,
            )
            created = True
        else:
            data.setdefault("created", existing.data.get("created"))
            data["metadata_updated"] = now.isoformat()
            data.setdefault("updated_by", identity.subject)
            stored_data = {**existing.data, **data}
            stored_data.pop("collected_by", None)
            if flow_write.tags is None:
                stored_data.pop("tags", None)
            else:
                stored_data["tags"] = replacement_tags
            record = FlowRecord(
                id=flow_id,
                data=stored_data,
                source_id=source_id,
                format=format_value,
                container=data.get("container"),
                read_only=bool(flow_write.read_only)
                if flow_write.read_only is not None
                else existing.read_only,
                tags=replacement_tags,
                created=existing.created,
                metadata_updated=now,
                segments_updated=existing.segments_updated,
            )
            created = False

        if flow_collection_supplied:
            self._replace_flow_collection(record, flow_write.flow_collection)

        self.repository.save_flow(record)
        if source is not None and source_was_created:
            self._publish_source_event("sources/created", source)
        self._publish_flow_event(
            "flows/created" if created else "flows/updated", record
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

    def referenced_flows_matching_tags(
        self,
        media_object: MediaObjectRecord,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
    ) -> list[UUID]:
        flow_ids: list[UUID] = []
        page: str | None = None
        while True:
            flow_page = self.referenced_flows_matching_tags_page(
                media_object,
                tag_values,
                tag_exists,
                page=page,
                limit=1000,
            )
            flow_ids.extend(flow_page.items)
            if flow_page.next_page is None:
                return flow_ids
            page = flow_page.next_page

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

    def _flow_collected_by_ids(self, flow: FlowRecord | None) -> list[str]:
        if flow is None:
            return []
        return _collected_by_by_flow_id(self.repository.list_flows()).get(flow.id, [])

    def _flow_with_collected_by(self, flow: FlowRecord) -> FlowRecord:
        return _flow_with_collected_by(flow, self._flow_collected_by_ids(flow))

    def _flow_payload(self, flow: FlowRecord) -> dict[str, Any]:
        payload = dict(self._flow_with_collected_by(flow).data)
        payload["id"] = str(flow.id)
        if flow.source_id is not None:
            payload["source_id"] = str(flow.source_id)
        if flow.format is not None:
            payload["format"] = flow.format
        if flow.container is not None:
            payload["container"] = flow.container
        payload["read_only"] = flow.read_only
        payload["tags"] = flow.tags
        if flow.segments_updated is not None:
            payload["segments_updated"] = flow.segments_updated.isoformat()
        return {key: value for key, value in payload.items() if value is not None}
