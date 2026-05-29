# RULES.md

Behavioral guidelines to reduce common coding mistakes. Merge with project-specific instructions as needed.

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.


---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.

The following is the LiveWyer Cloud Native Operational Engineering Standards (CaNOES)

## 5. Engineering Standards

- Always prioritise these standards
- When you are about to implement something against these standards, prompt the user with a clear description of how it will break these standards and authorise the change.
- Non-standard design decisions must be captured in the closest owned ADR, design document, or release note; do not create repo-wide exception ledgers.

* Whenever possible remove duplicated data entry, try to maintain a single source of truth
* Minimise any requirements for local tool installation
  - Use Dockerfile / docker builds to contain tool requirements
* Technical elegance is preffered over shoe-horned solutions
  - If you are constantly having to add forced solutions to problems AKA "going against the grain" then re-review the overall design and suggest a more aligned approach to ensure "elegance"
* Where possible reuse the same tooling, standards, formats etc to minimise technical knowledge spread
* Always approach a project as a product
  - Consider the longterm maintainability as well as the day 0 "from nothing" experience of a new user
  - Documentation and folder structures should be consistent
  - Reduce the spread of temporary or short term files in the codebase
* Approach the work in a short, provable iterative loop
  - Avoid making too much change at once without having a testing process for the changes you have made
  - A git commit of your work is confirmation that we are hapy with the changes made and can focus on the next iterative cycle
* See things from an operational, systems administrator, cloud native engineer persepctive first
  - Then review things from an end user experience for simplicity
* When interacting with code which has created infrastructure assets on a paid cloud account, ensure that any changes can still result in the successful deletion of said resources and nothing is left "orphaned"
* When dealing with state driven code, confirm all destructive actions
* Where possible we want the single source of truth to be applied and inhereted by resources, processess etc rather then duplicated in any way.
* Kubernetes "convergence" patterns are preferred to "one-shot" interactions
* Try to avoid creating scripts to orchestrate actions and create minimise entry points which consist of 4 or less commands
* TAMOSS Kubernetes workloads are reconciled by the TAMOSS operator from `Tamoss` and related CRs. Third-party prerequisite operators may use whichever install path their upstream documents.
* Don't change component versions unless confirmed by user  Provide clear reason for version change
* Do not manually create resources, unless temporarily testing something all resources must be applied via the codebase that is meant to apply them rather then hacking a quick fix, progress is measured in working code not immediate patches
*  We start with the latest version of all software, components and dependencies and only revert back to old versions if we have a clear need to do so which is confirmed by the end user
* DO NOT randomly apply self generated yaml or one-shoot fixes. The goal is for changes to be in the code base so we can recreate the environment from scratch
* The goal is always to build an infrastructure software product that can be used by a third party with no assumptions. Not just a working target environment.
 - If we cannot get to the current state using our codebase then we have failed our current task.
 - This means patching resources with configuration and settings is *NOT ALLOWED* whereas patching to start a process (like reconciling) is.
 - Configuration flow is important. Duplicate hardcoded configuration in our code should be replaced by a single source of truth which flows through by variables and inheritence.
