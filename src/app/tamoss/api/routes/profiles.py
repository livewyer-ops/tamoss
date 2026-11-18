from __future__ import annotations

from typing import Annotated, Any

from fastapi import APIRouter, Body, Depends, Path, Query, Request, Response, status

from tamoss.api.dependencies import (
    ResourceUUID,
    get_profile_use_cases,
    require_json_body,
)
from tamoss.api.presenters import head_response, with_page_headers
from tamoss.api.query_params import validate_query_params
from tamoss.application.contexts.profiles import ProfileUseCases, profile_payload
from tamoss.auth import identify_request

router = APIRouter(tags=["Profiles"])


@router.get("/service/profiles")
@router.head("/service/profiles")
def list_profiles(
    request: Request,
    response: Response,
    format: str | None = None,
    codec: str | None = None,
    label: str | None = None,
    page: str | None = None,
    limit: int | None = Query(default=None, gt=0),
    profiles: ProfileUseCases = Depends(get_profile_use_cases),
) -> Any:
    validate_query_params(request, {"format", "codec", "label", "page", "limit"})
    result = profiles.list_profiles(
        format=format,
        codec=codec,
        label=label,
        page=page,
        limit=limit,
    )
    with_page_headers(response, request, result)
    if head := head_response(request, response):
        return head
    return [profile_payload(profile) for profile in result.items]


@router.get("/service/profiles/{profileId}")
@router.head("/service/profiles/{profileId}")
def get_profile(
    profile_id: Annotated[ResourceUUID, Path(alias="profileId")],
    request: Request,
    profiles: ProfileUseCases = Depends(get_profile_use_cases),
) -> Any:
    validate_query_params(request, set())
    profile = profiles.get_profile(profile_id)
    if head := head_response(request):
        return head
    return profile_payload(profile)


@router.post(
    "/service/profiles/{profileId}",
    status_code=status.HTTP_201_CREATED,
    dependencies=[Depends(require_json_body)],
)
def create_profile(
    profile_id: Annotated[ResourceUUID, Path(alias="profileId")],
    request: Request,
    profile: dict[str, Any] = Body(...),
    profiles: ProfileUseCases = Depends(get_profile_use_cases),
) -> Any:
    created = profiles.create_profile(
        profile_id=profile_id,
        payload=profile,
        identity=identify_request(request),
    )
    return profile_payload(created)
