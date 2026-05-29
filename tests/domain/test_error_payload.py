from __future__ import annotations

import pytest
from tamoss.domain.model import DomainErrorPayload


def test_domain_error_payload_round_trips_json_shape() -> None:
    payload = DomainErrorPayload(
        type="HTTPError",
        summary="HTTP 503: Service Unavailable",
        time="2026-01-02T03:04:05+00:00",
        incident_id="incident-1",
        traceback=("line 1", "line 2"),
    )

    json_payload = payload.to_json_dict()

    assert json_payload == {
        "type": "HTTPError",
        "summary": "HTTP 503: Service Unavailable",
        "time": "2026-01-02T03:04:05+00:00",
        "traceback": ["line 1", "line 2"],
        "incident_id": "incident-1",
    }
    assert DomainErrorPayload.from_json_dict(json_payload) == payload


def test_domain_error_payload_rejects_legacy_message_payload() -> None:
    payload = {
        "message": "legacy message",
        "time": "not-a-timestamp",
        "traceback": "single traceback line",
    }

    with pytest.raises(
        ValueError, match=r"Error payload requires a non-empty string type\."
    ):
        DomainErrorPayload.from_json_dict(payload)
