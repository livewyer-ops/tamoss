from __future__ import annotations

from tamoss.application.contexts._shared import (
    UUID,
    Any,
    BadRequest,
    FlowRecord,
    Iterable,
    NotFound,
    Page,
    SourceRecord,
    SourceRelationships,
    TagValue,
    UseCaseContext,
    _collection_child_id,
    _collection_role,
    _flow_collection,
    _valid_tag_value,
    utc_now,
    validation,
)


class SourceUseCases(UseCaseContext):
    def list_sources(
        self,
        *,
        label: str | None,
        format: str | None,
        tag_values: dict[str, set[str]],
        tag_exists: dict[str, bool],
        page: str | None,
        limit: int | None,
    ) -> Page[SourceRecord]:
        try:
            validation.validate_content_format_filter(format)
        except ValueError as exc:
            raise BadRequest("Bad request. Invalid query options.") from exc
        return self.repository.list_sources_page(
            label=label,
            format=format,
            tag_values=tag_values,
            tag_exists=tag_exists,
            page=page,
            limit=limit,
        )

    def get_source(self, source_id: UUID) -> SourceRecord:
        source = self.repository.get_source(source_id)
        if source is None:
            raise NotFound("The requested Source does not exist.")
        return source

    def source_relationships(
        self, source_ids: Iterable[UUID] | None = None
    ) -> dict[UUID, SourceRelationships]:
        if source_ids is not None:
            return self.repository.source_relationships_for(source_ids)

        relationships: dict[UUID, SourceRelationships] = {}

        def relationship_for(source_id: UUID) -> SourceRelationships:
            return relationships.setdefault(source_id, SourceRelationships([], []))

        flows_by_id = {flow.id: flow for flow in self.repository.list_flows()}
        for parent_flow in flows_by_id.values():
            if parent_flow.source_id is None:
                continue
            collection = _flow_collection(parent_flow)
            if not collection:
                continue

            parent_relationship = relationship_for(parent_flow.source_id)
            for item in collection:
                child_flow_id = _collection_child_id(item)
                if child_flow_id is None:
                    continue
                child_flow = flows_by_id.get(child_flow_id)
                if child_flow is None or child_flow.source_id is None:
                    continue
                role = _collection_role(item)
                if role is None:
                    continue

                source_item = {"id": str(child_flow.source_id), "role": role}
                if source_item not in parent_relationship.source_collection:
                    parent_relationship.source_collection.append(source_item)

                child_relationship = relationship_for(child_flow.source_id)
                if parent_flow.source_id not in child_relationship.collected_by:
                    child_relationship.collected_by.append(parent_flow.source_id)

        return relationships

    def get_source_property(self, source_id: UUID, property_name: str) -> str:
        source = self.get_source(source_id)
        value = getattr(source, property_name)
        if value is None:
            raise NotFound("The requested Source property does not exist.")
        return value

    def set_source_property(
        self, source_id: UUID, property_name: str, value: str
    ) -> None:
        if not isinstance(value, str):
            raise BadRequest("Bad request. Invalid Source property value.")
        source = self.get_source(source_id)
        setattr(source, property_name, value)
        source.metadata_updated = utc_now()
        self.repository.save_source(source)
        self._publish_source_event("sources/updated", source)

    def delete_source_property(self, source_id: UUID, property_name: str) -> None:
        source = self.get_source(source_id)
        setattr(source, property_name, None)
        source.metadata_updated = utc_now()
        self.repository.save_source(source)
        self._publish_source_event("sources/updated", source)

    def get_source_tags(self, source_id: UUID) -> dict[str, TagValue]:
        return self.get_source(source_id).tags

    def get_source_tag(self, source_id: UUID, name: str) -> TagValue:
        source = self.get_source(source_id)
        if name not in source.tags:
            raise NotFound("The requested Source tag does not exist.")
        return source.tags[name]

    def set_source_tag(self, source_id: UUID, name: str, value: TagValue) -> None:
        if not _valid_tag_value(value):
            raise BadRequest("Bad request. Invalid Source tag value.")
        source = self.get_source(source_id)
        source.tags[name] = value
        source.metadata_updated = utc_now()
        self.repository.save_source(source)
        self._publish_source_event("sources/updated", source)

    def delete_source_tag(self, source_id: UUID, name: str) -> None:
        source = self.get_source(source_id)
        source.tags.pop(name, None)
        source.metadata_updated = utc_now()
        self.repository.save_source(source)
        self._publish_source_event("sources/updated", source)

    def _source_for_flow(self, flow: FlowRecord) -> SourceRecord | None:
        if flow.source_id is None:
            return None
        return self.repository.get_source(flow.source_id)

    def _source_collected_by_ids(self, source: SourceRecord | None) -> list[str]:
        if source is None:
            return []
        relationship = self.source_relationships().get(source.id)
        if relationship is None:
            return []
        return [str(source_id) for source_id in relationship.collected_by]

    def _source_payload(self, source: SourceRecord) -> dict[str, Any]:
        payload = {
            "id": str(source.id),
            "format": source.format,
            "label": source.label,
            "description": source.description,
            "tags": source.tags,
        }
        return {key: value for key, value in payload.items() if value is not None}
