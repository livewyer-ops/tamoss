# Authentik managed Blueprint readiness

The TAMOSS operator renders per-instance Authentik Blueprint content and applies
it through Authentik's managed Blueprint API. The Kubernetes handoff is limited
to Secrets the operator needs itself:

- `Secret/{tamoss-name}-oauth2-creds` in the `Tamoss` namespace for the
  generated OAuth2 client ID and client secret.
- `Secret/authentik-api-token` in the Authentik platform namespace by default,
  or `.spec.auth.authentikBlueprints.apiTokenSecretRef`, for the bearer token
  used to call Authentik's API.

## Status signal

`IdentityBlueprintSubmitted=True` means the managed Blueprint API upsert/apply
completed. `IdentityReady=True` means the operator's OIDC discovery probe
succeeded for:

`/application/o/{applicationSlug}/.well-known/openid-configuration`

The status source is Authentik itself, not a Kubernetes Blueprint Secret.

## Platform implication

The platform Authentik install must expose its API at the configured internal
URL or issuer URL and provide an API token Secret in the platform namespace. The
operator does not install Authentik itself and does not mutate the Authentik
Deployment.
