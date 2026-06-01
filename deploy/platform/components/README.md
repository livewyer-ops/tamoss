# Platform Components

Most files below this directory are vendored or generated from pinned upstream
versions recorded in `deploy/platform/dependencies.yaml`. The runtime platform
entrypoint is the Helm chart in `deploy/platform/chart`; these component files
are the checked-in upstream inputs used by that chart.

Regenerate checked-in platform manifests with:

```bash
task operator:platform:vendor
task operator:platform:deps:check
```

Do not hand-edit generated upstream manifests unless a component README says
otherwise; patch them through Kustomize overlays or the vendoring inputs.
