# Cut a TAMOSS release

Use a clean checkout of the release branch. You need permission to push a version tag and access to the repository's Actions runs.

## Prepare the candidate

Use the implemented BBC TAMS API version followed by `-ossN`, incrementing the counter for each stable release of that API version. Append `-rcN` for a release candidate. Set `version` in your shell to the exact version you intend to publish.

Update `operator/compatibility.yaml` with the target TAMS API, schema revision, upgrade class and supported source versions. A candidate can use the compatibility entry for its stable version. Record the release notes in a non-empty section of `CHANGELOG.md` headed with the exact candidate or stable version. Review the catalogue of supported releases and the platform pins in `deploy/platform/dependencies.yaml`.

From the repository root, run:

```bash
git submodule update --init --recursive
python3 .github/scripts/release-metadata.py --validate
python3 .github/scripts/release-metadata.py "$version"
python3 .github/scripts/release-notes.py "$version"
task operator:install-manifest
git diff --exit-code -- deploy/operator/install.yaml operator/api operator/config operator/go.mod operator/go.sum
```

Commit any required generated changes to the release branch and qualify that revision. Run the checks in the [testing guide](testing.md), including `task security:audit` and `task kind:test PROFILE=local-kind`. Exercise the [upgrade procedure](../operations/upgrades.md) from the supported previous release and check the existing instance and its media after the upgrade.

## Tag the qualified revision

Check that `HEAD` is the reviewed commit on the remote release branch. Push an annotated tag for that commit:

```bash
git status --short
git log -1 --oneline
git tag -a "$version" -m "$version"
git push origin "refs/tags/$version"
```

Follow the **Docker Hub** workflow for the tag. After preflight, tests and the dependency audit pass, it builds and signs the API, UI, Console API and operator images. It then invokes **Operator Release Assets** with their digests. That workflow verifies the generated assets, prepares the GitHub Release as a draft and publishes it with the reviewed changelog entry.

## Verify publication

```bash
gh release view "$version" --json tagName,isDraft,isPrerelease,assets
gh release download "$version" --dir "dist/verify-$version"
jq '{sourceCommit,images,validationRun}' "dist/verify-$version/release.json"
git rev-parse "$version^{commit}"
```

Check that the release is published with the expected prerelease status. Compare the recorded commit with the peeled tag and the four image digests with the build outputs. The assets are `install.yaml`, `compatibility.yaml`, `dependencies.yaml`, `catalogue.json` and `release.json`. Verify the checksums in `release.json` against the downloaded assets.

For a transient failure before publication, rerun the failed jobs for the same tag and commit. If the source needs a correction, qualify the corrected commit with a new version tag. Release preflight rejects attempts to replace a published release.
