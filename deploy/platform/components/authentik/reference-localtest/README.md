# Authentik Reference Localtest Overlay

This overlay is the checked-in Authentik reference manifest used by TAMOSS
platform profiles until a production identity overlay is introduced. It carries
localtest hostnames and bootstrap values, so remote environments must patch
identity routing from their environment overlay before production use.

Regenerate the rendered manifest with:

```bash
task operator:platform:vendor
```
