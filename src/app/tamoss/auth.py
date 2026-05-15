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
from jwt import InvalidTokenError, PyJWKClientError

from tamoss.errors import Unauthorized
from tamoss.settings import Settings

logger = logging.getLogger(__name__)
_JWKS_CLIENTS: dict[str, jwt.PyJWKClient] = {}
_JWKS_LOCK = RLock()


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
        settings = Settings()
    return authenticate_request(request, settings)


def unauthorized_headers() -> dict[str, str]:
    return {"WWW-Authenticate": "Bearer, Basic"}


def _authenticate_with_configured_methods(
    request: Request, settings: Settings
) -> Identity | None:
    if settings.trust_forward_auth_headers:
        identity = _forward_auth_identity(request)
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


def _forward_auth_identity(request: Request) -> Identity | None:
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
    if not settings.basic_auth_password:
        return None
    try:
        decoded = base64.b64decode(credentials, validate=True).decode("utf-8")
    except (binascii.Error, UnicodeDecodeError):
        return None

    username, separator, password = decoded.partition(":")
    if not separator:
        return None

    if _constant_time_equal(username, settings.basic_auth_username) and (
        _constant_time_equal(password, settings.basic_auth_password)
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

    required_scopes = set(settings.oauth2_required_scopes)
    token_scopes = _token_scopes(claims)
    if required_scopes and not required_scopes.issubset(token_scopes):
        raise InvalidTokenError("JWT is missing required scopes")
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


def _configured_token_matches(candidate: str, settings: Settings) -> bool:
    return bool(settings.api_token) and _constant_time_equal(
        candidate, settings.api_token
    )


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
