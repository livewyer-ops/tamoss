# 0006: External Browser Identity

## Status

- This is a proposed 8.2 design for discussion, not an implemented contract.
- External OAuth/OIDC remains supported for direct API bearer authentication.
- The UI and Console remain deliberately unavailable for required external
  authentication until this session boundary, or an equivalent reviewed
  design, is implemented and passes the release gates below.
- Reintroducing a shared API token, placing a client secret or access token in
  browser storage, or trusting browser-supplied identity headers is rejected.

## Context

Managed Authentik installations have a trusted reverse-proxy session. The UI
uses an internal Authentik `auth_request`; nginx strips browser headers and
adds separate proof-backed identity assertions for the TAMS API and Console.

An external OAuth/OIDC provider currently authenticates direct API clients,
but it does not provide the UI with a same-origin session or give Console a
trusted human identity. The old pattern of putting application credentials in
the UI would authenticate the application, not the user, and would grant every
browser the same authority.

The proposed design follows the OAuth security BCP's authorization-code,
exact-redirect, CSRF, PKCE, and issuer-binding guidance. It uses OpenID Connect
for identity and validates the ID Token rather than treating an access token as
an unverified user profile.

## Proposed Boundary

Console becomes the external-provider session broker. It remains a separate
Deployment and does not become a general TAMS API proxy.

1. nginx sends an unauthenticated browser to a same-origin login route.
2. Console starts Authorization Code with PKCE S256, a transaction-specific
   nonce, and encrypted state bound to the browser.
3. The provider redirects only to the exact configured public UI callback.
4. Console exchanges the code over its back channel, validates issuer,
   audience, signature, algorithm, expiry, nonce, and PKCE, and extracts only
   the configured subject, username, and group claims.
5. The flow does not request `offline_access`. Provider tokens are discarded
   after validation and are never returned to JavaScript, nginx, the TAMS API,
   browser storage, logs, or audit records.
6. Console sets a short-lived encrypted and authenticated session cookie.
7. nginx uses an internal Console session-check subrequest and receives only
   bounded subject, username, and group headers.
8. nginx strips browser headers and adds the existing distinct API or Console
   proof before proxying. Console never receives the API proof.

Direct Bearer, Basic, URL-token, and machine-client OAuth behaviour remains
independent. The browser session only enables the same constrained forwarded
identity path already used by managed Authentik: browser TAMS API access stays
`GET`/`HEAD`, and Console commands stay typed, capability-gated, and audited.

## Session Contract

Add same-origin routes with one versioned contract:

| Route | Purpose |
| --- | --- |
| `GET /auth/login` | Start one external OIDC transaction |
| `GET /auth/callback` | Validate and exchange the provider response |
| `GET /ui-api/v1/auth/check` | Internal nginx session subrequest |
| `POST /ui-api/v1/auth/logout` | Clear the local session and return a bounded provider logout target when supported |

The login route accepts only a relative return path from an allowlist. It does
not accept an arbitrary redirect URI or provider URL.

Use two encrypted `__Host-` cookies:

- `__Host-tamoss-oidc-state`, a single-use transaction cookie with a maximum
  age of ten minutes, containing the state binding, PKCE verifier, nonce,
  issuer identifier, and relative return path; and
- `__Host-tamoss-session`, a session cookie with a maximum age of fifteen
  minutes and never beyond the validated ID Token expiry, containing a version,
  issuer, stable subject, optional username, bounded groups, issued-at time,
  and expiry.

Both cookies are `Secure`, `HttpOnly`, host-only, `Path=/`, and
`SameSite=Lax`, allowing the top-level authorisation callback without enabling
cross-site subrequests. Logout requires the same exact Origin-to-forwarded-host
check as existing Console commands. Cookie plaintext and provider tokens are
never logged. The session payload is bounded below the browser cookie-size
limit; oversized claims fail authentication rather than truncating groups and
changing authority.

Use AES-256-GCM from the Go standard library with versioned associated data
bound to the cookie name, public UI origin, namespace, and Tamoss UID. The
operator creates a dedicated session-key Secret and mounts it only into
Console. All Console replicas share that key, so the design requires no
per-browser Kubernetes object, database row, or sticky session. Automatic key
rotation is deferred until a current-plus-previous-key rollout is implemented;
an intentional key replacement logs users out.

## Configuration Shape

Move browser role mapping to a provider-neutral auth block while preserving
the existing managed-Authentik field during a deprecation window:

```yaml
spec:
  publicEndpoint:
    uiURL: https://app.example.com
  auth:
    providedBy: external
    required: true
    browser:
      groupClaim: groups
      usernameClaim: preferred_username
      groupBindings:
        - groupName: tamoss-viewers
          permissions: [viewer]
        - groupName: tamoss-operators
          permissions: [operator]
    external:
      oauth2:
        enabled: true
        issuer: https://identity.example.com/application/o/tamoss/
        audience: tamoss
        clientCredentialsSecret:
          existingSecret: tamoss-oauth2-creds
```

`publicEndpoint.uiURL` is the sole callback-origin source. It is already the
explicit public-origin escape hatch for non-standard ports and must validate as
an absolute HTTP(S) origin with no userinfo, path, query, or fragment. The
provider registration must exactly match `${uiURL}/auth/callback`.

The referenced client Secret keeps the existing
`TAMOSS_OAUTH2_CLIENT_ID`/`TAMOSS_OAUTH2_CLIENT_SECRET` keys. API receives the
current OAuth verifier configuration; Console receives only the client values
and discovery configuration needed for the browser flow. UI receives neither.

External discovery, token, and JWKS calls add a Console egress requirement.
Multi-server must use an explicitly destination-scoped rule or a constrained
in-cluster egress proxy. The operator must not restore unrestricted HTTPS
egress to make external identity work.

## Failure Behaviour

- Missing public origin, client Secret, discovery metadata, PKCE S256 support,
  group bindings, or destination-scoped egress keeps browser auth unavailable
  and surfaces a Tamoss condition before UI readiness is reported.
- Provider timeout or key refresh failure returns a bounded `503`; it does not
  fall back to anonymous or application credentials.
- Missing or invalid subject is `401`. A valid identity with missing,
  malformed, or unmapped groups is `403`, matching the current API and Console
  contract.
- Callback state, issuer, nonce, code, and token errors use stable reason codes
  without echoing query parameters, codes, tokens, claims, or provider bodies.
- Console restart does not invalidate sessions while the session key remains;
  key replacement deliberately does.

## Implementation Stages

1. Add provider-neutral browser auth/group configuration, validation, status,
   and generated CRD tests without enabling a new runtime mode.
2. Add a narrow OIDC client boundary and deterministic tests using an in-process
   fake provider. Prefer `golang.org/x/oauth2` plus the smallest maintained OIDC
   verifier that implements discovery and ID Token validation; record its
   dependency and vulnerability impact.
3. Implement transaction/session cookies and login, callback, check, and logout
   handlers with fixed clocks and deterministic cryptographic test vectors.
4. Add the Console-only session key and external client Secret mounts. Prove UI
   receives neither and API/Console proof separation is unchanged.
5. Add nginx `external-oidc` mode. Its auth subrequest must preserve the current
   header-stripping, method restrictions, origin handling, and distinct proofs.
6. Add destination-scoped provider egress and an explicit readiness/status gate.
7. Validate managed Authentik and anonymous development modes for regression,
   then run real external providers through Ingress and HTTPRoute deployments.

## 8.2 Release Gates

- Exact redirect matching; state swap/replay, PKCE downgrade, nonce replay,
  issuer mix-up, wrong audience, wrong algorithm, expired token, malformed
  claims, oversized groups, and open-redirect tests all fail closed.
- Cookies pass Secure, HttpOnly, SameSite, scope, tamper, expiry, cross-instance,
  restart, replica, and key-replacement tests. No OAuth material appears in the
  DOM, browser storage, URL after callback, logs, traces, or audit records.
- Browser-spoofed `Authorization`, Cookie, forwarding proof, subject, username,
  groups, `Remote-User`, and `X-authentik-*` headers cannot cross nginx.
- Viewer, operator, and ingest-runner permissions match managed Authentik for
  the same bound groups. Unmapped identities cannot read runtime or catalog
  data through the browser proxy.
- Provider outage, discovery/JWKS rotation, slow responses, Console rollout,
  and multiple Console replicas have bounded and observable behaviour.
- Enforcing-CNI tests deny arbitrary provider/HTTPS egress while the configured
  discovery, authorization, token, JWKS, TAMS API, Console, DNS, and Kubernetes
  paths remain available.
- Deployed tests pass with at least two non-Authentik OIDC providers and cover
  both Ingress and HTTPRoute. Managed Authentik remains green throughout.

## References

- [RFC 9700: OAuth 2.0 Security Best Current Practice](https://www.rfc-editor.org/rfc/rfc9700.html)
- [RFC 7636: Proof Key for Code Exchange](https://www.rfc-editor.org/rfc/rfc7636.html)
- [OpenID Connect Core 1.0](https://openid.net/specs/openid-connect-core-1_0-18.html)
- [OAuth 2.0 for Browser-Based Applications, current IETF draft](https://datatracker.ietf.org/doc/draft-ietf-oauth-browser-based-apps/)
