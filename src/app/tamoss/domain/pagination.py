from __future__ import annotations

from dataclasses import dataclass
from typing import Generic, Sequence, TypeVar

from tamoss.errors import BadRequest

T = TypeVar("T")
DEFAULT_LIMIT = 100
MAX_LIMIT = 1000


@dataclass(frozen=True)
class Page(Generic[T]):
    items: list[T]
    limit: int
    next_page: str | None = None
    timerange: str | None = None


@dataclass(frozen=True)
class PageWindow:
    offset: int
    limit: int


def resolve_page_window(*, page: str | None, limit: int | None) -> PageWindow:
    resolved_limit = limit or DEFAULT_LIMIT
    if resolved_limit < 1:
        raise BadRequest("limit must be greater than zero.")
    resolved_limit = min(resolved_limit, MAX_LIMIT)

    try:
        offset = int(page) if page else 0
    except ValueError as exc:
        raise BadRequest(
            "page must be an opaque token issued by this service."
        ) from exc
    if offset < 0:
        raise BadRequest("page token is invalid.")

    return PageWindow(offset=offset, limit=resolved_limit)


def page_sequence(
    items: Sequence[T], *, page: str | None, limit: int | None
) -> Page[T]:
    window = resolve_page_window(page=page, limit=limit)

    chunk = list(items[window.offset : window.offset + window.limit])
    next_offset = window.offset + len(chunk)
    next_page = str(next_offset) if next_offset < len(items) else None
    return Page(items=chunk, limit=window.limit, next_page=next_page)
