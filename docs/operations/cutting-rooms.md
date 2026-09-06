# Cutting Rooms Acceptance and Recovery

The 8.2 release gate covers Reporter ingestion, TAMOSS persistence, webhook
delivery, CR catalogue visibility and actual media playback. Do not treat
these as interchangeable success signals.

## Acceptance Record

Run with a designated test account and clearly labelled test media. Retain a
restricted-access record containing:

- UTC start/end, tester, TAMOSS release/source SHA and all deployed digests,
  BBC TAMS revision, schema revision, Reporter version and CR version.
- TAMOSS instance, namespace, API URL and CR application URL. The pilot is
  expected to use `cr-tamoss-1`; verify the deployed CR and ingress first.
- Source, parent collection, child Flow, media Object and webhook IDs.
- Callback and query evidence, with authorisation headers, query credentials,
  webhook keys, payload media URLs and cookies removed.
- Each check below marked **pass**, **fail** or **not run**, with an artefact
  reference. All must pass before stable-release sign-off.

## Checks

1. Register the intended webhook before ingest. Record its event list and
   every selector. In 8.2, absent `flow_collected_by_ids` means no collection
   restriction; `[]` selects only top-level Flows. The equivalent distinction
   applies to Sources. Check IDs refer to the right resource type.
2. Ingest a short, uniquely labelled audiovisual clip through Reporter. Record
   its successful upload, segment-registration and metadata responses. Read
   those same IDs back from the TAMOSS API, including collection relationships,
   Profiles, Flow status, segment timeranges and initialisation Objects.
3. Observe the receiver accepting `sources/created`, `flows/created`, relevant
   metadata updates and `flows/segments_added`. Match event IDs/resource IDs
   and timestamps to the ingest record. Check callback authentication without
   logging its key. A receiver HTTP 2xx proves acceptance only, not indexing.
4. Capture the CR API queries for the same clip. Compare endpoint, scopes,
   `source_id`, collection selectors, tags, status/Profile filters,
   `include_timerange`, and URL selection options. Follow each returned
   `Link: rel=next`; never manufacture page tokens or discard filters.
5. Confirm the clip appears in the CR catalogue and open it in CR. Play video
   and audio, seek across a segment boundary, and verify duration and initial
   segment handling. Read fresh object URLs if old signed URLs have expired.
6. On a test receiver or isolated CR test integration, return a retryable 503
   for one callback and restore service before retry exhaustion. Confirm a
   retry is accepted and CR sees no duplicate clip or segments. Do not take
   the live CR receiver offline for this test.
7. Exercise a terminal/disabled webhook and the recovery procedure below in
   the test integration. Confirm CR reconciles changes made while delivery
   was inactive. Re-enabling alone is not sufficient.
8. Clean up only the test resources by their recorded IDs. Capture the
   expected delete events and confirm the CR catalogue reflects deletion.

If CR-side credentials or playback access are unavailable, this gate remains
**not run** even when all TAMOSS-local tests pass.

## Diagnose Delivery

The Webhooks page shows actual selector values, including collection filters
and signed-URL selection. For delivery history, run the read-only diagnostic
from the repository with the target's existing `POSTGRES_*` configuration:

```bash
umask 077
uv run --project src python scripts/webhook_diagnostics.py \
  --webhook-id 00000000-0000-4000-8000-000000000001 \
  > webhook-diagnostics.json
```

Use a read-only database identity where available. The script also enforces a
read-only, repeatable-read transaction and a five-second statement timeout.
It reports pending, in-flight, done and dead counts; expired claims; oldest
pending time; last successful delivery and last attempt-related activity;
and at most 20 recent deliveries with HTTP status and error type. It does
not emit webhook keys, full callback URLs, payloads or raw error messages.

These are **retained** rows, not lifetime totals. Completed/dead rows are
purged according to `TAMOSS_WORKER_QUEUE_RETENTION_SECONDS` (seven days by
default). A null success timestamp after retention is not proof that the
receiver has never worked. `last_attempt_activity_at` is the last recorded
attempt state update, not a precise HTTP request start timestamp.

Interpret the boundary before changing configuration:

- No queued event: inspect registration time, active status and selectors.
- Pending/expired claim: inspect worker readiness, queue age and leases.
- 401/403: compare callback authentication with the receiver's expected key.
- Target blocked: inspect DNS/egress policy. Default delivery pins the validated
  destination address and ignores ambient HTTP proxies and netrc credentials.
  Do not enable all private destinations to work around an unexplained error.
- 2xx but absent in CR: inspect the receiver's indexing result and CR queries.
- Visible but unplayable: inspect fresh GET URLs, CORS, object availability,
  Profile/initialisation metadata and segment timeranges.

## Recover and Reconcile

1. Preserve the diagnostic report and record the earliest suspected gap. Fix
   the receiver, credentials or query/filter mismatch first.
2. Keep the same webhook ID where possible. With a write-authorised API token,
   PUT the full registration to `/service/webhooks/{webhookId}` with
   `status: "created"` and the intended filters and credentials. Use the
   original secret source, not redacted GET output. Forward-auth UI access is
   read-only. Verify a new test event reaches CR.
3. Reconcile CR against the current TAMOSS catalogue and segment listings,
   including collections and deletions. Follow opaque pagination links;
   upsert by Source/Flow/Object identity. Use fresh media URLs. The receiver
   must tolerate duplicate events: delivery is at least once, not exactly once.
4. Recheck resources changed during reconciliation and compare CR counts and
   IDs, then repeat catalogue and playback checks. For a definitive snapshot,
   quiesce test writers or perform repeated reconciliation until stable.

Reactivation does not replay dead rows or reconstruct events never queued
while the registration was disabled/in error. Do not mass-update queue rows
to `pending`: historical payload URLs may have expired, routing/filter
configuration may have changed, and old events can overwrite newer state.
There is deliberately no one-click historical replay in this release.

Flow/Source lists now issue keyset cursors ordered by the requested sort value
and ID. Deleting an earlier page or its anchor no longer skips surviving
records. Numeric tokens remain accepted for transition, but clients must use
newly issued opaque tokens. These are live listings, not snapshots: changing
a label or update timestamp can move an item. Prefer `sort_by=created` for
catalogue reconciliation and reconcile concurrent changes separately.
