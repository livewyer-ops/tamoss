from __future__ import annotations

import re
from typing import Any
from uuid import UUID

from pydantic import ValidationError

from tamoss.application.contexts.flows import validate_flow_technical_metadata
from tamoss.auth import Identity
from tamoss.contract.generated import contract_models
from tamoss.contract.serialization import contract_dump
from tamoss.contract.validation import strict_contract_model
from tamoss.domain.model import ProfileRecord, utc_now
from tamoss.domain.pagination import Page
from tamoss.errors import BadRequest, NotFound
from tamoss.ports.repositories import ProfileRepository

_PROFILE_FORMATS = {
    "urn:x-nmos:format:video",
    "urn:x-tam:format:image",
    "urn:x-nmos:format:audio",
    "urn:x-nmos:format:data",
}
_MIME_TYPE_PATTERN = re.compile(
    r"^(application|audio|font|example|image|message|model|multipart|text|video|"
    r"x-(?:[0-9A-Za-z!#$%&'*+.^_`|~-]+))/"
    r"([0-9A-Za-z!#$%&'*+.^_`|~-]+)$"
)
_PROTECTED_PROFILE_FLOW_FIELDS = set(contract_models.FlowCommon.model_fields) | {
    "profile_id"
}


class ProfileConflict(RuntimeError):
    """The requested immutable UUID already describes different metadata."""


class ProfileInUse(RuntimeError):
    """The requested Profile is still referenced by at least one Flow."""


class ProfileUseCases:
    def __init__(self, *, repository: ProfileRepository) -> None:
        self.repository = repository

    def list_profiles(
        self,
        *,
        format: str | None,
        codec: str | None,
        label: str | None,
        page: str | None,
        limit: int | None,
    ) -> Page[ProfileRecord]:
        if format is not None and format not in _PROFILE_FORMATS:
            raise BadRequest("Bad request. Invalid query options.")
        if codec is not None and _MIME_TYPE_PATTERN.fullmatch(codec) is None:
            raise BadRequest("Bad request. Invalid query options.")
        return self.repository.list_profiles_page(
            format=format,
            codec=codec,
            label=label,
            page=page,
            limit=limit,
        )

    def get_profile(self, profile_id: UUID) -> ProfileRecord:
        profile = self.repository.get_profile(profile_id)
        if profile is None:
            raise NotFound("The requested Profile does not exist.")
        return profile

    def create_profile(
        self,
        *,
        profile_id: UUID,
        payload: dict[str, Any],
        identity: Identity,
    ) -> ProfileRecord:
        record = self._validated_record(
            profile_id=profile_id,
            payload=payload,
            identity=identity,
        )
        with self.repository.unit_of_work():
            self.repository.lock_profile(profile_id)
            if self.repository.get_profile(profile_id) is not None:
                raise BadRequest("Bad request. Profile already exists.")
            if not self.repository.create_profile(record):
                raise BadRequest("Bad request. Profile already exists.")
        return record

    def ensure_profile(
        self,
        *,
        profile_id: UUID,
        payload: dict[str, Any],
        identity: Identity,
    ) -> tuple[ProfileRecord, bool]:
        """Create or adopt an exact immutable Profile for operator reconciliation."""
        desired = self._validated_record(
            profile_id=profile_id,
            payload=payload,
            identity=identity,
        )
        with self.repository.unit_of_work():
            self.repository.lock_profile(profile_id)
            existing = self.repository.get_profile(profile_id)
            if existing is not None:
                if not _same_profile_definition(existing, desired):
                    raise ProfileConflict(
                        f"Profile {profile_id} already exists with different metadata"
                    )
                return existing, False
            if not self.repository.create_profile(desired):
                raise ProfileConflict(f"Profile {profile_id} was created concurrently")
        return desired, True

    def delete_profile_if_unused(self, profile_id: UUID) -> bool:
        with self.repository.unit_of_work():
            self.repository.lock_profile(profile_id)
            if self.repository.get_profile(profile_id) is None:
                return False
            references = self.repository.count_flows_by_profile(profile_id)
            if references:
                raise ProfileInUse(
                    f"Profile {profile_id} is referenced by {references} Flow(s)"
                )
            return self.repository.delete_profile(profile_id)

    def _validated_record(
        self,
        *,
        profile_id: UUID,
        payload: dict[str, Any],
        identity: Identity,
    ) -> ProfileRecord:
        raw_flow_metadata = payload.get("flow_metadata")
        if isinstance(raw_flow_metadata, dict) and (
            _PROTECTED_PROFILE_FLOW_FIELDS.intersection(raw_flow_metadata)
        ):
            raise BadRequest("Bad request. Invalid Profile JSON.")
        try:
            if isinstance(raw_flow_metadata, dict):
                validate_flow_technical_metadata(raw_flow_metadata)
            profile = strict_contract_model(
                contract_models.Profile,
                payload,
                non_nullable_fields=contract_models.Profile.model_fields,
            )
        except (TypeError, ValidationError, ValueError) as exc:
            raise BadRequest("Bad request. Invalid Profile JSON.") from exc
        data = contract_dump(profile, exclude_unset=True)
        # A Profile is immutable technical identity. The generated union model
        # marks nested schema defaults as set, even when they were omitted by
        # the client. Retain the strictly validated input so a later read does
        # not describe a different Profile from the one that was created.
        if isinstance(raw_flow_metadata, dict):
            data["flow_metadata"] = raw_flow_metadata
        if UUID(str(data["id"])) != profile_id:
            raise NotFound("The requested Profile ID in the path is invalid.")
        flow_metadata = dict(data["flow_metadata"])
        return ProfileRecord(
            id=profile_id,
            flow_metadata=flow_metadata,
            label=data.get("label"),
            description=data.get("description"),
            created_by=data.get("created_by") or identity.subject,
            created=utc_now(),
            tags=dict(data.get("tags") or {}),
        )


def _same_profile_definition(left: ProfileRecord, right: ProfileRecord) -> bool:
    return (
        left.id == right.id
        and left.flow_metadata == right.flow_metadata
        and left.label == right.label
        and left.description == right.description
        and left.tags == right.tags
    )


def profile_payload(profile: ProfileRecord) -> dict[str, Any]:
    record_payload: dict[str, Any] = {
        "id": str(profile.id),
        "flow_metadata": profile.flow_metadata,
        "label": profile.label,
        "description": profile.description,
        "created_by": profile.created_by,
        "created": profile.created.isoformat(),
        "tags": profile.tags,
    }
    payload = contract_dump(
        contract_models.Profile.model_validate(
            {key: value for key, value in record_payload.items() if value is not None}
        ),
        exclude_unset=True,
    )
    payload["flow_metadata"] = profile.flow_metadata
    return payload
