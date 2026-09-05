# Releases

How to cut a TAMOSS release. This is a maintainer task: it requires push access to `main` and permission to push tags.

Releases are triggered by pushing a tag, not by merging. Nothing is published until a tag matching `*.*.*` lands on the repository.

## Version scheme

A release version is the BBC TAMS API version the release implements, followed by an `-ossN` counter for TAMOSS releases against that API version.

```text
8.1.0-oss6
│ │ │  └── sixth TAMOSS release implementing TAMS 8.1
└─┴─┴───── implemented BBC TAMS API version
```

Increment `-ossN` for a normal release. The API portion changes only when `src/vendor/bbc-tams/` moves to a new contract version, which is a deliberate contract update in its own right.

## 1. Pre-flight on `main`

Start from a clean checkout of the commit you intend to release.

```bash
git checkout main && git pull
git submodule update --init --recursive
```

Run the gates:

```bash
task check
task test:tams
task security:audit
task versions:check
task kind:test PROFILE=local-kind
```

Then confirm the generated operator assets are current:

```bash
task operator:install-manifest
git status --short deploy/operator/install.yaml operator/api operator/config
```

The release workflow re-runs the render and fails the release if `deploy/operator/install.yaml`, `operator/api`, `operator/config`, `operator/go.mod`, or `operator/go.sum` come back dirty. If the render produces a diff, commit it before releasing rather than discovering it after the tag exists.

## 2. Create the release branch

```bash
git checkout -b release/<version>
```

## 3. Add the release to `operator/compatibility.yaml`

This file is the source of truth for a release. Append an entry:

```json
{
  "version": "8.1.0-oss7",
  "tamsAPI": "8.1",
  "schemaRevision": "8.1.0-oss2",
  "upgrade": {
    "class": "APIOnly",
    "from": ["8.1.0-oss6"]
  }
}
```

| Field | Meaning |
| --- | --- |
| `version` | The release version. Must be unique across the file, and must equal the tag you will push. |
| `tamsAPI` | The BBC TAMS API version this release implements. |
| `schemaRevision` | The database schema revision. Carry the previous value forward unless this release changes migrations under `src/app/tamoss/db/migrations/assets/`. |
| `upgrade.class` | How an upgrade into this release behaves. Existing entries use `FreshInstall`, `APIOnly`, and `SchemaAndAPI`. |
| `upgrade.from` | The versions that can upgrade directly into this release. Every entry must name a release already present in the file. |

Two validation rules are easy to trip:

- Every version named in `upgrade.from` must already exist in the file.
- All versions named in `upgrade.from` must share the same `schemaRevision`. The release workflow derives a single `previous_schema_revision` from that list and fails if the sources disagree.

Check it locally before pushing:

```bash
python3 .github/scripts/release-metadata.py --validate
python3 .github/scripts/release-metadata.py <version>
```

The first validates the whole file. The second prints the metadata the release workflow will resolve for your version, and fails if the version is absent.

## 4. Open and merge the pull request

Open a pull request from `release/<version>` into `main`. CI validates `operator/compatibility.yaml` through `release-metadata.py --validate` in both the operator and validate workflows, so a malformed entry fails here rather than at tag time.

Release notes are generated from merged pull request titles, so the titles that landed since the previous tag become the published notes. Merge the release pull request normally once it is green.

## 5. Tag and push

Tag the merge commit on `main`:

```bash
git checkout main && git pull
git tag <version>
git push origin <version>
```

The tag must match `*.*.*` and must exactly match the `version` field added in step 3.

## 6. What the tag triggers

Two workflows run independently on the tag:

- `.github/workflows/operator-release.yaml` validates the tag ref and format, resolves the release metadata, verifies the generated operator assets are current, renders an install manifest pinned to the released image tag, and creates the GitHub Release with `install.yaml` and `compatibility.yaml` attached plus generated notes.
- `.github/workflows/docker-hub.yaml` builds and pushes `livewyer/tamoss-api`, `livewyer/tamoss-ui`, and `livewyer/tamoss-operator`.

## 7. Verify

- The GitHub Release exists for the tag, with `install.yaml` and `compatibility.yaml` attached.
- The three images are published for the version.
- `install.yaml` from the release pins the operator image to the released tag.

## If a release fails

The tag already exists, so fix the cause on `main` first, then move the tag:

```bash
git push origin :refs/tags/<version>
git tag -d <version>
# land the fix, then re-tag the new merge commit
git tag <version> && git push origin <version>
```

Deleting and re-pushing a tag re-runs both workflows. Anything the first attempt already published, including container images, is overwritten rather than removed, so check the published artifacts match the retagged commit.
