# syntax=docker/dockerfile:1
ARG API_IMAGE=livewyer/tamoss-api:dev
FROM ${API_IMAGE}

RUN --mount=type=bind,source=src/pyproject.toml,target=/app/pyproject.toml \
  --mount=type=bind,source=src/uv.lock,target=/app/uv.lock \
  --mount=type=bind,source=src/README.md,target=/app/README.md \
  uv sync --frozen --no-dev --group test

COPY --chown=1001:1001 operator/test/chainsaw/storagebackend-reconciliation/default-web-identity/*.py /tests/
COPY --chown=1001:1001 operator/test/chainsaw/storagebackend-reconciliation/multiple-storagebackends-ingest/*.py /tests/
COPY --chown=1001:1001 operator/test/chainsaw/fixtures/*.py /tests/
