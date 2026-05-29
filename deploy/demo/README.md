# Demo Media Fixture

`tamoss-demo.ts` is a tiny one-second H.264 MPEG-TS segment used by
`task kind:up` to prove the deployed TAMOSS API and object-storage ingest path.

It was derived from `tests/fixtures/e2e/tiny-ingest.mp4` and pre-segmented once
so the default install path does not require local `ffmpeg`, browser
ffmpeg.wasm, or network media downloads.

This file is for local smoke/demo use only. It is not representative production
media.

Expected probe-derived metadata:

- Flow `timerange`: `[0:0_1:0)`
- Object `object_timerange`: `[1:600000000_2:600000000)`
- Segment `ts_offset`: `-1:600000000`
- Segment `last_duration`: `0:100000000`
- Segment `key_frame_count`: `1`

Run `task test:media:fixtures` to validate the fixture through containerized
`ffprobe`.
