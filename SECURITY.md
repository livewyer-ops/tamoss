# Security Policy

## Reporting a Vulnerability

Do not report security vulnerabilities through public GitHub issues.

Instead, please report them via one of the following methods:

1. **Email**: Send details to <hello@livewyer.com>
2. **Private Security Advisory**: Use GitHub's [private vulnerability reporting](https://github.com/livewyer-ops/tams/security/advisories/new)

### What to Include

Please include the following information in your report:

- **Type of vulnerability** (e.g., SQL injection, XSS, authentication bypass)
- **Affected version(s)**
- **Step-by-step instructions** to reproduce the issue
- **Proof of concept** or exploit code (if possible)
- **Potential impact** of the vulnerability
- **Suggested fix** if you have one

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

3. **Reverse-proxy identity headers are opt-in.** When
   `TAMOSS_TRUST_FORWARD_AUTH_HEADERS=1`, the API accepts `Remote-User` or
   `X-Authentik-Username` as authenticated identity. This is designed for the
   authenticating ingress path documented in `docs/deployment.md`: the ingress
   authenticates the caller and injects this header so downstream services
   don't have to re-validate. **The API itself does not verify it.** If the pod
   is reachable via any route that bypasses the ingress, clients can forge
   identity by setting this header themselves.

   Required deployment stance:

   - Ensure the API pod is reachable **only** through the authenticating
     ingress (NetworkPolicy, private Service, or otherwise).
   - If you need direct access for a debugging workflow, use
     `kubectl port-forward` on a developer machine where the port is
     not shared.
   - Do not expose the API directly with
     `TAMOSS_TRUST_FORWARD_AUTH_HEADERS=1` (NodePort, LoadBalancer without
     the ingress in front, or `hostNetwork: true`) unless you strip
     these headers at the edge.

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

### Presigned URLs

TAMOSS uses S3-compatible presigned URLs for media access. These URLs:

- Expire after a configured time (default: 1 hour)
- Can be shared and used by anyone who has the URL
- Should be treated as temporary credentials

Configure expiration times based on the deployment's access pattern.

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

For general questions, use the [GitHub Discussions](https://github.com/livewyer-ops/tams/discussions) page.
