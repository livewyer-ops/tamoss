from __future__ import annotations

import base64
from uuid import uuid4

import pytest
from tamoss.domain.listing_pagination import listing_window
from tamoss.domain.listings import FlowSortBy
from tamoss.errors import BadRequest


def test_issued_cursor_round_trips_labels_without_an_artificial_length_limit() -> None:
    options = {
        "resource": "flows",
        "sort_by": FlowSortBy.LABEL,
        "reverse_order": False,
        "limit": 2,
    }
    first = listing_window(page=None, **options)
    identity, label = uuid4(), "A" * 6000
    cursor = listing_window(page=first.next_page(label, identity), **options)
    assert cursor.anchor_id == identity
    assert cursor.anchor_value == label


def test_deeply_nested_invalid_cursor_is_a_bad_request() -> None:
    malformed = base64.urlsafe_b64encode(("[" * 2000 + "]" * 2000).encode()).decode()
    with pytest.raises(BadRequest):
        listing_window(
            page="k1." + malformed,
            limit=2,
            resource="flows",
            sort_by=FlowSortBy.LABEL,
            reverse_order=False,
        )
