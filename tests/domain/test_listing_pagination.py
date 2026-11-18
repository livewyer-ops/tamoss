from __future__ import annotations

import base64
import json
from uuid import uuid4

import pytest
from tamoss.domain.listing_pagination import listing_window
from tamoss.domain.listings import FlowSortBy
from tamoss.errors import BadRequest


def test_old_label_keysets_are_readable_but_new_tokens_use_offsets() -> None:
    options = {
        "resource": "flows",
        "sort_by": FlowSortBy.LABEL,
        "reverse_order": False,
        "limit": 2,
    }
    identity, label = uuid4(), "A" * 6000
    payload = json.dumps(["flows:label:0", label, str(identity)])
    old_token = "k1." + base64.urlsafe_b64encode(payload.encode()).decode().rstrip("=")
    cursor = listing_window(page=old_token, **options)
    assert cursor.anchor_id == identity
    assert cursor.anchor_value == label
    first = listing_window(page=None, **options)
    token = first.next_page(label, identity)
    assert token == "2"
    second = listing_window(page=token, **options)
    assert second.next_page(label, identity) == "4"


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
