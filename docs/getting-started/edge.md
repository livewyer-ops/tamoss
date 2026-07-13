# Edge

`edge` is the single-node ARM64 profile for self-contained TAMOSS installs. It
uses the normal operator install path, disables Authentik, and keeps API access
token-only through the TAMOSS API token Secret.

## Cluster Requirements

- A 64-bit ARM Kubernetes node. Raspberry Pi 4 Model B with 4 GB RAM is the
  minimum target; larger Raspberry Pi 4/5 boards give more headroom.
- Raspberry Pi OS Lite 64-bit with K3s is the reference software shape.
- Persistent storage backed by an SSD. Avoid SD-card-backed database and object
  storage.
- Active cooling, a stable power supply, and wired Ethernet.
- Swap configured on SSD-backed storage for constrained 4 GB nodes.
- A default StorageClass, or explicit CNPG and RustFS storage classes in the
  `Tamoss` CR.
- cert-manager, Traefik, CNPG, and RustFS Operator installed by the TAMOSS
  platform layer unless the cluster already owns those services.
- Local DNS or host entries for API, UI, and S3 hostnames.

K3s should be installed without its bundled Traefik when the TAMOSS platform
installs the pinned Traefik release for this profile.

## Install

For local profile validation on Kind:

```bash
task kind:up PROFILE=edge
```

For an existing ARM64 cluster:

```bash
export KUBECONFIG=/path/to/kubeconfig

task env:init NAME=my-edge PROFILE=edge DOMAIN=tamoss.edge
$EDITOR deploy/environments/my-edge/platform-values.yaml
$EDITOR deploy/environments/my-edge/tamoss-patch.yaml
task env:apply ENV=my-edge KUBECONFIG="$KUBECONFIG"
task env:wait ENV=my-edge KUBECONFIG="$KUBECONFIG"
task env:summary ENV=my-edge KUBECONFIG="$KUBECONFIG"
```

The generated edge platform values disable Authentik and use self-signed TLS by
default. Keep `authentik.enabled: false` unless the install intentionally moves
to an identity-backed profile.

## Defaults

The operator defaults for `edge` are intentionally smaller than `single-server`
and sized to fit a Raspberry Pi 4 Model B with 4 GB RAM:

- API, worker, and UI run as one replica each.
- Authentik is not selected by default.
- Runtime auth is token-only with OAuth2 disabled.
- CNPG runs one PostgreSQL instance with a 10 GiB volume and bounded resources.
- RustFS Operator runs one server with four 10 GiB volumes.
- RustFS disk checks are bypassed for single-node local storage.
- API and worker runtime pools are reduced for small ARM systems.
- Worker health probes use longer timeouts for ARM startup and import latency.
- TLS defaults to `ClusterIssuer/tamoss-edge-selfsigned`.

The API token is stored in the generated `<fullname>-api-token` Secret. Use
`task env:summary` to print the resolved Secret value after the instance is
ready.

## Configure

Use `tamoss-patch.yaml` for durable overrides:

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: Tamoss
metadata:
  name: tamoss-edge
  namespace: tams
spec:
  profile: edge
  publicEndpoint:
    baseDomain: tamoss.edge
  backends:
    db:
      cnpg:
        storage:
          size: 20Gi
    s3:
      rustfsOperator:
        pools:
          - name: pool-0
            servers: 1
            volumesPerServer: 4
            storage:
              size: 20Gi
```

Use larger PVC sizes on 128 GB or larger SSDs, and make backup ownership
explicit before accepting durable data.

See also:

- [Profiles](../concepts/profiles.md)
- [Install](../operations/install.md)
- [Provider Ownership](../concepts/provider-ownership.md)
