from __future__ import annotations

from uuid import uuid4

from tamoss.contract.generated import contract_models
from tamoss.contract.serialization import contract_dump

from tests.tams.support import (
    segment_payload,
    storage_allocation_payload,
    video_flow_payload,
    webhook_payload,
)


def test_generated_storage_backend_identity_fields_are_required() -> None:
    fields = contract_models.StorageBackendsListItem.model_fields

    for name in ("store_type", "provider", "store_product"):
        assert fields[name].is_required()


def test_generated_contract_models_validate_representative_bbc_payloads() -> None:
    flow_id = uuid4()
    source_id = uuid4()

    contract_models.Flow.model_validate(video_flow_payload(flow_id, source_id))
    contract_models.Source.model_validate(
        {
            "id": str(source_id),
            "format": "urn:x-nmos:format:video",
            "tags": {},
        }
    )
    contract_models.FlowSegmentPost.model_validate(
        segment_payload("bbc/example/segment.ts")
    )
    contract_models.FlowStoragePost.model_validate(
        storage_allocation_payload(["bbc/example/segment.ts"])
    )
    contract_models.WebhookPost.model_validate(webhook_payload())


def test_contract_dump_uses_public_json_model_options() -> None:
    segment = contract_models.FlowSegment.model_validate(
        {
            "object_id": "bbc/example/segment.ts",
            "timerange": "[0:0_1:0)",
            "object_timerange": None,
            "get_urls": [],
        }
    )

    payload = contract_dump(segment)

    assert payload == {
        "object_id": "bbc/example/segment.ts",
        "timerange": "[0:0_1:0)",
        "get_urls": [],
    }
