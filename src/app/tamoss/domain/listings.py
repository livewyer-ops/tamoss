from __future__ import annotations

from collections.abc import Callable, Iterable
from enum import StrEnum
from typing import Any
from uuid import UUID

from tamoss.errors import BadRequest


class ListingSortBy(StrEnum):
    @property
    def default_descending(self) -> bool:
        return self.value not in {"label", "url"}

    def descending(self, *, reverse_order: bool) -> bool:
        return self.default_descending != reverse_order


class FlowSortBy(ListingSortBy):
    CREATED = "created"
    METADATA_UPDATED = "metadata_updated"
    LABEL = "label"


class SourceSortBy(ListingSortBy):
    CREATED = "created"
    UPDATED = "updated"
    LABEL = "label"


class DeleteRequestSortBy(ListingSortBy):
    CREATED = "created"
    EXPIRY = "expiry"


def parse_collected_by_ids(raw: str | None) -> tuple[set[UUID] | None, bool]:
    if raw is None:
        return None, False
    if raw == "":
        return set(), True
    try:
        values = {UUID(value) for value in raw.split(",") if value}
    except ValueError as exc:
        raise BadRequest("Bad request. Invalid query options.") from exc
    if not values:
        raise BadRequest("Bad request. Invalid query options.")
    return values, False


def sorted_listing[T](
    items: Iterable[T],
    *,
    value: Callable[[T], Any | None],
    identity: Callable[[T], str],
    descending: bool,
    missing_first: bool,
) -> list[T]:
    present: list[tuple[T, Any]] = []
    missing: list[T] = []
    for item in items:
        sort_value = value(item)
        if sort_value is None:
            missing.append(item)
        else:
            present.append((item, sort_value))

    present.sort(key=lambda pair: identity(pair[0]), reverse=descending)
    present.sort(key=lambda pair: pair[1], reverse=descending)
    missing.sort(key=identity, reverse=descending)
    populated = [item for item, _sort_value in present]
    return missing + populated if missing_first else populated + missing
