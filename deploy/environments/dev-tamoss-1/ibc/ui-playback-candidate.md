# Internal public playback candidate

Source: `12b0115e559c436a691829fdaab0aa1e584302cb` on
`fix/ibc-portrait-verification`, based on RC7 plus the branding subtitle removal.
[Signed build](https://github.com/livewyer-ops/tamoss/actions/runs/34250758118).

## Scope

Only the public UI changes. HLS owns joint audio/video playback; the custom audio
sidecar synchroniser is removed. Start/recovery requires eight seconds of joint
buffer (or the remainder near the end); playback holds below one second.
User pause survives recovery, readiness failures stop after 30 seconds, and
Retry playback obtains a fresh descriptor and player.

The existing logo, portrait containment, media timing, initialization objects,
signed URL redaction and access boundaries are retained. No API, schema, TAMSin,
worker, Console API, operator, private deployment, storage or recording changes.
No dependency was added. No official RC or stable release is created.

## Deploy and rollback

The candidate and rollback files are JSON merge patches for the Tamoss parent,
not standalone installation manifests. Use the GKE kubeconfig from the operational
worktree. Do not apply the whole IBC kustomization or change child Deployments.

```sh
kubectl -n ibc-tamoss-public patch tamoss ibc-tamoss-public --type=merge \
  --patch-file=deploy/environments/dev-tamoss-1/ibc/ui-playback-candidate.patch.yaml
kubectl -n ibc-tamoss-public wait --timeout=3m \
  --for='jsonpath={.spec.template.spec.containers[0].image}=livewyer/tamoss-ui:sha-12b0115@sha256:9a3c91fd6df7a52eb5ec22beb7b42bd3d79f68f23a9714137bef5b791577a64c' \
  deployment/ibc-tamoss-public-ui
kubectl -n ibc-tamoss-public rollout status deployment/ibc-tamoss-public-ui --timeout=5m
```

Candidate: `livewyer/tamoss-ui:sha-12b0115@sha256:9a3c91fd6df7a52eb5ec22beb7b42bd3d79f68f23a9714137bef5b791577a64c`.
Cosign verification passed for the branch Docker Hub workflow identity and GitHub
OIDC issuer, including certificate and transparency-log checks. Both architecture
image configurations declare the source commit; the signed index includes SBOM
and provenance attestations.

The operational worktree's full `ibc-tamoss-public.yaml` carries the same UI pin.
For rollback, restore its UI tag to `sha-5586c17@sha256:ffdc185decf951c7ce3f784cdbabebe560bd49e7a07766f1d2e882aacb1f3f6e`
and apply that parent, or use `ui-playback-rollback.patch.yaml` with the patch
command above and restore the full parent file before any subsequent apply.
Do not restore databases or change private/backend images for a UI rollback.

## Qualification

- All 220 frontend tests, lint, typecheck, entrypoint checks and production build pass.
- Twenty cold starts: five per browser for portrait and Reporter 1080p landscape,
  alternating desktop/mobile viewports. Full duration, every media object, decoded
  non-silent PCM, zero dropped frames and no measured post-start stalls.
- Chromium at 8 Mbps / 120 ms: portrait completes without post-start stalls.
  Firefox uses real object-delay tests, not CDP bandwidth emulation.
- Chromium/Firefox: 8-second initial audio delay, 12-second runtime audio/video
  delays, and explicit pause during recovery pass. Runtime delays cause a
  coordinated hold, not uninterrupted playback; no media is skipped.
- Split TS, fMP4 with init objects and muxed TS flash/tone fixtures all measure
  within 60 ms A/V alignment. Five additional muxed cold starts per browser pass
  with worst measured alignment of 56 ms. Fixtures are generated locally, not uploaded.
- Production build tested under the real public CSP before deployment.
- Repeated object reads: 24 local and 24 GKE, all successful; first-byte p95
  125 ms / 78 ms respectively. This does not rule out slower cold storage reads.

See [test instructions](tests/README.md) for controls and the invalid older
synthetic fixture exclusion. Native Omakase pause alignment can retry a sub-frame
target once and re-decode a GOP; the adapter does not perform correction seeks.

Raw sanitized evidence is retained in the operational worktree at
`.local/ibc/continuity-verification/`. Headless decoded PCM is not a substitute
for final listening and interaction in Zen/Firefox. Full tag-only release gates,
broader partial-object conformance and backup readiness remain separate release
qualifications; this is not a claim of complete BBC TAMS 8.2 conformity.

## Live verification

Deployed on 2026-09-08 at 16:29 UTC. Public parent generation 8 is Ready;
UI deployment generation 6 has two Ready replicas with zero restarts.
Both pods report the verified index and amd64 configuration digests.
Private, public API/worker/Console/gateway and shared operator generations,
images and replica counts match the saved baseline. Both full parent YAML
diffs against the cluster are empty.

All eight live playback runs pass: portrait and Reporter, Chromium and Firefox,
desktop and mobile, complete duration and objects, non-silent decoded audio,
zero dropped frames and no measured post-start stalls. Startup was 2.4-4.5 seconds.
Both browsers also recover from injected storage failures through the explicit
Retry playback button without a page refresh or uncaught browser errors.
Four logo/subtitle/portrait-layout checks, 20 protected-navigation cases and
18 HTTP access/CORS checks pass. Write-rejection probes used a nonexistent
random Flow ID; no stored recording was changed.
