# IngestRun CR Reference

`IngestRun` declares one durable attempt to ingest media into a `Tamoss`
instance. The operator validates its immutable input against the instance's
source policy and owns the resulting
[TAMSin v1.0.0-rc.3](https://github.com/livewyer-ops/tamsin/releases/tag/v1.0.0-rc.3)
Kubernetes Job.

Group: `tamoss.livewyer.io`

Version: `v1alpha1`

Kind: `IngestRun`

Scope: `Namespaced`

Short name: `ir`

## Public HTTPS Example

The `local-kind` profile defaults to `PublicHTTPS`. Other profiles default to
`Disabled` and must opt in explicitly:

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: Tamoss
metadata:
  name: tamoss-media
  namespace: tams
spec:
  profile: single-server
  ingest:
    sourcePolicy:
      mode: PublicHTTPS
```

An authorised Kubernetes client can then create a run for any public HTTPS
asset on port 443. The asset does not have to be added to the `Tamoss` first:

```yaml
apiVersion: tamoss.livewyer.io/v1alpha1
kind: IngestRun
metadata:
  name: example-programme-001
  namespace: tams
spec:
  tamossRef:
    name: tamoss-media
  input:
    kind: HTTP
    uri: https://media.example.com/programmes/example.mp4
  profile: essence-segments@1
  sizeClass: standard
  options:
    verify: true
    maxInputs: 1
  output:
    flowMetadata:
      label: Example programme
      description: Programme ingest requested by media operations
      tags:
        editorial_purpose:
          - programme
  desiredState: Running
```

Public selectors must use HTTPS port 443 and cannot contain user information,
query strings, or fragments. The operator resolves and validates the selector
immediately before creating the Job.

## Restricted HTTP Source Example

Use `Restricted` when every run must name an operator-approved source. A source
is a reusable trust boundary, not an asset record:

```yaml
spec:
  ingest:
    sourcePolicy:
      mode: Restricted
    sources:
      - name: review-library
        kind: HTTP
        http:
          origin: https://media.internal.example.com
          pathPrefixes:
            - /approved/
          allowPrivateAddresses: true
          credentialSecretRef:
            name: review-library-http
```

The HTTP credential Secret contains a JSON array accepted by TAMSin's
[`source.http_headers`](https://github.com/livewyer-ops/tamsin/blob/v1.0.0-rc.3/docs/reference/configuration.md)
setting:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: review-library-http
  namespace: tams
type: Opaque
stringData:
  TAMSIN_SOURCE_HTTP_HEADERS: '["Authorization: Bearer replace-me"]'
```

The run selects the source but cannot select a Secret:

```yaml
spec:
  tamossRef:
    name: tamoss-media
  input:
    kind: HTTP
    uri: https://media.internal.example.com/approved/example.mp4
    sourceRef:
      name: review-library
```

## Restricted S3 Source Example

S3 inputs always require a named source:

```yaml
spec:
  ingest:
    sourcePolicy:
      mode: Restricted
    sources:
      - name: archive
        kind: S3
        s3:
          endpoint: https://objects.example.com
          region: eu-west-2
          bucket: archive
          keyPrefixes:
            - incoming/
          pathStyle: true
          credentialSecretRef:
            name: archive-reader
```

The optional Secret uses the standard AWS keys `AWS_ACCESS_KEY_ID` and
`AWS_SECRET_ACCESS_KEY`; `AWS_SESSION_TOKEN` is optional. A run can select one
object or a bounded prefix:

```yaml
spec:
  tamossRef:
    name: tamoss-media
  input:
    kind: S3
    uri: s3://archive/incoming/day-1/
    sourceRef:
      name: archive
```

See [TAMSin input reference](https://github.com/livewyer-ops/tamsin/blob/v1.0.0-rc.3/docs/reference/inputs.md)
for selector expansion behaviour.

## IngestRun Spec Fields

| Field | Purpose |
| --- | --- |
| `.spec.tamossRef.name` | Target `Tamoss` in the same namespace. Required and immutable. |
| `.spec.input.kind` | `HTTP` or `S3`. Required and immutable. |
| `.spec.input.uri` | One HTTPS URL, S3 object, or bounded S3 prefix. Required, immutable, and limited to 2,048 characters. User information, queries, and fragments are forbidden. |
| `.spec.input.sourceRef.name` | Named source on the target `Tamoss`. Required in `Restricted` mode and for every S3 input. |
| `.spec.profile` | Versioned TAMSin treatment: `preserve@1`, `demux@1`, `muxed-segments@1`, `essence-segments@1`, or `mpegts-segments@1`. Defaults to `essence-segments@1`; immutable. |
| `.spec.sizeClass` | Operator-owned resource class: `small`, `standard`, or `large`. Defaults to `standard`; immutable. |
| `.spec.options.storageBackendRef.name` | Optional Ready, media-purpose `StorageBackend` belonging to the target instance. Unset uses the default TAMS backend. |
| `.spec.options.verify` | Verify uploaded Object bytes. Defaults to `true`, which selects TAMSin `auto`; `false` selects `none`. |
| `.spec.options.dryRun` | Render and plan without changing TAMS. Defaults to `false`; `true` selects TAMSin `exact`. |
| `.spec.options.maxInputs` | Maximum S3 prefix expansion, from 1 to 10,000. Defaults to 1,000. |
| `.spec.options.concurrency` | Parallel inputs, from 0 to 32. Zero selects the size-class default. |
| `.spec.options.tamsFlowProfiles` | Optional TAMS Flow Profile assignments. Each item selects an essence `format`, zero-based stream `index`, and exactly one of `profileRef` or `profileID`. Format and index pairs must be unique. |
| `.spec.output.flowMetadata.label` | Optional human-readable label, from 1 to 256 characters, applied by TAMSin to every Flow generated from the input. Requires `.spec.options.maxInputs: 1`; immutable. |
| `.spec.output.flowMetadata.description` | Optional description, from 1 to 4,096 characters, applied by TAMSin to every Flow generated from the input. Requires `.spec.options.maxInputs: 1`; immutable. |
| `.spec.output.flowMetadata.tags` | Up to 32 TAMS tags whose values are a string or an array of strings. TAMSin merges them into every Flow generated from the input. Requires `.spec.options.maxInputs: 1`; immutable. Names beginning with `_tamsin_`, in any letter case, are reserved. |
| `.spec.desiredState` | `Running` or `Cancelled`. Defaults to `Running`; cancellation is a one-way transition. |
| `.spec.retryOf.name` | Name of a terminal parent run. |
| `.spec.retryOf.uid` | Exact parent UID, preventing a replacement object with the same name from becoming the retry source. |

All spec fields except the one-way `desiredState` transition are immutable. A
retry must preserve the parent's target, input, profile, size class, options,
and output intent.

`spec.output` is a curated metadata contract, not an arbitrary TAMSin argument
surface. Its metadata applies to the complete generated Flow graph. It cannot
set Flow IDs, Source IDs, `created_by`, generation,
read-only state, codec, container, essence parameters, Segment duration,
FFmpeg arguments, or other technical metadata. TAMOSS renders the accepted
fields through TAMSin's `--flow-metadata` option.

Assign operator-managed Flow Profiles by essence format and stream index:

```yaml
spec:
  options:
    tamsFlowProfiles:
      - format: video
        index: 0
        profileRef:
          name: hd-avc
      - format: audio
        index: 0
        profileRef:
          name: stereo-aac
```

Each reference must identify a current `Ready` `FlowProfile` in the same
namespace, associated with the same `Tamoss`, and matching the selected essence
format. The operator resolves it to a UUID before creating the TAMSin Job and
records both forms in `.status.resolvedTamsFlowProfiles`.

Use a raw UUID for a Profile managed outside Kubernetes:

```yaml
spec:
  options:
    tamsFlowProfiles:
      - format: audio
        index: 0
        profileID: 73b13cf7-719a-448d-9852-7c4d5e1bb522
```

TAMOSS renders resolved assignments as TAMSin `--tams-flow-profile` arguments.
The run never creates, updates, or deletes Profiles. See
[FlowProfile CR](flowprofile-cr.md) for operator-managed registration.

## Tamoss Source Policy Fields

| Field | Purpose |
| --- | --- |
| `.spec.ingest.sourcePolicy.mode` | `Disabled`, `PublicHTTPS`, or `Restricted`. Omitted is effectively `Disabled`, except `local-kind` defaults to `PublicHTTPS`. |
| `.spec.ingest.sources` | Up to 32 named HTTP or S3 source boundaries. Names must be unique. |
| `.spec.ingest.sources[].name` | DNS-label source name referenced by a run. |
| `.spec.ingest.sources[].kind` | `HTTP` or `S3`; exactly the matching settings object must be present. |
| `.spec.ingest.sources[].http.origin` | Exact HTTPS origin. Paths, user information, queries, and fragments are forbidden. |
| `.spec.ingest.sources[].http.pathPrefixes` | Optional permitted URL path prefixes. |
| `.spec.ingest.sources[].http.allowPrivateAddresses` | Explicitly permits private, loopback, or link-local resolved addresses for this named source. |
| `.spec.ingest.sources[].http.credentialSecretRef.name` | Optional source-owned HTTP header Secret. |
| `.spec.ingest.sources[].s3.endpoint` | Exact HTTPS S3-compatible endpoint. |
| `.spec.ingest.sources[].s3.region` | AWS region supplied to the S3 client. |
| `.spec.ingest.sources[].s3.bucket` | Exact permitted bucket. |
| `.spec.ingest.sources[].s3.keyPrefixes` | Optional permitted object-key prefixes. |
| `.spec.ingest.sources[].s3.pathStyle` | Enables path-style S3 requests. |
| `.spec.ingest.sources[].s3.allowPrivateAddresses` | Explicitly permits private resolved endpoint addresses. |
| `.spec.ingest.sources[].s3.credentialSecretRef.name` | Optional source-owned AWS credential Secret. |

Wildcard hosts and workload identity are not supported in this release.

## Status Fields

| Field | Purpose |
| --- | --- |
| `.status.observedGeneration` | Resource generation observed by the controller. |
| `.status.phase` | `Pending`, `Queued`, `Running`, `Succeeded`, `PartiallySucceeded`, `Failed`, or `Cancelled`. |
| `.status.conditions` | At most 16 Kubernetes conditions with bounded, stable reason codes. |
| `.status.jobRef` | Name and UID of the operator-owned TAMSin Job. |
| `.status.resolvedSource.name` | Named source used by the Job, or `public-https` for unnamed public input. |
| `.status.resolvedSource.policyDigest` | SHA-256 digest of the operator validation snapshot that admitted the Job. |
| `.status.resolvedTamsFlowProfiles` | Immutable format/index assignments with resolved Profile UUIDs and optional `FlowProfile` names. |
| `.status.tamsinRunId` | TAMSin attempt identity observed from its event stream. |
| `.status.attempt` | Attempt number, including validated retry lineage. |
| `.status.progress` | Bounded input totals, completion counts, failure counts, and uploaded bytes. |
| `.status.lastEventSequence` | Last accepted TAMSin event sequence. |
| `.status.resultRef` | Durable key, SHA-256 digest, size, media type, and verification state. The Console projection omits the private key. |
| `.status.output.rootFlowID` | Root Flow UUID reported by TAMSin for a completed single-input result. |
| `.status.output.sourceID` | Source UUID reported with the root Flow result. |
| `.status.output.memberFlows` | Up to 16 non-root Flow identities, formats, and roles reported by TAMSin. |
| `.status.output.memberFlowsTruncated` | True when the validated event stream contained more member Flows than fit in bounded Kubernetes status. Use TAMSin's durable result artefact for exhaustive bulk results. |
| `.status.startedAt`, `.status.completedAt` | Attempt start and completion timestamps. |

## Ownership and Access

The operator alone creates and mutates the TAMSin Job. A user-supplied
`IngestRun` cannot choose its image, command, environment, volumes, service
account, Pod security context, Secret, or destination backend credentials.
The operator passes only the admitted selector and matching source client
settings to TAMSin; it does not pass a general policy document. The policy
digest on the Job and status is an audit identity, not a second enforcement
mechanism.

IngestRun does not create a per-run `NetworkPolicy`; it uses the instance's
existing cluster networking. Source-policy enforcement is an admission check,
not a general Pod-egress firewall. DNS answers and HTTP redirect targets can
change after the operator's check and before TAMSin reads them. Environments
that require a hard destination boundary must enforce it outside `IngestRun`,
for example with cluster egress controls or an approved proxy.

The Console supports paginated list and detail reads plus operator-authorised
one-way cancellation. It deliberately provides no POST endpoint, create form,
or retry command. Create runs through `kubectl`, GitOps, or another explicitly
authorised Kubernetes client.

See [Ingest Runs](../concepts/ingest-runs.md) for the model and
[Manage Ingest Runs](../operations/manage-ingest-runs.md) for operational
commands. The canonical CRD under `operator/config/crd/bases/` is the
exhaustive schema source.
