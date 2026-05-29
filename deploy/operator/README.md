# Operator Install Manifest

`install.yaml` is generated from the operator Kustomize configuration for
low-tooling installation paths.

Regenerate it with:

```bash
task operator:install-manifest
```

Do not hand-edit `install.yaml`; update the operator source manifests instead.
