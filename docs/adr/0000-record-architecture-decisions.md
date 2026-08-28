---
status: "accepted"
---
# Record Architecture Decisions

## Context and Problem Statement

TAMOSS has no decision records of its own.
The reasoning behind its load-bearing choices exists only in the implementation: where media flows, what the work queue is, and where the tenancy boundary sits.

That cost is paid twice.
It is paid whenever someone questions a design, because the alternatives that were rejected have to be reconstructed from the code, and a reconstruction is not evidence that anyone weighed them.
It is paid again whenever a decision is revisited, because there is no record of what the choice was supposed to cost, so a proposal argues against an imagined price rather than a stated one.

The only ADRs in the tree belong to the vendored TAMS specification and concern the API contract, not this implementation.

## Considered Options

* Option 1: Continue with the two existing files and accept the poor fit
* Option 2: Add a single `docs/concepts/decisions.md` page listing the decisions
* Option 3: Adopt MADR records, one file per decision, in `docs/adr/`

## Decision Outcome

Chosen Option 3: Adopt MADR records, one file per decision, in `docs/adr/`.

This matches the convention already present in the repository for the TAMS specification, so there is one format to learn rather than two.
One file per decision gives each record its own history, its own reviewable pull request, and a stable reference other documents can link to.

**Confidence:** High.
The format is already proven in this repository by the vendored specification records, so it is not an untested choice.

**Reevaluate if:** the number of records outgrows a flat directory, or people who do not work in Git need to contribute records.

### Consequences

* A record here answers why the system is shaped as it is. Defects and outstanding work are not decisions and belong in the issue tracker, so the boundary has to be held deliberately.
* Two records numbered from zero now exist in one repository. The README states that an unqualified `ADR0038` means the upstream one.
* Records must be written when the decision is taken. A directory of ADRs that lags the code is worse than none, because it is trusted.

## Pros and Cons of the Options

### Option 1: Continue with the two existing files

* Good, because it needs no new structure and no migration
* Bad, because the reasoning behind the load-bearing decisions stays unwritten
* Bad, because entries continue to be filed in documents whose stated purpose they contradict

### Option 2: A single `decisions.md` page

* Good, because it is one file to write and one to read
* Good, because it sits naturally in the existing `docs/concepts/` structure
* Bad, because decisions accumulate and a single page becomes a document nobody revises
* Bad, because there is no per-decision history, status, or stable reference to cite

### Option 3: MADR records in `docs/adr/`

* Good, because it matches the convention already vendored in this repository
* Good, because each decision carries its own status, history, and reviewable change
* Good, because the "Considered Options" section forces the alternatives to be written down rather than implied
* Neutral, because it introduces a second ADR numbering sequence alongside upstream's
* Bad, because it is more files to maintain than a single page
