# Release Runbook

Release candidates and stable releases use the same gates. This runbook builds
on the release workflow proposed in myauie's PR #176, with all four images and
immutable release records included.

## Prepare

1. Agree the release branch and exact commit. The 8.2 candidate branch is
   `8.2-preview`; do not assume `main` contains the candidate changes.
2. Review dependency and contributor PRs individually. Include compatible
   updates with passing checks; leave major or behaviour-changing updates for
   a separate compatibility review. Do not batch-merge solely by author.
3. Verify `operator/compatibility.yaml`, migration assets, generated manifests,
   and the pinned `src/vendor/bbc-tams` revision. Do not change the submodule
   independently of the generated contract and conformance tests.
4. Run `task check` and the deployed Kind gate. Retain test reports, including
   PostgreSQL rollback/concurrency tests, webhook transport tests and 8.2
   contract and semantics results.
5. Complete the [Cutting Rooms acceptance gate](../operations/cutting-rooms.md)
   on the candidate. A successful TAMOSS write or webhook HTTP 2xx is not
   evidence that the CR catalogue or playback works.

Resolve metadata before tagging:

```bash
python3 .github/scripts/release-metadata.py 8.2.0-oss1-rc5
git rev-parse HEAD
git submodule status src/vendor/bbc-tams
```

The RC number above is an example. Use a new, unused version; never reuse RC4
or any other published tag. Compatibility metadata derives an RC from its
base release, so a separate compatibility entry is not required for each RC.

## Publish

Create an annotated version tag on the reviewed commit and push that tag.
The `Docker Hub` workflow is the sole release orchestrator. It:

1. Validates metadata and refuses to overwrite an already published release.
2. Runs the dependency audit and the Python, frontend, operator and Kind gates
   against the tagged commit. Kind builds that source; this is not a claim
   that Kind tests both architectures of the published images.
3. Builds, pushes and signs API, UI, Console API and operator images for
   `linux/amd64` and `linux/arm64`.
4. Calls `Operator Release Assets` only after every prerequisite succeeds.
5. Generates a digest-pinned operator install manifest and `release.json`,
   creates the draft release and then publishes it.

The reusable asset workflow has no independent tag or manual trigger. A
failed or cancelled prerequisite must leave the release unpublished.

The four image jobs share a composite build/sign action. They remain in
`docker-hub.yaml` to preserve the signing identity and per-image digest outputs.

`release.json` contains the source SHA, BBC specification SHA, compatibility
metadata, four immutable image references, worker/API image identity, asset
SHA-256 checksums, and the validating workflow run and attempt. The worker
uses the API image; it is not a fifth independently built image.

## Verify and Promote

- Download `release.json`, `install.yaml` and `compatibility.yaml` from the
  published release. Verify the asset hashes against the release record and
  image signatures against this repository's Docker Hub workflow identity.
- Compare all deployed image digests and schema/API versions with that record.
  Pin digests where the deployment interface supports them; otherwise record
  the resolved digest for every configured version tag, including the worker.
- Upgrade a non-production environment first. For the Cutting Rooms pilot,
  confirm the actual instance is `cr-tamoss-1` in namespace `cr-tams`; do not
  infer it from a `dev-` directory name or a similarly named instance.
- Retain the environment CR revision, migration result, acceptance evidence
  and a complete restore record alongside the release record. Use the
  [upgrade](../operations/upgrades.md) and
  [backup/restore](../operations/backup-restore.md) procedures.
- Promote a stable release only after CR-side catalogue, playback and outage
  recovery checks pass. CI's generic receiver cannot certify an external CR
  deployment. Record any missing access as **not run**, not a pass.

## Failed Releases and Rollback

For an unpublished candidate, fix the cause and rerun the failed jobs against
the same immutable commit, or abandon it and use a new tag. Re-running all
jobs after publication is intentionally rejected. Never delete and recreate
a published tag, overwrite release assets or manually publish a partial draft.

Before upgrading, record the previous four image digests, worker digest,
source and environment revisions, database migration revision, backup ID and
recovery point, object-store bucket/versioning state, and a completed restore
drill. Back up Authentik/platform state under its own ownership as required.
Keep credentials and signed URLs out of release notes and public CI artifacts.

An image rollback is not a schema rollback. If the previous release cannot
read the migrated schema, restore the database into a separate instance and
verify it with the matching images and object storage before moving traffic.
Stop writers during the final cutover and account for writes after the backup
point. A backup status alone is not proof that this recovery works.
