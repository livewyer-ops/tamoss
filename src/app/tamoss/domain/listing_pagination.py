from __future__ import annotations

import base64
import binascii
import json
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from datetime import UTC, datetime
from uuid import UUID

from tamoss.domain.listings import ListingSortBy
from tamoss.domain.pagination import Page, resolve_page_window
from tamoss.errors import BadRequest

_PREFIX = "k1."


def listing_value(value: datetime | str | None) -> str | None:
    return value.astimezone(UTC).isoformat() if isinstance(value, datetime) else value


@dataclass(frozen=True)
class ListingWindow:
    context: str
    limit: int
    offset: int
    descending: bool
    missing_first: bool
    anchor_id: UUID | None = None
    anchor_value: str | None = None

    def follows(self, value: datetime | str | None, identity: UUID) -> bool:
        if self.anchor_id is None:
            return True
        comparable = listing_value(value)
        if comparable is None and self.anchor_value is not None:
            return not self.missing_first
        if comparable is not None and self.anchor_value is None:
            return self.missing_first
        if comparable == self.anchor_value:
            return (
                identity < self.anchor_id
                if self.descending
                else identity > self.anchor_id
            )
        assert comparable is not None and self.anchor_value is not None
        return (
            comparable < self.anchor_value
            if self.descending
            else comparable > self.anchor_value
        )

    def next_page(self, value: datetime | str | None, identity: UUID) -> str:
        payload = json.dumps([self.context, listing_value(value), str(identity)])
        return _PREFIX + base64.urlsafe_b64encode(payload.encode()).decode().rstrip("=")


def listing_window(
    *,
    page: str | None,
    limit: int | None,
    resource: str,
    sort_by: ListingSortBy,
    reverse_order: bool,
) -> ListingWindow:
    context = f"{resource}:{sort_by.value}:{int(reverse_order)}"
    legacy = resolve_page_window(
        page=None if page and page.startswith(_PREFIX) else page, limit=limit
    )
    anchor_id = None
    anchor_value = None
    if page and page.startswith(_PREFIX):
        try:
            encoded = page[len(_PREFIX) :]
            decoded = json.loads(
                base64.b64decode(
                    encoded + "=" * (-len(encoded) % 4), altchars=b"-_", validate=True
                )
            )
            if (
                not isinstance(decoded, list)
                or len(decoded) != 3
                or decoded[0] != context
            ):
                raise BadRequest("page token is invalid for this listing.")
            anchor_value = decoded[1]
            if anchor_value is not None and not isinstance(anchor_value, str):
                raise BadRequest("page token has an invalid value.")
            anchor_id = UUID(decoded[2])
            if sort_by.value != "label":
                if anchor_value is None:
                    raise BadRequest("page token requires a timestamp.")
                timestamp = datetime.fromisoformat(anchor_value)
                if timestamp.tzinfo is None:
                    raise BadRequest("page token requires a timestamp timezone.")
                anchor_value = listing_value(timestamp)
        except (
            ValueError,
            TypeError,
            AttributeError,
            RecursionError,
            binascii.Error,
        ) as exc:
            raise BadRequest("page token is invalid for this listing.") from exc
    return ListingWindow(
        context,
        legacy.limit,
        legacy.offset,
        sort_by.descending(reverse_order=reverse_order),
        reverse_order,
        anchor_id,
        anchor_value,
    )


def listing_page[T](
    items: Sequence[T],
    window: ListingWindow,
    *,
    value: Callable[[T], datetime | str | None],
    identity: Callable[[T], UUID],
) -> Page[T]:
    chunk = list(items[: window.limit])
    token = (
        window.next_page(value(chunk[-1]), identity(chunk[-1]))
        if len(items) > window.limit
        else None
    )
    return Page(items=chunk, limit=window.limit, next_page=token)


def page_listing_sequence[T](
    items: Sequence[T],
    *,
    page: str | None,
    limit: int | None,
    resource: str,
    sort_by: ListingSortBy,
    reverse_order: bool,
    value: Callable[[T], datetime | str | None],
    identity: Callable[[T], UUID],
) -> Page[T]:
    window = listing_window(
        page=page,
        limit=limit,
        resource=resource,
        sort_by=sort_by,
        reverse_order=reverse_order,
    )
    remaining = [item for item in items if window.follows(value(item), identity(item))]
    return listing_page(
        remaining[window.offset : window.offset + window.limit + 1],
        window,
        value=value,
        identity=identity,
    )
