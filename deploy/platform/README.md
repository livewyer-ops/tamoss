# Platform

Runtime platform installation is orchestrated by `helmfile.yaml.gotmpl`.
Environment compositions select platform behaviour through their
`platform-values.yaml` file, merged on top of `values/defaults.yaml`.

Helmfile installs each major platform dependency as a separate Helm release:

- `cert-manager` in `cert-manager`
- `traefik` in `traefik`
- `cnpg` in `cnpg-system`
- `authentik` in `auth`
- `rustfs-operator` in `rustfs-system`
- `tamoss-platform-config` in `tamoss-platform`

Each dependency release uses its own namespace and `createNamespace`, so the
platform no longer templates dependency namespaces in an umbrella chart.

From this directory:

```bash
helmfile --kubeconfig "$KUBECONFIG" \
  --file helmfile.yaml.gotmpl \
  --state-values-file values/defaults.yaml \
  --state-values-file ../../deploy/environments/local-kind/platform-values.yaml \
  sync \
  --sync-args "--server-side=true --rollback-on-failure" \
  --wait \
  --wait-for-jobs
```

Most platform prerequisites use upstream Helm charts pinned in
`helmfile.yaml.gotmpl` and recorded in `dependencies.yaml`. TAMOSS-owned local
charts live under `charts/`:

- `charts/authentik-bootstrap` creates the generated Authentik bootstrap and
  PostgreSQL Secrets before the Authentik chart starts.
- `charts/rustfs-operator` installs the RustFS operator manifests because the
  operator source is pinned directly rather than consumed as an upstream chart.
- `charts/config` applies post-dependency configuration such as certificate
  issuers and explicit Authentik ingress resources.

Traefik CRDs are installed through the upstream Traefik chart CRD mechanism.
That means Helm can install them during first install, but like normal Helm CRDs
they are not removed on uninstall and should be treated as cluster API surface.

TLS issuer creation is selected with `tls.mode`:

- `selfSigned` creates a self-signed `ClusterIssuer`.
- `public` creates an ACME `ClusterIssuer` using HTTP-01.
- `existing` creates no issuer and expects the named ClusterIssuer to exist.
- `disabled` creates no issuer for BYO TLS Secret workflows.
