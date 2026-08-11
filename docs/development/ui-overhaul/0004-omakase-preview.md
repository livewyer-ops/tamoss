# 0004: Omakase Preview Adapter

## Status

- Omakase is accepted as the default read-only preview engine for 8.2.
- The prototype uses exactly `@byomakase/omakase-player` `1.1.1` behind the
  lazy preview route and a bounded TAMOSS-owned descriptor adapter.
- Split HLS audio uses the exactly pinned `hls.js` `1.6.17` already present in
  Omakase's dependency graph, behind the same lazy route.
- The published Omakase TAMS and React wrappers are deliberately not installed.
- Prototype availability does not make the player releasable. Security,
  licensing, media, browser, and accessibility evidence remain 8.2 release
  gates.

## Context

TAMOSS needs operational inspection of TAMS media, not timeline editing. The
current UI constructs HLS and synchronises audio in browser-owned code.
Omakase already provides media playback, TAMS integration, sidecar tracks,
subtitles, frame-accurate seeking, markers, waveforms, and timeline
visualisations.

As checked on 9 August 2026, `@byomakase/omakase-tams-player` `1.0.6` and
`@byomakase/omakase-react-components` `1.4.2` both require Omakase core
`0.25.4`, while the selected core is `1.1.1`. The wrappers also publish type
declarations that deep-import paths absent from core 1.1.1. Installing that
combination with ignored peer dependencies is not accepted.

The reviewed core artefact has npm integrity
`sha512-Bc5Md7N3hpeSBeTJgjg1/qNeUmm2MNmSv2cgxmrOoTzXYjoySjczlfRZQG0Rwyz+qarYTcwMCqt9yvLOhGapHA==`,
contains seven published files, and is 15,635,122 bytes unpacked. A clean
fixture added 112 packages; npm classified 111 as production dependencies. A
minimal Vite 8 build measured 3,714.54 kB minified / 967.90 kB gzip of
JavaScript and 897.96 kB / 375.80 kB gzip of required CSS. That is about
1.34 MB gzip (1.28 MiB) before TAMOSS adapter code. The integrated TAMOSS build
emitted 3,768.21 kB / 977.59 kB gzip of preview JavaScript and 900.70 kB /
376.82 kB gzip of preview CSS; the manifest gate measured 1,314.90 KiB gzip
across the five files exclusive to the lazy preview graph.

In the TAMOSS graph, `npm audit --omit=dev` currently reports three moderate
entries for one chain: Omakase -> `subtitle-converter` -> `xml2js` `<0.5.0`
([GHSA-776f-qx25-q3cc](https://github.com/advisories/GHSA-776f-qx25-q3cc)).
The repository's `js-yaml` override removes the separately installed
`js-yaml` findings from npm's report. It does not rewrite Omakase's published
ES module: sourcemaps and bundle inspection show the legacy XML/subtitle code
was prebundled upstream. The override must not be represented as remediation
for those embedded bytes.

## Accepted Decisions

The application owns a `MediaPreview` interface and one imperative Omakase
adapter. Page components depend on that boundary, not Omakase classes. TAMOSS
does not use the published React or TAMS wrappers.

The player and its CSS load only after a preview route is entered. Disposing or
changing a preview tears down subscriptions, network requests, object URLs,
workers, and media elements before creating the next instance.

The prototype currently:

- builds a bounded descriptor from TAMS Flow and Segment data;
- plays supported video, audio, and Multi-Flow windows through a browser-local
  playback plan;
- gives audio-only HLS an operational 25 fps timeline timebase; this is a UI
  display model and is not written back as audio essence metadata;
- constrains split video/audio playback to their shared timerange, plays the
  primary video through Omakase, and synchronises selectable audio renditions
  through hidden, non-focusable HLS media elements;
- displays the selected timerange, track inventory, Segment coverage, and
  actionable playback state; and
- creates the Omakase player and timeline only after the preview route loads.

Subtitle and caption conversion is disabled and is not implemented by the
TAMOSS adapter. Subtitle tracks may appear in the operational inventory, but
the prototype does not pass their content to Omakase's subtitle APIs. Markers,
thumbnails, waveforms, level visualisations, signed URL renewal, and storage
failover remain target capabilities rather than completed prototype behaviour.

It does not expose trim, splice, marker mutation, segment mutation, mixing,
export, or other nonlinear editing controls.

## Adapter Contract

The page gives the adapter a normalised descriptor, not raw API responses:

```ts
interface MediaPreviewDescriptor {
  rootFlow: Flow;
  tracks: PreviewTrack[];
  video?: PreviewTrack;
  audio: PreviewTrack[];
  images: PreviewTrack[];
  data: PreviewTrack[];
  muxed?: PreviewTrack;
  initialTimerange: string;
  segmentCount: number;
  truncated: boolean;
}
```

The descriptor builder owns collection traversal, format classification,
Segment limits, URL policy, and the selected playback window. The prototype
uses the first allowed server-ordered presigned location and marks a window
truncated when another Segment page exists; storage preference and signed URL
renewal remain work at this boundary. Omakase owns display and playback only;
it does not call arbitrary TAMS endpoints.

The adapter reports typed `loading`, `ready`, `playing`, `paused`, `buffering`,
`ended`, and `error` states and exposes the selected tracks, relative playback
position, duration, and selected TAMS timerange. Page code does not inspect
Omakase internals to infer state.

Omakase 1.1.1's public sidecar API cannot play the segmented HLS audio media
playlist used by the Kind fixture in Chromium, and a master playlist spanning
video time with no matching early audio stalls while waiting for its declared
rendition. The accepted adapter therefore keeps a standards-correct master as
a tested playback artefact, loads a video-only media playlist into Omakase,
and maps each separate audio clock to the common video window. A sidecar must
reach both HLS manifest and media readiness within 15 seconds; any declared
audio rendition failure fails the preview rather than silently continuing
video-only.

## Credential and URL Rules

- The prototype requests and accepts only cross-origin presigned Segment URLs,
  which the player fetches without TAMS credentials. Same-origin presigned
  media is rejected because a native media request can attach ambient cookies.
  It never gives Omakase a TAMS bearer token, Authentik cookie, or forward-auth
  header.
- Supporting non-presigned same-origin media later requires an explicit
  credential-aware adapter path and browser tests; it must not be inferred from
  URL origin inside Omakase.
- Accept HTTPS media URLs. Plain HTTP is limited to the current origin or
  browser loopback hosts used by local development.
- Reject URL userinfo, fragments, control characters, and non-HTTP schemes.
- Do not persist or log presigned URLs. Errors, telemetry, browser history, and
  copy controls use storage labels and object identifiers without URL queries.
- Core 1.1.1 logs raw HLS error objects. The pinned prototype installs a
  lifetime-scoped console error guard keyed to its in-memory signed URLs before
  constructing Omakase and restores the console after teardown. This blocks
  the reviewed logging path but does not replace the upstream-release or fork
  gate for a future package version.
- No UI surface renders URL userinfo or query data, and no screen mints a
  signed URL before a bounded playback request needs it.
- Treat expiry as data: renew a descriptor before expiry and retry once after
  an expiry-class playback failure without changing the current timestamp.
- Generate a narrow `media-src` and `connect-src` policy from configured
  storage origins. Wildcard origins are not accepted.

Storage endpoints must support browser CORS, byte ranges, and the required
methods and headers. A preview failure shows the failed track, storage label,
time range, and a retry action; it does not reveal credentials or silently
drop audio.

## Accessibility Contract

The canvas timeline is supplementary. The DOM provides:

- named play, pause, seek, mute, volume, track, captions, and fullscreen
  controls with visible focus;
- keyboard operation without focus traps;
- current time and duration without announcing every frame;
- a table/list alternative for tracks, markers, and Segment gaps;
- caption selection and an available transcript/text view; and
- reduced-motion behaviour for animated indicators.

Automated accessibility tests are necessary but not sufficient. A keyboard
and screen-reader pass is part of the release gate.

## Version and Security Policy

Only `@byomakase/omakase-player` is installed from the `@byomakase` scope,
exactly pinned to `1.1.1`. `hls.js` is also a direct exact pin at `1.6.17` so
the split-audio boundary does not float independently of the reviewed player
graph. The build verifies both npm integrities, that no official wrapper is
added, that the player is absent from the initial static graph, and that the
exclusive lazy preview JavaScript and CSS remain within a 1.4 MiB gzip budget.

Accepted security outcomes, in preference order, are:

1. an upstream core release that removes or updates the legacy parser stack;
2. a documented reachability decision backed by tests that prove untrusted
   media cannot invoke the affected code; or
3. a narrow, documented temporary fork from tag `1.1.1`, rebuilt from source
   with an owner and removal condition.

Do not use `--legacy-peer-deps`, silently pin an obsolete vulnerable graph, or
patch the minified distribution. Package-manager overrides do not remediate
dependencies already compiled into the distributed player.

## 8.2 Release Gates

### Recorded evidence

The local Kind fixture at `192.168.122.103` has passed a deployed Chrome stable
146 run through Authentik and real TLS for split video/audio and audio-only HLS.
The browser test also verifies successful CORS and byte-range responses, no
Bearer or Cookie header on requests to the configured S3 origin, no signed URL
in the DOM or captured console/page errors, and active-playback route teardown.
The deployed test discovers the current split fixture rather than depending on
a hard-coded Flow identifier, and its browser executable can be overridden with
`TAMOSS_E2E_BROWSER_EXECUTABLE` for compatibility runs. When no override is
set, the harness uses an installed Google Chrome stable binary and otherwise
falls back to Playwright Chromium; setting the variable to an empty value
forces that fallback.

This does not close the browser gate. The Chromium 149 binary bundled with the
current Playwright install fails audio-only readiness and has crashed its
renderer during active-playback SPA teardown, while Chrome stable 146 passes
the same test. Firefox, WebKit, real Safari, MP4, renewal/failover, subtitles,
accessibility, and `cnm-tamoss-1` evidence are still outstanding.

- Keep core exactly pinned to `1.1.1` and `hls.js` to `1.6.17`, retain both npm
  integrity checks, generate an SBOM, and ship the checked-in Apache-2.0 and
  MPL-2.0 third-party notices.
- Resolve or formally accept the three moderate audit entries, and separately
  account for vulnerable dependency code prebundled into the reviewed artefact.
  No subtitle conversion may be enabled before this evidence is approved.
- Test MP4 and HLS, single and Multi-Flow media, separate video/audio,
  audio-only, subtitles, discontinuities, gaps, and long Segment lists.
- Test signed URL renewal, byte ranges, CORS, storage failover, and prove no
  credential is sent to an external or presigned origin.
- Pass Chromium, Firefox, and WebKit automated playback checks, with a real
  Safari check for separate audio and visualisation routing.
- Pass keyboard, focus, screen-reader, captions, contrast, zoom, and reduced
  motion checks, including the non-canvas track/marker view.
- Verify route lazy-loading and cleanup with repeated open, switch, and close
  cycles. Overview and catalog routes must load no Omakase JavaScript or CSS;
  the combined exclusive preview graph must remain at or below 1.4 MiB gzip.
- Validate Kind playback through `192.168.122.103` and Authentik, TLS, CORS,
  CSP, and signed URLs on `cnm-tamoss-1`.

The 8.2 UI must not be released with the prototype merely enabled. Every gate
above requires recorded evidence or an explicit, owned risk acceptance.

## References

- [Omakase technical specification](https://player.byomakase.org/technical-specification)
- [Omakase API documentation](https://api.player.byomakase.org/interfaces/OmakasePlayerApi.html)
