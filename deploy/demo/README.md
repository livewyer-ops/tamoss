# Demo Media Fixture

`tamoss-demo.ts` and `tamoss-demo-audio.ts` are tiny one-second H.264 and AAC
MPEG-TS segments used by `task kind:up` to prove the deployed TAMOSS API,
object-storage ingest path and split video/audio preview.

It was derived from `tests/fixtures/e2e/tiny-ingest.mp4` and pre-segmented once
so the default install path does not require local `ffmpeg`, browser
ffmpeg.wasm, or network media downloads.

These files are for local smoke/demo use only. They are not representative
production media.

Expected probe-derived metadata:

- Flow `timerange`: `[0:0_1:0)`
- Object `object_timerange`: `[1:600000000_2:600000000)`
- Segment `ts_offset`: `-1:600000000`
- Segment `last_duration`: `0:100000000`
- Segment `key_frame_count`: `1`

The audio fixture is a generated 1 kHz mono tone. It has a 48 kHz sample rate,
48 AAC frames and a probe-derived Object timerange of
`[1:400000000_2:424000000)`.

Run `task test:media:fixtures` to validate the fixture through containerized
`ffprobe`.
