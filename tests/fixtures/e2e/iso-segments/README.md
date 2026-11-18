# ISO Segment playback fixture

The playlist contains three one-second H.264 segments sharing `init.mp4`.
Video is 64 by 64 pixels at 10 frames per second, starting at 0.2 seconds.
The deployed playback test uses this non-zero start to check initial playback
and replay. `task test:media:fixtures` verifies the timing and media properties.

To recreate the fixture from `tiny-ingest.mp4`, run from the repository root:

```bash
docker build -f tests/media/Dockerfile -t tamoss-media-test:dev .
docker run --rm -v "$PWD/tests/fixtures/e2e:/fixtures" \
  -w /fixtures/iso-segments tamoss-media-test:dev \
  ffmpeg -y -v error -stream_loop 2 -i ../tiny-ingest.mp4 -c copy \
  -hls_time 1 -hls_segment_type fmp4 stream.m3u8
```
