# Documentation Structure

TAMOSS documentation follows the [Diataxis](https://diataxis.fr/) model. Choose
the page type from the reader's immediate need before choosing a directory or
writing an outline.

| Reader need | Page type | Location | Typical shape |
| --- | --- | --- | --- |
| Learn by completing a first working journey | Tutorial | `docs/getting-started/` | Guided sequence with a known starting point and a verified result |
| Complete an operational goal | How-to guide | `docs/operations/` | Prerequisites, ordered actions, verification, and recovery |
| Look up an exact contract | Reference | `docs/reference/` | Fields, accepted values, defaults, status, and invariants |
| Understand why the product behaves this way | Explanation | `docs/concepts/` | Boundaries, relationships, trade-offs, and mental models |

`docs/development/` contains contributor workflows and internal design records.
Design records preserve decisions, alternatives, and unfinished gates; they do
not replace current user-facing explanation, operations, or reference pages.

## Authoring Rules

- Give each page one primary purpose. Link to another page type instead of
  embedding a second manual in the same page.
- Keep tutorials reliable and linear. Avoid optional branches until the reader
  has reached the promised working result.
- Write how-to guides around a concrete goal, not around a component inventory.
  Include a check that proves the goal was achieved.
- Keep reference pages factual and scannable. Put rationale in concepts and
  procedures in operations.
- Describe enduring behaviour by capability. Use release numbers only when the
  version boundary itself matters, such as an upgrade or release-delta record.
- Name the public product resource before its implementation detail. For
  example, an `IngestRun` is the durable request and history record; its Tamsin
  Kubernetes `Job` is operator-owned execution machinery, not a second public
  resource called an "IngestJob".
- State current behaviour in public documentation. Put proposed or unavailable
  behaviour in a design record, and label it explicitly.
- Use British English in prose and commit messages. Do not alter literal API
  fields, commands, error codes, or upstream names to satisfy this rule.
- Do not include Secret values, bearer tokens, private object locators, or full
  presigned URLs in examples or diagnostics.

## Review Checklist

Before merging documentation:

1. Identify the page type and confirm its directory matches.
2. Check that headings serve that page type rather than mixing tutorial,
   procedure, lookup, and rationale.
3. Verify commands and field names against the current implementation.
4. Link to the other page types needed to complete the reader's journey.
5. Run the repository Markdown, spelling, and link checks through pre-commit.

The [IngestRun concept](../concepts/ingest-runs.md),
[operations guide](../operations/manage-ingest-runs.md), and
[CR reference](../reference/ingestrun-cr.md) show one capability split across
explanation, procedure, and lookup without duplicating the internal design
record.
