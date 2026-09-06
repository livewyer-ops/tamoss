# Security Policy

## Reporting a Vulnerability

Do not report security vulnerabilities through public GitHub issues.

Instead, report them privately via one of the following methods:

1. **Email**: Send details to <hello@livewyer.com>
2. **Private Security Advisory**: Use GitHub's [private vulnerability reporting](https://github.com/livewyer-ops/tamoss/security/advisories/new)

### What to Include

Please include the following information in your report:

- **Type of vulnerability** (e.g., SQL injection, XSS, authentication bypass)
- **Affected version(s)**
- **Step-by-step instructions** to reproduce the issue
- **Proof of concept** or exploit code (if possible)
- **Potential impact** of the vulnerability
- **Suggested fix** if you have one

Do not include production credentials, bearer tokens, private keys, database
passwords, S3 access keys, OAuth client secrets, or complete presigned object
URLs. If a credential is required to demonstrate impact, explain the requirement
and wait for maintainers to agree on a safe exchange path.

## Security Considerations

### Authentication — deployment assumptions

The auth chain in `src/app/tamoss/auth.py` accepts Bearer tokens, HTTP Basic
credentials, URL access tokens, OAuth2 JWTs, and trusted reverse-proxy identity
headers when explicitly configured. Three behaviours must be understood before
deploying TAMOSS in production:

1. **Authentication should be required in production.** Set
   `TAMOSS_AUTH_REQUIRED=1` and configure at least one production authentication
   method. The simplest static-token setup uses `TAMOSS_API_TOKEN`; OAuth2
   deployments should configure the `TAMOSS_OAUTH2_*` settings described in
   `docs/configuration.md`.

2. **HTTP Basic is implemented as a compatibility scheme.** By default,
   Basic auth uses username `tamoss` and the configured API token as its
   password. Set `TAMOSS_BASIC_AUTH_USERNAME` and
   `TAMOSS_BASIC_AUTH_PASSWORD` if you need separate Basic credentials.
   Bearer token or OAuth2 auth is preferred because Basic sends a reusable
   secret on every request.

3. **Reverse-proxy identity is proof-bound and opt-in.** When
   `TAMOSS_TRUST_FORWARD_AUTH_HEADERS=1`, the API trusts only normalised
   `X-TAMOSS-Forward-Auth-*` headers carrying a verifier-specific proof, a
   stable subject, and groups matching configured bindings. `Remote-User` and
   raw `X-Authentik-*` headers are ignored. Direct Bearer, Basic, and URL-token
   authentication takes precedence and retains its own authority. Forwarded
   identities are limited to explicitly mapped `GET` and `HEAD` routes that
   admit read scope; unknown routes and mutations are denied.

   Required deployment stance:

   - Strip browser-supplied authorization and identity headers before adding
     only the normalised allow-list established by the authentication
     subrequest; keep that subrequest location internal and non-routable.
   - Use distinct operator-generated proofs for API and Console. The proxy may
     receive both issuer keys, but each verifier receives only its own key;
     proofs must never reach the browser, logs, or unrelated workloads.
   - Isolate API and Console verifier Services from untrusted bypass paths.
     Allow only the trusted proxy and separately intended direct-auth clients.
   - Declare destination-scoped NetworkPolicy egress yourself under
     `spec.networkPolicy.<component>.egress`: the UI needs only its Authentik,
     API, and Console destinations, and Console needs only the intended
     Kubernetes API endpoint (plus required DNS). Default egress is port-scoped
     only, because destination-scoped defaults are deferred until they can be
     verified against an enforcing CNI, so an unmodified install does permit
     arbitrary destinations on those ports. Setting
     `spec.networkPolicy.kubernetesAPIIPBlocks` narrows the Console 443 and 6443
     rule but leaves the remaining rules port-scoped.

### Object Storage

TAMOSS supports S3-compatible storage backends. Production deployments should
apply provider-appropriate controls to the selected backend, whether that is a
managed cloud object store, an on-premises S3-compatible service, or the local
RustFS profile used by the Kind stack.

At minimum:

- Keep API/worker credentials separate from browser or user credentials.
- Scope credentials to the required bucket or prefix where the provider allows
  it.
- Disable public bucket access unless a deployment has a deliberate public-media
  requirement.
- Enable server-side encryption, audit logs, lifecycle policy, and backup or
  replication according to the provider's capabilities.
- Expose browser-facing object URLs only through the intended public endpoint.
- Configure CORS at the public object-store endpoint so browser uploads are
  limited to the TAMOSS UI origin and required methods/headers.

### Presigned URLs

TAMOSS uses S3-compatible presigned URLs for media access. These URLs:

- Expire after a configured time (default: 1 hour)
- Can be shared and used by anyone who has the URL
- Should be treated as temporary credentials

Configure expiration times based on the deployment's access pattern.

Do not paste full presigned URLs into public issues. They can grant temporary
access to media objects until they expire.

### Media Content

TAMOSS allocates object IDs and presigned object-store URLs. Media bytes are
uploaded to the configured storage backend; the API records Flow Segment
metadata after upload. TAMOSS does not proxy media uploads or scan files.

Deployments that accept media across a trust boundary should enforce content
controls in the storage path:

- Malware scanning
- Media format validation
- Object size limits
- Upload rate limits at the storage ingress or API gateway

### Database Access

- The API has **full database access**
- Use **connection pooling** to prevent connection exhaustion
- Monitor for **SQL performance issues** that could indicate attacks

## Additional Resources

- [OWASP Web Security Testing Guide](https://owasp.org/www-project-web-security-testing-guide/)
- [FastAPI Security Documentation](https://fastapi.tiangolo.com/tutorial/security/)
- [Docker Security Best Practices](https://docs.docker.com/develop/security-best-practices/)
- [PostgreSQL Security](https://www.postgresql.org/docs/current/security.html)
- Apply the hardening guidance from the provider of the selected S3-compatible backend

## Contact

For security concerns, contact: <hello@livewyer.com>

For general questions, use the [GitHub Discussions](https://github.com/livewyer-ops/tamoss/discussions) page.
