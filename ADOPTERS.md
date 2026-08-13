# Adopters

This page lists organisations and projects using TAMOSS, in production, for evaluation, or as a reference implementation of the [BBC TAMS API](https://github.com/bbc/tams).

TAMOSS is pre-1.0. The current release series (`8.1.0-ossN`) tracks BBC TAMS v8.1, and the operator API (`Tamoss`, `StorageBackend`, `IngestRun`, `TamossHibernation`, etc) may still change between releases.

## Who is using TAMOSS

| Adopter                  | Since | Status | Profile | Contact (optional) | Use case |
|--------------------------|-------|--------|---------|--------------------|----------|
| _your organisation here_ |       |        |         |                    |          |

**Status**: `Evaluating`, `Non-production`, or `Production`.

**Profile**: One or more of `local-kind`, `edge`, `single-server`, `multi-server`. See [Profiles](docs/concepts/profiles.md).

**Use case**: `Running a TAMS store`, `Client integration` (you build a client that talks to a store), `Storage backend` (you provide the object storage underneath), etc.

## Adding yourself

This document also lists organisations using TAMOSS based on public information in blog posts, events, and videos.

If you are running TAMOSS (even a Kind evaluation, a lab deployment, or a conformance comparison against your own TAMS implementation), **[open an adopter issue](https://github.com/livewyer-ops/tamoss/issues/new?template=adopter.yaml)** with your details. No pull request is needed, a maintainer will add your row for you. Please note:

- Only include what you are happy to publish. A row with just an organisation name and a status is welcome; a public contact is optional, and a team alias or GitHub handle is fine in place of a person.

If you want to talk about a deployment before listing it, feel free to use [GitHub Discussions](https://github.com/livewyer-ops/tamoss/discussions) or the `#tamoss` Slack channel. If you would rather not be listed publicly but are happy for us to know, you can also email <hello@livewyer.com>.
