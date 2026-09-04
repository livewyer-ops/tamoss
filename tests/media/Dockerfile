# syntax=docker/dockerfile:1
FROM python:3.14.7-slim-bookworm

COPY --from=ghcr.io/astral-sh/uv:0.9.25 /uv /uvx /bin/

RUN apt-get update \
  && apt-get install -y --no-install-recommends ffmpeg \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /work
