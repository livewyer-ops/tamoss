# syntax=docker/dockerfile:1
FROM python:3.14.5-alpine3.22

ARG VERSION=tamoss-dev
ARG TAMS_API_VERSION=8.2

LABEL "org.opencontainers.image.authors"="LiveWyer"
LABEL "org.opencontainers.image.description"="TAMOSS API server"
LABEL "org.opencontainers.image.source"="https://github.com/livewyer-ops/tamoss"

COPY --from=ghcr.io/astral-sh/uv:0.9.25 /uv /uvx /bin/

WORKDIR /app
RUN addgroup -S -g 1001 tamoss && adduser -S -D -H -u 1001 -G tamoss tamoss && chown tamoss:tamoss /app
USER 1001:1001

EXPOSE 8000

ENV HOME=/tmp \
  PYTHONPATH=/app/app \
  UV_CACHE_DIR=/tmp/uv-cache \
  UV_PYTHON_DOWNLOADS=never \
  TAMOSS_VERSION=${VERSION} \
  TAMOSS_SERVICE_VERSION=${VERSION} \
  TAMOSS_TAMS_API_VERSION=${TAMS_API_VERSION} \
  UVICORN_WORKERS=1

COPY --chown=1001:1001 src/app ./app

RUN --mount=type=bind,source=src/pyproject.toml,target=/app/pyproject.toml \
  --mount=type=bind,source=src/uv.lock,target=/app/uv.lock \
  --mount=type=bind,source=src/README.md,target=/app/README.md \
  uv sync --frozen --no-dev

# Worker count is deployment-tunable via UVICORN_WORKERS; exec preserves
# signal handling for graceful shutdown.
CMD ["/bin/sh", "-c", "exec uv run uvicorn tamoss.app:app --host 0.0.0.0 --port 8000 --proxy-headers --forwarded-allow-ips '*' --workers \"${UVICORN_WORKERS:-1}\""]
