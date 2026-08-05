# Getting Started

Pick a profile and follow its guide. Remote installs use the environment
workflow: run `task env:init`, edit the generated `platform-values.yaml` and
`tamoss-patch.yaml`, then run `task env:apply` and `task env:wait`.
`local-kind` runs the whole flow with `task kind:up`.

| Profile | Use it for |
| --- | --- |
| [`local-kind`](local-kind.md) | Disposable local evaluation on [Kind](https://kind.sigs.k8s.io/). |
| [`edge`](edge.md) | One ARM64 node, 4 GB memory floor. |
| [`single-server`](single-server.md) | One durable node or small cluster. |
| [`multi-server`](multi-server.md) | Production, multi-node. |

## Common Configuration

Make these three decisions before `task env:apply`. Each guide's Key
Settings section covers the profile-specific parts.

### DNS

`local-kind` needs no DNS: it uses `tamoss.localtest.me` hostnames, which
resolve to 127.0.0.1 from public DNS. Every other profile derives `api`,
`app`, `s3`, and `auth` hostnames from `spec.publicEndpoint.baseDomain`.
Create real DNS records for those names, or use a wildcard resolver such as
`<ip>.sslip.io` as the base domain when no DNS control exists. Host-file
entries work for edge installs on private networks.

### TLS

Set `tls.mode` in the environment `platform-values.yaml`:

- `selfSigned` (local-kind and edge default): no external dependencies;
  browsers and API clients must trust or skip verification.
- `public`: [cert-manager](https://cert-manager.io/) requests certificates
  from Let's Encrypt. Requires
  real DNS for the derived hostnames, an ACME email in the platform values, and
  port 80 reachable for HTTP-01.
- `existing`/`disabled`: certificate ownership stays outside the platform
  layer; name the TLS Secrets in the `Tamoss` CR.

### Auth

Every profile enforces authentication by default. `local-kind`,
`single-server`, and `multi-server` run the managed
[Authentik](https://goauthentik.io/) stack and issue
OAuth logins. `edge` defaults to a bearer-token Secret and can opt into the
same Authentik stack; both modes are covered in the
[edge guide](edge.md#key-settings). OAuth requires the `auth` hostname to
resolve and carry valid TLS, so configure DNS and TLS first.

See [Profiles](../concepts/profiles.md) for the full comparison and
[Install](../operations/install.md) for the environment workflow.
