# Single Server

Use `single-server` when TAMOSS runs on one Kubernetes node or a small
self-managed cluster and should still use the same operator-managed backend
shape as the larger profile.

## Requirements

- A conformant Kubernetes cluster.
- A working kubeconfig with permissions to apply platform, operator, and
  instance resources.
- A default StorageClass or explicit storage classes in the `Tamoss` CR.
- DNS and TLS planning for API, UI, S3, and
  [Authentik](https://goauthentik.io/) hostnames.

## Install

### Provision a single-node cluster

Any conformant Kubernetes distribution works. On a blank Linux server,
[K3s](https://k3s.io/) provisions a suitable single-node cluster; disable its
bundled [Traefik](https://traefik.io/) so
the TAMOSS platform can install the pinned Traefik release, and set
`<node-address>` to the address you will manage the server through:

```bash
curl -sfL https://get.k3s.io | sh -s - --disable traefik --tls-san <node-address>
ssh <user>@<node-address> sudo cat /etc/rancher/k3s/k3s.yaml \
  | sed 's/127.0.0.1/<node-address>/' > ~/.kube/tamoss-single.yaml
export KUBECONFIG=~/.kube/tamoss-single.yaml
kubectl get nodes
```

Run the install commands from the workstation that holds this kubeconfig and
a clone of this repository.

### Validate the profile on Kind

For local validation on [Kind](https://kind.sigs.k8s.io/):

```bash
task kind:up PROFILE=single-server
```

This validates the `single-server` profile shape on Kind; it is not the
existing-cluster install path.

For an existing cluster:

Set `TAMOSS_VERSION` to the exact release to install. The generated environment
pins its operator installation and each instance independently.

```bash
export KUBECONFIG=/path/to/kubeconfig

task env:init TAMOSS_VERSION="$TAMOSS_VERSION" NAME=my-single-server PROFILE=single-server DOMAIN=tamoss.example.com
$EDITOR deploy/environments/my-single-server/platform-values.yaml
$EDITOR deploy/environments/my-single-server/operator/defaults.yaml
task env:apply ENV=my-single-server KUBECONFIG="$KUBECONFIG"
task env:wait ENV=my-single-server KUBECONFIG="$KUBECONFIG"
task env:summary ENV=my-single-server KUBECONFIG="$KUBECONFIG"
```

Work through the [Key Settings](#key-settings) while editing the two
generated files, before `task env:apply`.

## Key Settings

Start from the generated environment composition under
`deploy/environments/<name>`. Use `platform-values.yaml` to select platform
components. Set the shared domain in `operator/defaults.yaml`; the operator
derives instance hostnames and the shared Authentik address as described in
[Configuration](../configuration.md#installation-defaults).

The generated defaults select `ClusterIssuer/tamoss-public`; set the ACME email
in `platform-values.yaml`. For an existing issuer or pre-created TLS Secrets,
follow [Install](../operations/install.md#existing-cluster). Use
`tls.mode: selfSigned` when choosing self-signed certificates, and keep `tls.issuerName`
aligned with `clusterIssuer` in `operator/defaults.yaml`. Override normal
`Tamoss` YAML fields directly in `tamoss-patch.yaml` when you need different
provider ownership, resources, storage, or routing.

### One replica of everything

The profile runs one replica each of API, worker, and UI, one
[CNPG](https://cloudnative-pg.io/) PostgreSQL
instance, and one [RustFS](https://github.com/rustfs/rustfs) server. There
are no PodDisruptionBudgets or
anti-affinity rules; a node drain takes the service down until pods
reschedule. The profile accepts that trade-off by design.

### Scaling up without scaling out

Raise the resources of the single pods in `tamoss-patch.yaml` when the
defaults (API 384Mi request/768Mi limit, worker 128Mi/384Mi) run short:

```yaml
  api:
    resources:
      requests:
        cpu: 500m
        memory: 768Mi
      limits:
        cpu: "2"
        memory: 1536Mi
  backends:
    db:
      cnpg:
        resources:
          requests:
            memory: 1Gi
          limits:
            memory: 2Gi
```

When one node is no longer enough, move to `multi-server` rather than adding
replicas here. PodDisruptionBudgets, anti-affinity, and replicated CNPG
PostgreSQL are profile defaults there.

## Validate

```bash
task e2e:deployed PROFILE=single-server KUBECONFIG="$KUBECONFIG"
```

The checked-in target file behind this command,
[`tests/targets/single-server.env`](../../tests/targets/single-server.env),
carries the Kind validation hostnames; for a remote server whose hostnames
differ, copy
[`tests/targets/remote.env.example`](../../tests/targets/remote.env.example)
into the environment directory. Use `task env:summary` and the instance's
`status.endpoints` and `status.resolved.generatedSecrets` for the effective
URLs and Secret names. Set `TEST_TAMOSS_NAMESPACE=tams`,
`TEST_TAMOSS_CR_NAME=tamoss-single-server` and the resolved token Secret name in
`TEST_TAMOSS_TOKEN_SECRET`, then pass
`TARGET_ENV=deploy/environments/my-single-server/target.env` to the same
command. Supply browser login credentials through the target's
`TEST_TAMOSS_AUTH_USER` and `TEST_TAMOSS_AUTH_PASSWORD` settings or its password
Secret reference; `env:summary` prints the access details.

A fresh install has no demo media, so seed it first with the demo ingest
helper that `task kind:up` runs —
[`.tasks/lib/demo_ingest.sh`](../../.tasks/lib/demo_ingest.sh)
`<target.env> <media-file> <kubeconfig>` — or expect the deployed media
checks to fail on the first run.

See also:

- [Profiles](../concepts/profiles.md)
- [Provider Ownership](../concepts/provider-ownership.md)
- [Install](../operations/install.md)
