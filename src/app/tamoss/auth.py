from __future__ import annotations

import base64
import binascii
import logging
import secrets
from dataclasses import dataclass, field
from threading import RLock
from typing import Any

import jwt
from fastapi import Request
from fastapi.routing import APIRoute, iter_route_contexts
from jwt import InvalidTokenError, PyJWKClientError
from starlette.routing import Match

from tamoss.errors import Forbidden, Unauthorized
from tamoss.settings import Settings, get_settings

logger = logging.getLogger(__name__)
_JWKS_CLIENTS: dict[str, jwt.PyJWKClient] = {}
_JWKS_LOCK = RLock()

_ADMIN_SCOPE = "admin"
_READ_SCOPE = "read"
_WRITE_SCOPE = "write"
_DELETE_SCOPE = "delete"
_ANY_API_SCOPE = frozenset({_ADMIN_SCOPE, _READ_SCOPE, _WRITE_SCOPE, _DELETE_SCOPE})
_READ_API_SCOPE = frozenset({_ADMIN_SCOPE, _READ_SCOPE})
_WRITE_API_SCOPE = frozenset({_ADMIN_SCOPE, _WRITE_SCOPE})
_DELETE_API_SCOPE = frozenset({_ADMIN_SCOPE, _DELETE_SCOPE})
_ADMIN_API_SCOPE = frozenset({_ADMIN_SCOPE})

OAUTH2_ROUTE_SCOPE_GROUPS: dict[tuple[str, str], frozenset[str]] = {
    ("GET", "/"): _ANY_API_SCOPE,
    ("HEAD", "/"): _ANY_API_SCOPE,
    ("GET", "/service"): _ANY_API_SCOPE,
    ("HEAD", "/service"): _ANY_API_SCOPE,
    ("POST", "/service"): _ADMIN_API_SCOPE,
    ("GET", "/service/storage-backends"): _ANY_API_SCOPE,
    ("HEAD", "/service/storage-backends"): _ANY_API_SCOPE,
    ("GET", "/service/webhooks"): _READ_API_SCOPE,
    ("HEAD", "/service/webhooks"): _READ_API_SCOPE,
    ("POST", "/service/webhooks"): _WRITE_API_SCOPE,
    ("GET", "/service/webhooks/{webhookId}"): _READ_API_SCOPE,
    ("HEAD", "/service/webhooks/{webhookId}"): _READ_API_SCOPE,
    ("PUT", "/service/webhooks/{webhookId}"): _WRITE_API_SCOPE,
    ("DELETE", "/service/webhooks/{webhookId}"): _WRITE_API_SCOPE,
    ("GET", "/flow-delete-requests"): _DELETE_API_SCOPE,
    ("HEAD", "/flow-delete-requests"): _DELETE_API_SCOPE,
    ("GET", "/flow-delete-requests/{request_id}"): _DELETE_API_SCOPE,
    ("HEAD", "/flow-delete-requests/{request_id}"): _DELETE_API_SCOPE,
    ("GET", "/sources"): _READ_API_SCOPE,
    ("HEAD", "/sources"): _READ_API_SCOPE,
    ("GET", "/sources/{sourceId}"): _READ_API_SCOPE,
    ("HEAD", "/sources/{sourceId}"): _READ_API_SCOPE,
    ("GET", "/sources/{sourceId}/label"): _READ_API_SCOPE,
    ("HEAD", "/sources/{sourceId}/label"): _READ_API_SCOPE,
    ("PUT", "/sources/{sourceId}/label"): _WRITE_API_SCOPE,
    ("DELETE", "/sources/{sourceId}/label"): _WRITE_API_SCOPE,
    ("GET", "/sources/{sourceId}/description"): _READ_API_SCOPE,
    ("HEAD", "/sources/{sourceId}/description"): _READ_API_SCOPE,
    ("PUT", "/sources/{sourceId}/description"): _WRITE_API_SCOPE,
    ("DELETE", "/sources/{sourceId}/description"): _WRITE_API_SCOPE,
    ("GET", "/sources/{sourceId}/tags"): _READ_API_SCOPE,
    ("HEAD", "/sources/{sourceId}/tags"): _READ_API_SCOPE,
    ("GET", "/sources/{sourceId}/tags/{name:path}"): _READ_API_SCOPE,
    ("HEAD", "/sources/{sourceId}/tags/{name:path}"): _READ_API_SCOPE,
    ("PUT", "/sources/{sourceId}/tags/{name:path}"): _WRITE_API_SCOPE,
    ("DELETE", "/sources/{sourceId}/tags/{name:path}"): _WRITE_API_SCOPE,
    ("GET", "/flows"): _READ_API_SCOPE,
    ("HEAD", "/flows"): _READ_API_SCOPE,
    ("GET", "/flows/{flowId}"): _READ_API_SCOPE,
    ("HEAD", "/flows/{flowId}"): _READ_API_SCOPE,
    ("PUT", "/flows/{flowId}"): _WRITE_API_SCOPE,
    ("DELETE", "/flows/{flowId}"): _DELETE_API_SCOPE,
    ("GET", "/flows/{flowId}/flow_collection"): _READ_API_SCOPE,
    ("HEAD", "/flows/{flowId}/flow_collection"): _READ_API_SCOPE,
    ("PUT", "/flows/{flowId}/flow_collection"): _WRITE_API_SCOPE,
    ("DELETE", "/flows/{flowId}/flow_collection"): _WRITE_API_SCOPE,
    ("GET", "/flows/{flowId}/label"): _READ_API_SCOPE,
    ("HEAD", "/flows/{flowId}/label"): _READ_API_SCOPE,
    ("PUT", "/flows/{flowId}/label"): _WRITE_API_SCOPE,
    ("DELETE", "/flows/{flowId}/label"): _WRITE_API_SCOPE,
    ("GET", "/flows/{flowId}/description"): _READ_API_SCOPE,
    ("HEAD", "/flows/{flowId}/description"): _READ_API_SCOPE,
    ("PUT", "/flows/{flowId}/description"): _WRITE_API_SCOPE,
    ("DELETE", "/flows/{flowId}/description"): _WRITE_API_SCOPE,
    ("GET", "/flows/{flowId}/avg_bit_rate"): _READ_API_SCOPE,
    ("HEAD", "/flows/{flowId}/avg_bit_rate"): _READ_API_SCOPE,
    ("PUT", "/flows/{flowId}/avg_bit_rate"): _WRITE_API_SCOPE,
    ("DELETE", "/flows/{flowId}/avg_bit_rate"): _WRITE_API_SCOPE,
    ("GET", "/flows/{flowId}/max_bit_rate"): _READ_API_SCOPE,
    ("HEAD", "/flows/{flowId}/max_bit_rate"): _READ_API_SCOPE,
    ("PUT", "/flows/{flowId}/max_bit_rate"): _WRITE_API_SCOPE,
    ("DELETE", "/flows/{flowId}/max_bit_rate"): _WRITE_API_SCOPE,
    ("GET", "/flows/{flowId}/read_only"): _READ_API_SCOPE,
    ("HEAD", "/flows/{flowId}/read_only"): _READ_API_SCOPE,
    ("PUT", "/flows/{flowId}/read_only"): _WRITE_API_SCOPE,
    ("GET", "/flows/{flowId}/tags"): _READ_API_SCOPE,
    ("HEAD", "/flows/{flowId}/tags"): _READ_API_SCOPE,
    ("GET", "/flows/{flowId}/tags/{name:path}"): _READ_API_SCOPE,
    ("HEAD", "/flows/{flowId}/tags/{name:path}"): _READ_API_SCOPE,
    ("PUT", "/flows/{flowId}/tags/{name:path}"): _WRITE_API_SCOPE,
    ("DELETE", "/flows/{flowId}/tags/{name:path}"): _WRITE_API_SCOPE,
    ("POST", "/flows/{flowId}/storage"): _WRITE_API_SCOPE,
    ("GET", "/flows/{flowId}/segments"): _READ_API_SCOPE,
    ("HEAD", "/flows/{flowId}/segments"): _READ_API_SCOPE,
    ("POST", "/flows/{flowId}/segments"): _WRITE_API_SCOPE,
    ("DELETE", "/flows/{flowId}/segments"): _DELETE_API_SCOPE,
    ("GET", "/objects/{objectId:path}"): _READ_API_SCOPE,
    ("HEAD", "/objects/{objectId:path}"): _READ_API_SCOPE,
    ("POST", "/objects/{objectId:path}/instances"): _WRITE_API_SCOPE,
    ("DELETE", "/objects/{objectId:path}/instances"): _WRITE_API_SCOPE,
}


@dataclass(frozen=True)
class Identity:
    subject: str
    method: str
    scopes: frozenset[str] = field(default_factory=frozenset)


def authenticate_request(request: Request, settings: Settings) -> Identity:
    identity = _authenticate_with_configured_methods(request, settings)
    if identity is not None:
        _mark_authenticated(request, identity)
        return identity

    if not settings.auth_required:
        identity = Identity(subject="anonymous", method="none")
        request.state.tamoss_identity = identity
        request.state.authenticated = False
        request.state.auth_subject = identity.subject
        request.state.auth_method = identity.method
        request.state.auth_scopes = []
        return identity

    raise Unauthorized(
        "Authentication required. Provide valid Bearer, Basic, URL token, "
        "or trusted forward-auth credentials."
    )


def identify_request(request: Request) -> Identity:
    identity = getattr(request.state, "tamoss_identity", None)
    if isinstance(identity, Identity):
        return identity

    settings = getattr(request.app.state, "tamoss_settings", None)
    if not isinstance(settings, Settings):
        settings = get_settings()
    return authenticate_request(request, settings)


def authorize_request(request: Request, identity: Identity, settings: Settings) -> None:
    if identity.method != "bearer-oauth2":
        return
    if not identity.scopes:
        if settings.oauth2_allow_unscoped_full_access:
            return
        raise Forbidden(
            "Forbidden. OAuth2 token is missing a required TAMOSS API scope."
        )

    route_path = _matched_api_route_path(request)
    if route_path is None:
        return

    route_key = (request.method.upper(), route_path)
    scope_groups = OAUTH2_ROUTE_SCOPE_GROUPS.get(route_key)
    if scope_groups is None:
        raise Forbidden(
            "Forbidden. OAuth2 route scope is not configured for this API route."
        )

    required_scopes = _oauth2_scope_names(settings, scope_groups)
    if identity.scopes.isdisjoint(required_scopes):
        raise Forbidden(
            "Forbidden. OAuth2 token is missing a required TAMOSS API scope."
        )


def unauthorized_headers(settings: Settings) -> dict[str, str]:
    methods = ["Bearer"]
    if settings.basic_auth_password or settings.basic_auth_password_file:
        methods.append("Basic")
    return {"WWW-Authenticate": ", ".join(methods)}


def _authenticate_with_configured_methods(
    request: Request, settings: Settings
) -> Identity | None:
    if settings.trust_forward_auth_headers:
        identity = _forward_auth_identity(request, settings)
        if identity is not None:
            return identity

    authorization = request.headers.get("authorization", "")
    scheme, _, credentials = authorization.partition(" ")
    if scheme.lower() == "bearer" and credentials:
        identity = _bearer_identity(credentials, settings)
        if identity is not None:
            return identity
    if scheme.lower() == "basic" and credentials:
        identity = _basic_identity(credentials, settings)
        if identity is not None:
            return identity

    access_token = request.query_params.get("access_token")
    if access_token and _configured_token_matches(access_token, settings):
        return Identity(subject="token-user", method="url-token")

    return None


def _forward_auth_identity(request: Request, settings: Settings) -> Identity | None:
    expected_proof = settings.forward_auth_shared_secret_value()
    supplied_proof = request.headers.get("x-tamoss-forward-auth-secret", "")
    if not expected_proof or not _constant_time_equal(supplied_proof, expected_proof):
        return None

    for header_name in ("remote-user", "x-authentik-username"):
        subject = request.headers.get(header_name)
        if subject:
            return Identity(subject=subject, method="forward-auth")
    return None


def _bearer_identity(token: str, settings: Settings) -> Identity | None:
    if _configured_token_matches(token, settings):
        return Identity(subject="token-user", method="bearer-static")
    if not settings.oauth2_enabled:
        return None

    try:
        claims = _validate_oauth2_bearer_token(token, settings)
    except (InvalidTokenError, PyJWKClientError, ValueError):
        logger.warning("OAuth2 bearer token validation failed", exc_info=True)
        return None
    return Identity(
        subject=_claim_subject(claims),
        method="bearer-oauth2",
        scopes=frozenset(_token_scopes(claims)),
    )


def _basic_identity(credentials: str, settings: Settings) -> Identity | None:
    expected_password = settings.basic_auth_password_value()
    if not expected_password:
        return None
    try:
        decoded = base64.b64decode(credentials, validate=True).decode("utf-8")
    except (binascii.Error, UnicodeDecodeError):
        return None

    username, separator, password = decoded.partition(":")
    if not separator:
        return None

    if _constant_time_equal(username, settings.basic_auth_username) and (
        _constant_time_equal(password, expected_password)
    ):
        return Identity(subject=username, method="basic")
    return None


def _validate_oauth2_bearer_token(token: str, settings: Settings) -> dict[str, Any]:
    jwks_uri = settings.oauth2_jwks_uri
    if not jwks_uri:
        raise InvalidTokenError("TAMOSS_OAUTH2_JWKS_URI is required")

    with _JWKS_LOCK:
        signing_key = _jwks_client(jwks_uri, settings).get_signing_key_from_jwt(token)
    claims = jwt.decode(
        token,
        signing_key.key,
        algorithms=settings.oauth2_algorithms or ["RS256"],
        issuer=settings.oauth2_issuer or None,
        audience=settings.oauth2_audience or None,
        options={
            "require": ["exp"],
            "verify_aud": bool(settings.oauth2_audience),
        },
    )
    if not isinstance(claims, dict):
        raise InvalidTokenError("JWT claims must be an object")

    return claims


def warm_oauth2_jwks(settings: Settings) -> None:
    jwks_uri = settings.oauth2_jwks_uri
    if not settings.oauth2_enabled or not jwks_uri:
        return
    with _JWKS_LOCK:
        _jwks_client(jwks_uri, settings).get_jwk_set()


def _jwks_client(jwks_uri: str, settings: Settings) -> jwt.PyJWKClient:
    cache_key = _jwks_cache_key(jwks_uri, settings)
    client = _JWKS_CLIENTS.get(cache_key)
    if client is None:
        client = jwt.PyJWKClient(
            jwks_uri,
            cache_keys=True,
            cache_jwk_set=True,
            lifespan=settings.oauth2_jwks_cache_seconds,
            timeout=settings.oauth2_jwks_timeout_seconds,
        )
        _JWKS_CLIENTS[cache_key] = client
    return client


def _jwks_cache_key(jwks_uri: str, settings: Settings) -> str:
    return (
        f"{jwks_uri}|{settings.oauth2_jwks_cache_seconds}|"
        f"{settings.oauth2_jwks_timeout_seconds}"
    )


def _token_scopes(claims: dict[str, Any]) -> set[str]:
    raw_scope = claims.get("scope", claims.get("scp", ""))
    if isinstance(raw_scope, str):
        return {scope for scope in raw_scope.split() if scope}
    if isinstance(raw_scope, list):
        return {scope for scope in raw_scope if isinstance(scope, str) and scope}
    return set()


def _claim_subject(claims: dict[str, Any]) -> str:
    for claim_name in (
        "username",
        "preferred_username",
        "email",
        "sub",
        "client_id",
        "azp",
        "iss",
    ):
        value = claims.get(claim_name)
        if isinstance(value, str) and value:
            return value
    return "oauth2-client"


def _matched_api_route_path(request: Request) -> str | None:
    for route_context in iter_route_contexts(request.app.routes):
        route = route_context.route
        if not isinstance(route, APIRoute):
            continue
        match, _child_scope = route_context.matches(request.scope)
        if match == Match.FULL:
            return route_context.path
    return None


def _oauth2_scope_names(
    settings: Settings,
    scope_groups: frozenset[str],
) -> frozenset[str]:
    scope_names = {
        _ADMIN_SCOPE: settings.oauth2_admin_scope,
        _READ_SCOPE: settings.oauth2_read_scope,
        _WRITE_SCOPE: settings.oauth2_write_scope,
        _DELETE_SCOPE: settings.oauth2_delete_scope,
    }
    return frozenset(scope_names[scope_group] for scope_group in scope_groups)


def _configured_token_matches(candidate: str, settings: Settings) -> bool:
    expected = settings.api_token_value()
    return bool(expected) and _constant_time_equal(candidate, expected)


def _constant_time_equal(candidate: str, expected: str) -> bool:
    return secrets.compare_digest(
        candidate.encode("utf-8"),
        expected.encode("utf-8"),
    )


def _mark_authenticated(request: Request, identity: Identity) -> None:
    request.state.tamoss_identity = identity
    request.state.authenticated = True
    request.state.auth_subject = identity.subject
    request.state.auth_method = identity.method
    request.state.auth_scopes = sorted(identity.scopes)
