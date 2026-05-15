# syntax=docker/dockerfile:1
FROM python:3.11.14-alpine3.22

LABEL "org.opencontainers.image.authors"="LiveWyer"
LABEL "org.opencontainers.image.description"="TAMOSS API server"
LABEL "org.opencontainers.image.source"="https://github.com/livewyer-ops/tams"

COPY --from=ghcr.io/astral-sh/uv:0.9.25 /uv /uvx /bin/

WORKDIR /app
RUN addgroup -S -g 1001 tamoss && adduser -S -D -H -u 1001 -G tamoss tamoss && chown tamoss:tamoss /app
USER 1001:1001

EXPOSE 8000

ENV PYTHONPATH=/app/app \
  UV_CACHE_DIR=/tmp/uv-cache

COPY --chown=1001:1001 src/app ./app

RUN --mount=type=bind,source=src/pyproject.toml,target=/app/pyproject.toml \
  --mount=type=bind,source=src/uv.lock,target=/app/uv.lock \
  --mount=type=bind,source=src/README.md,target=/app/README.md \
  uv sync --frozen --no-dev

CMD ["uv", "run", "uvicorn", "tamoss.app:app", "--host", "0.0.0.0", "--port", "8000"]
