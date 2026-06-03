from __future__ import annotations

from collections.abc import Iterable
from typing import Literal
from uuid import UUID

from tamoss.application import webhooks as webhooking
from tamoss.application.contexts.flows import validate_content_format_filter
from tamoss.domain.flow_collections import (
    collection_child_id,
    collection_role,
    flow_collection,
)
from tamoss.domain.model import SourceRecord, SourceRelationships, utc_now
from tamoss.domain.pagination import Page
from tamoss.domain.tags import TagValue, valid_tag_value
from tamoss.errors import BadRequest, NotFound
from tamoss.ports.repositories import SourceRepository, WebhookEventRepository

SourcePropertyName = Literal["label", "description"]


class SourceUseCases:
    repository: SourceRepository
    webhook_repository: WebhookEventRepository

    def __init__(
        self,
        *,
        repository: SourceRepository,
        webhook_repository: WebhookEventRepository,
    ) -> None:
        self.repository = repository
        self.webhook_repository = webhook_repository

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
            validate_content_format_filter(format)
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
            collection = flow_collection(parent_flow)
            if not collection:
                continue

            parent_relationship = relationship_for(parent_flow.source_id)
            for item in collection:
                child_flow_id = collection_child_id(item)
                if child_flow_id is None:
                    continue
                child_flow = flows_by_id.get(child_flow_id)
                if child_flow is None or child_flow.source_id is None:
                    continue
                role = collection_role(item)
                if role is None:
                    continue

                source_item = {"id": str(child_flow.source_id), "role": role}
                if source_item not in parent_relationship.source_collection:
                    parent_relationship.source_collection.append(source_item)

                child_relationship = relationship_for(child_flow.source_id)
                if parent_flow.source_id not in child_relationship.collected_by:
                    child_relationship.collected_by.append(parent_flow.source_id)

        return relationships

    def get_source_property(
        self, source_id: UUID, property_name: SourcePropertyName
    ) -> str:
        value = getattr(self.get_source(source_id), property_name)
        if value is None:
            if property_name == "label":
                return ""
            raise NotFound("The requested Source property does not exist.")
        if not isinstance(value, str):
            raise BadRequest("Bad request. Invalid Source property value.")
        return value

    def set_source_property(
        self,
        source_id: UUID,
        property_name: SourcePropertyName,
        value: str,
    ) -> None:
        if not isinstance(value, str):
            raise BadRequest("Bad request. Invalid Source property value.")
        source = self.get_source(source_id)
        setattr(source, property_name, value)
        source.metadata_updated = utc_now()
        self.repository.save_source(source)
        webhooking.publish_source_event(
            repository=self.webhook_repository,
            resource_repository=self.repository,
            event_type="sources/updated",
            source=source,
        )

    def delete_source_property(
        self, source_id: UUID, property_name: SourcePropertyName
    ) -> None:
        source = self.get_source(source_id)
        setattr(source, property_name, None)
        source.metadata_updated = utc_now()
        self.repository.save_source(source)
        webhooking.publish_source_event(
            repository=self.webhook_repository,
            resource_repository=self.repository,
            event_type="sources/updated",
            source=source,
        )

    def get_source_tags(self, source_id: UUID) -> dict[str, TagValue]:
        return self.get_source(source_id).tags

    def get_source_tag(self, source_id: UUID, name: str) -> TagValue:
        source = self.get_source(source_id)
        if name not in source.tags:
            raise NotFound("The requested Source tag does not exist.")
        return source.tags[name]

    def set_source_tag(self, source_id: UUID, name: str, value: TagValue) -> None:
        if not valid_tag_value(value):
            raise BadRequest("Bad request. Invalid Source tag value.")
        source = self.get_source(source_id)
        source.tags[name] = value
        source.metadata_updated = utc_now()
        self.repository.save_source(source)
        webhooking.publish_source_event(
            repository=self.webhook_repository,
            resource_repository=self.repository,
            event_type="sources/updated",
            source=source,
        )

    def delete_source_tag(self, source_id: UUID, name: str) -> None:
        source = self.get_source(source_id)
        source.tags.pop(name, None)
        source.metadata_updated = utc_now()
        self.repository.save_source(source)
        webhooking.publish_source_event(
            repository=self.webhook_repository,
            resource_repository=self.repository,
            event_type="sources/updated",
            source=source,
        )
