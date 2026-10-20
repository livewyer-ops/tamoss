# TAMOSS Python Package

This package provides the TAMOSS FastAPI application for the BBC
Time-addressable Media Store API contract.

Deployment, operator installation and contributor workflows are documented in
the repository [README](../README.md).

## Package Scope

- HTTP routers and API schemas live under `app/tamoss/api/`.
- Application use cases, validation, and webhook delivery behaviour live under
  `app/tamoss/application/`.
- Domain types and port definitions live under `app/tamoss/domain/` and
  `app/tamoss/ports/`.
- PostgreSQL and S3-compatible object-storage adapters live under
  `app/tamoss/adapters/`.
- `app/tamoss/app.py` wires the FastAPI application, runtime authentication,
  error handling, and OpenAPI metadata alignment.

## Runtime

The package is intended to run inside the repo-managed container image or local
developer environment with PostgreSQL and S3-compatible storage configured by
environment variables. See the repository root README and `docs/configuration.md`
for deployment and runtime configuration details.
