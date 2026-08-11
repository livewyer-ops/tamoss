# 0004: Omakase Preview Adapter

## Status

- Omakase is accepted as the default read-only preview engine for 8.2.
- A dependency-free lazy adapter boundary exists, but preview is deliberately
  unavailable and unlinked until a releasable package set passes these gates.
- Package compatibility, security findings, and media/accessibility evidence
  are unresolved release gates.

## Context

TAMOSS needs operational inspection of TAMS media, not timeline editing. The
current UI constructs HLS and synchronises audio in browser-owned code.
Omakase already provides media playback, TAMS integration, sidecar tracks,
subtitles, frame-accurate seeking, markers, waveforms, and timeline
visualisations.

As checked on 9 August 2026, the published TAMS and React adapters require
Omakase core `0.25.4`, while the current core package is `1.1.1`. Installation
with ignored peer dependencies is not an accepted resolution.

A clean install of core 1.1.1 measured 112 production packages and a lazy
player chunk of approximately 3.71 MiB minified or 961 KiB gzip, excluding
`hls.js`. `npm audit --omit=dev` reported five moderate findings and one high
finding through subtitle/XML dependencies. Sourcemap inspection showed those
parsers compiled into the distributed ES module, so an application-level
override does not demonstrate remediation. These measurements explain why the
adapter exists without installing the player yet.

## Accepted Decisions

The application owns a `MediaPreview` interface and one Omakase adapter. Page
components depend on that interface, not Omakase classes. The adapter is
imperative at its boundary so the React-specific Omakase package is optional,
not architectural.

The player and its CSS load only after a preview route is entered. Disposing or
changing a preview tears down subscriptions, network requests, object URLs,
workers, and media elements before creating the next instance.

The preview supports:

- single video, audio, image, and data Flow inspection;
- Multi-Flow video with independently selectable audio and subtitle Flows;
- segment availability, marker, thumbnail, waveform, level, and caption lanes
  when the data exists;
- TAMS timecode and timerange display; and
- technical metadata and actionable playback errors.

It does not expose trim, splice, marker mutation, segment mutation, mixing,
export, or other nonlinear editing controls.

## Adapter Contract

The page gives the adapter a normalised descriptor, not raw API responses:

```ts
interface MediaPreviewDescriptor {
  rootFlowId: string;
  video?: PreviewTrack;
  audio: PreviewTrack[];
  subtitles: PreviewTrack[];
  markers: PreviewMarker[];
  initialTimerange?: string;
}
```

The descriptor builder owns collection traversal, role interpretation, Segment
pagination, storage preference, and URL renewal. It returns bounded timerange
windows instead of loading every Segment in a long Flow. Omakase owns display
and playback only; it does not call arbitrary TAMS endpoints.

The adapter reports typed `loading`, `ready`, `buffering`, `ended`, and `error`
states and exposes selected tracks and current TAMS timestamp. Page code does
not inspect Omakase internals to infer state.

## Credential and URL Rules

- Fetch same-origin TAMS media with `credentials: "same-origin"` only.
- Fetch presigned or external media with `credentials: "omit"`; never attach a
  TAMS bearer token, Authentik cookie, or forward-auth header.
- Accept HTTPS media URLs. Plain HTTP is limited to the configured same-origin
  `local-kind` development route.
- Remove URL userinfo and reject non-HTTP schemes.
- Do not persist or log presigned URLs. Errors, telemetry, browser history, and
  copy controls use storage labels and object identifiers without URL queries.
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

All `@byomakase/*` packages are exact-pinned and upgraded together behind the
adapter test suite. Accepted compatibility outcomes, in preference order, are:

1. an upstream TAMS adapter compatible with the current core;
2. a small TAMOSS descriptor adapter against the current core; or
3. a narrow, documented temporary fork with an owner and removal condition.

Do not use `--legacy-peer-deps`, silently pin an obsolete vulnerable graph, or
fork the full player without a maintenance plan.

## 8.2 Release Gates

- Resolve the core/TAMS adapter peer contract and record exact selected
  versions, licences, package sizes, and reachable security findings.
- Test MP4 and HLS, single and Multi-Flow media, separate video/audio,
  audio-only, subtitles, discontinuities, gaps, and long Segment lists.
- Test signed URL renewal, byte ranges, CORS, storage failover, and prove no
  credential is sent to an external or presigned origin.
- Pass Chromium, Firefox, and WebKit automated playback checks, with a real
  Safari check for separate audio and visualisation routing.
- Pass keyboard, focus, screen-reader, captions, contrast, zoom, and reduced
  motion checks, including the non-canvas track/marker view.
- Verify route lazy-loading and cleanup with repeated open, switch, and close
  cycles.
- Validate Kind playback through `192.168.122.103` and Authentik, TLS, CORS,
  CSP, and signed URLs on `cnm-tamoss-1`.

## References

- [Omakase technical specification](https://player.byomakase.org/technical-specification)
- [Omakase API documentation](https://api.player.byomakase.org/interfaces/OmakasePlayerApi.html)
