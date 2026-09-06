from __future__ import annotations

from collections.abc import Iterable, Iterator
from contextlib import contextmanager
from typing import Literal
from uuid import UUID

from tamoss.application import webhooks as webhooking
from tamoss.application.contexts.flows import validate_content_format_filter
from tamoss.domain.listings import SourceSortBy
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
        collected_by_ids: set[UUID] | None,
        top_level_only: bool,
        sort_by: SourceSortBy,
        reverse_order: bool,
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
            collected_by_ids=collected_by_ids,
            top_level_only=top_level_only,
            sort_by=sort_by,
            reverse_order=reverse_order,
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
        self, source_ids: Iterable[UUID]
    ) -> dict[UUID, SourceRelationships]:
        return self.repository.source_relationships_for(source_ids)

    def get_source_property(
        self, source_id: UUID, property_name: SourcePropertyName
    ) -> str:
        value = getattr(self.get_source(source_id), property_name)
        if value is None:
            return ""
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
        with self._edit_source(source_id) as source:
            setattr(source, property_name, value)

    def delete_source_property(
        self, source_id: UUID, property_name: SourcePropertyName
    ) -> None:
        with self._edit_source(source_id) as source:
            setattr(source, property_name, None)

    @contextmanager
    def _edit_source(self, source_id: UUID) -> Iterator[SourceRecord]:
        with self.repository.unit_of_work():
            self.repository.lock_source(source_id)
            source = self.get_source(source_id)
            yield source
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
        with self._edit_source(source_id) as source:
            source.tags[name] = value

    def delete_source_tag(self, source_id: UUID, name: str) -> None:
        with self._edit_source(source_id) as source:
            source.tags.pop(name, None)
