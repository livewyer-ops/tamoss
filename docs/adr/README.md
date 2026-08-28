# Decisions

This directory contains decision records for TAMOSS itself: the operator, the
runtime, and how they are deployed. Each record describes the rationale behind a
decision, which other options were considered, and the consequences we accepted
by choosing.

These records are based on the Markdown Any Decision Record template - see
<https://github.com/adr/madr> for the original template, and <https://adr.github.io/>
for more on ADRs in general. They deliberately follow the same format as the TAMS
specification ADRs in `src/vendor/bbc-tams/docs/adr/`, which are upstream's and
concern the API contract rather than this implementation. The two sets are
numbered independently; a reference to `ADR0038` without qualification means the
upstream one. The local copy of the template, trimmed to the conventions used
here, is [adr-template.md](./adr-template.md).

An accepted record is never reopened or edited to reflect a change of mind.
Supersede it with a new record instead, and set the old one's status to
`superseded by [ADR-xxxx](./xxxx-short-title.md)`, so the log shows what
governed the work and for how long.

Keep each record short, ideally a single page. If there is supporting material,
link to it rather than restating it here.

Not every change warrants an ADR. The test is architectural significance: record
a decision here when reversing it later would be expensive, or when the reasoning
would otherwise have to be reconstructed from the code.

## Creating a new ADR

0. Look at the existing records, and see whether this has been considered before or would supersede one of them
1. Copy [adr-template.md](./adr-template.md) to the next free number, with a title that names the decision (e.g. `0013-schema-migration-rollback.md`). Numbers are assigned in blocks, described below
2. Fill in at least the "Context and Problem Statement", "Considered Options" and "Pros and Cons of the Options" sections. The template carries the rest, and notes which MADR sections we leave out
3. Set `status` to `proposed` while it is under discussion, and to `accepted` when it is agreed. A record that is later replaced becomes `superseded by [ADR-xxxx](./xxxx-short-title.md)` rather than being edited
4. Record the consequences honestly; what the decision makes hard is the part that is useful later

Numbers are grouped so the log reads in a useful order: records accepted before
TAMS 8.2 first, then records accepted with 8.2 support, then everything still
`proposed`. A number is assigned from the right block when the record is first
committed and does not move afterwards, so a proposal that is later accepted
keeps its number and every reference to it stays valid.

## The records

| ADR Number | Title | Status |
| --- | --- | --- |
| [0000](./0000-record-architecture-decisions.md) | Record Architecture Decisions | accepted |
| [0001](./0001-media-never-transits-the-cluster.md) | Media Never Transits the Cluster | accepted |
| [0002](./0002-postgresql-as-index-and-work-queue.md) | PostgreSQL as Both Index and Work Queue | accepted |
| [0003](./0003-namespaces-as-the-tenancy-boundary.md) | Namespaces as the Tenancy Boundary | accepted |
| [0004](./0004-selectable-backends.md) | Selectable Backends Rather Than Bundled Ones | accepted |
| [0005](./0005-kubernetes-agnostic-api.md) | Keep the API Kubernetes-Agnostic | accepted |
| [0006](./0006-operator-owned-ingest-jobs.md) | The Operator Is the Only Author of Ingest Jobs | accepted |
| [0007](./0007-console-api-reads-kubernetes-for-the-ui.md) | A Separate Console API Reads Kubernetes for the UI | accepted |
| [0008](./0008-flowprofile-owns-the-tams-profile.md) | A Kubernetes Resource Owns Each TAMS Flow Profile | accepted |
| [0009](./0009-expand-flow-profiles-at-write-time.md) | Expand Flow Profile Metadata Into the Flow at Write Time | accepted |
