from __future__ import annotations

from uuid import uuid4

import pytest
from tamoss.application.contexts.profiles import (
    ProfileConflict,
    ProfileInUse,
    ProfileUseCases,
)
from tamoss.auth import Identity
from tamoss.domain.model import FlowRecord, StorageBackend

from tests.support.memory_repository import FakeTamossRepository


def _profile_payload(profile_id: object, *, label: str = "HD AVC") -> dict[str, object]:
    return {
        "id": str(profile_id),
        "label": label,
        "tags": {"tier": "mezzanine"},
        "flow_metadata": {
            "format": "urn:x-nmos:format:video",
            "codec": "video/h264",
            "container": "video/mp4",
            "essence_parameters": {
                "frame_rate": {"numerator": 25, "denominator": 1},
                "frame_width": 1920,
                "frame_height": 1080,
            },
        },
    }


def _profiles() -> tuple[FakeTamossRepository, ProfileUseCases]:
    repository = FakeTamossRepository(
        StorageBackend(
            id=uuid4(),
            label="test",
            provider="test",
            region="test",
            store_product="test",
        )
    )
    return repository, ProfileUseCases(repository=repository.profile_repository)


def test_operator_profile_ensure_creates_adopts_and_rejects_conflicts() -> None:
    repository, profiles = _profiles()
    profile_id = uuid4()
    identity = Identity(subject="tamoss-operator:media/hd-avc", method="operator")

    created, was_created = profiles.ensure_profile(
        profile_id=profile_id,
        payload=_profile_payload(profile_id),
        identity=identity,
    )
    adopted, was_adopted = profiles.ensure_profile(
        profile_id=profile_id,
        payload=_profile_payload(profile_id),
        identity=identity,
    )

    assert was_created is True
    assert was_adopted is False
    assert adopted == created
    assert repository.get_profile(profile_id) == created
    with pytest.raises(ProfileConflict, match="different metadata"):
        profiles.ensure_profile(
            profile_id=profile_id,
            payload=_profile_payload(profile_id, label="Different"),
            identity=identity,
        )


def test_operator_profile_delete_is_absent_safe_and_blocks_references() -> None:
    repository, profiles = _profiles()
    profile_id = uuid4()
    unused_profile_id = uuid4()
    identity = Identity(subject="tamoss-operator:media/hd-avc", method="operator")
    profiles.ensure_profile(
        profile_id=profile_id,
        payload=_profile_payload(profile_id),
        identity=identity,
    )
    profiles.ensure_profile(
        profile_id=unused_profile_id,
        payload=_profile_payload(unused_profile_id, label="Unused"),
        identity=identity,
    )
    repository.save_flow(
        FlowRecord(
            id=uuid4(),
            source_id=uuid4(),
            profile_id=profile_id,
            format="urn:x-nmos:format:video",
            container="video/mp4",
            data={"profile_id": str(profile_id)},
        )
    )

    with pytest.raises(ProfileInUse, match="1 Flow"):
        profiles.delete_profile_if_unused(profile_id)
    assert repository.get_profile(profile_id) is not None

    assert profiles.delete_profile_if_unused(unused_profile_id) is True
    assert profiles.delete_profile_if_unused(unused_profile_id) is False
