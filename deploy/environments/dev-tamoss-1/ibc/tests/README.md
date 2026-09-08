# Playback qualification

These checks use the existing Omakase controls, not a muted native `video.play()` shortcut.
They require Playwright with Chromium/Firefox installed, Node.js, and a working audio
output backend (a private PulseAudio null sink is sufficient on a headless test host).
The sync fixture additionally requires FFmpeg/ffprobe. No fixture media is uploaded.

```sh
export NODE_PATH=/tmp/tamstool-playwright/node_modules
export IBC_UI_URL=http://127.0.0.1:5180
export IBC_EVIDENCE_DIR=/tmp/ibc-playback-evidence
node deploy/environments/dev-tamoss-1/ibc/tests/playback-continuity.cjs
node deploy/environments/dev-tamoss-1/ibc/tests/playback-sync.cjs
IBC_CONTAINER=fmp4 node deploy/environments/dev-tamoss-1/ibc/tests/playback-sync.cjs
IBC_CONTAINER=muxed-ts node deploy/environments/dev-tamoss-1/ibc/tests/playback-sync.cjs
```

Continuity defaults to five cold browser contexts per clip/browser, alternating
desktop and mobile. `IBC_REPEATS`, `IBC_BROWSERS` and `IBC_FIXTURE` select subsets.
Run both `IBC_CONTROL=toolbar` (default) and `IBC_CONTROL=surface`. Surface mode
uses desktop clicks and mobile taps on the video/centre overlay for play, pause
and replay; toolbar-only checks cannot establish that the centre control works.
`IBC_PROFILE=8mbps` uses Chromium CDP at 8 Mbps / 120 ms. `delay-audio`,
`delay-video` and `delay-audio-first` inject a single 12-second object delay or
an 8-second initial audio delay; use `IBC_FIXTURE=portrait IBC_REPEATS=1`.
`delay-audio-pause` also pauses during recovery, waits 12 seconds and resumes.
`IBC_REQUIRE_NO_STALLS=1` makes any measured post-start buffering a failure.
Omakase can align a native pause to the next video frame, retrying that same target
once. Tests permit only this bounded, sub-frame adjustment, not advancing recovery
seeks. Such a pause can re-decode a GOP without dropping or skipping media.

`IBC_DIST=/absolute/path/to/frontend/dist` overlays locally built UI files in the
test browser only. With `IBC_UI_URL=https://tamoss.live`, the main document retains
the deployed CSP and other response headers. This is pre-deployment qualification,
not evidence that those files have been deployed. No CSP bypass is used.

The generated split TS, fragmented MP4 and muxed TS fixtures contain a flash and
tone every second. Packet timestamps and timebases determine their TAMS metadata.
The check requires measured alignment within 100 ms throughout playback.

`object-timings.cjs` repeats direct reads of eight audio objects three times.
Set `KUBECONFIG` to include a GKE measurement. Reports omit signed URL queries.
Healthy repeated reads do not establish that cold object delivery is always healthy.

The older public synthetic flow `e92401b7-0bec-46c9-90eb-436590479579` is not a
valid timing control: its video PTS restarts at 1.48 seconds in successive Objects,
but Segment timeranges advance by three seconds without a corresponding ts_offset.
Under prebuffering, hls.js reports overlapping video fragments and only three seconds
of joint A/V coverage. Do not relax buffering or invent offsets to hide that defect.
The generated muxed fixture replaces it; no existing recording is rewritten.

Decoded PCM measurements and headless screenshots do not replace final listening
and interaction checks in the user's Zen/Firefox. Official release approval remains
separate from an internal candidate deployment.
