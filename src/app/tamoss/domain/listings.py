from __future__ import annotations

from collections.abc import Callable, Iterable
from enum import StrEnum
from typing import Any


class ListingSortBy(StrEnum):
    @property
    def default_descending(self) -> bool:
        return self.value != "label"

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


def sorted_listing[T](
    items: Iterable[T],
    *,
    value: Callable[[T], Any | None],
    identity: Callable[[T], str],
    descending: bool,
) -> list[T]:
    present: list[tuple[T, Any]] = []
    missing: list[T] = []
    for item in items:
        sort_value = value(item)
        if sort_value is None:
            missing.append(item)
        else:
            present.append((item, sort_value))

    # The identity sort is a stable tie-breaker for paged listings.
    present.sort(key=lambda pair: identity(pair[0]))
    present.sort(key=lambda pair: pair[1], reverse=descending)
    missing.sort(key=identity)
    return [item for item, _sort_value in present] + missing
