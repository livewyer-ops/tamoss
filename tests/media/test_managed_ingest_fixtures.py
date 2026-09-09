from __future__ import annotations

import json
import os
import shutil
import subprocess
from decimal import Decimal
from pathlib import Path
from typing import Any

import pytest

pytestmark = pytest.mark.needs_media

ROOT = Path(__file__).resolve().parents[2]
DEMO_SEGMENT = ROOT / "deploy/demo/tamoss-demo.ts"
DEMO_AUDIO_SEGMENT = ROOT / "deploy/demo/tamoss-demo-audio.ts"
NANOS_PER_SECOND = Decimal("1000000000")


def test_demo_fixture_media_timing_matches_registered_metadata() -> None:
    ffprobe = _ffprobe()
    stream_info = _probe_json(
        ffprobe,
        "-v",
        "error",
        "-select_streams",
        "v:0",
        "-count_frames",
        "-show_entries",
        "stream=codec_name,width,height,start_time,duration,avg_frame_rate,nb_read_frames",
        "-of",
        "json",
        str(DEMO_SEGMENT),
    )
    stream = stream_info["streams"][0]
    start_ns = _decimal_seconds_to_ns(stream["start_time"])
    duration_ns = _decimal_seconds_to_ns(stream["duration"])
    end_ns = start_ns + duration_ns

    assert stream["codec_name"] == "h264"
    assert stream["width"] == 64
    assert stream["height"] == 64
    assert stream["avg_frame_rate"] == "10/1"
    assert stream["nb_read_frames"] == "10"
    assert duration_ns == 1_000_000_000
    assert {
        "timerange": "[0:0_1:0)",
        "object_timerange": _timerange(start_ns, end_ns),
        "ts_offset": _timestamp(-start_ns),
        "last_duration": "0:100000000",
        "key_frame_count": _key_frame_count(ffprobe, DEMO_SEGMENT),
    } == {
        "timerange": "[0:0_1:0)",
        "object_timerange": "[1:600000000_2:600000000)",
        "ts_offset": "-1:600000000",
        "last_duration": "0:100000000",
        "key_frame_count": 1,
    }


def test_demo_audio_fixture_timing_matches_registered_metadata() -> None:
    ffprobe = _ffprobe()
    stream_info = _probe_json(
        ffprobe,
        "-v",
        "error",
        "-select_streams",
        "a:0",
        "-count_frames",
        "-show_entries",
        "stream=codec_name,sample_rate,channels,start_time,nb_read_frames",
        "-of",
        "json",
        str(DEMO_AUDIO_SEGMENT),
    )
    stream = stream_info["streams"][0]
    start_ns = _decimal_seconds_to_ns(stream["start_time"])
    frame_count = int(stream["nb_read_frames"])
    sample_rate = int(stream["sample_rate"])
    frame_duration_ns = 1024 * int(NANOS_PER_SECOND) // sample_rate
    end_ns = start_ns + frame_count * 1024 * int(NANOS_PER_SECOND) // sample_rate

    assert stream["codec_name"] == "aac"
    assert stream["channels"] == 1
    assert {
        "sample_rate": sample_rate,
        "frame_count": frame_count,
        "object_timerange": _timerange(start_ns, end_ns),
        "last_duration": _timestamp(frame_duration_ns),
    } == {
        "sample_rate": 48000,
        "frame_count": 48,
        "object_timerange": "[1:400000000_2:424000000)",
        "last_duration": "0:21333333",
    }


def _ffprobe() -> str:
    ffprobe = os.getenv("TAMOSS_FFPROBE_BIN") or shutil.which("ffprobe")
    if ffprobe is None:
        pytest.skip("ffprobe is unavailable; run task test:media:fixtures")
    return ffprobe


def _probe_json(ffprobe: str, *args: str) -> dict[str, Any]:
    result = subprocess.run(
        [ffprobe, *args],
        capture_output=True,
        check=True,
        text=True,
    )
    return json.loads(result.stdout)


def _key_frame_count(ffprobe: str, path: Path) -> int:
    frames = _probe_json(
        ffprobe,
        "-v",
        "error",
        "-select_streams",
        "v:0",
        "-show_frames",
        "-show_entries",
        "frame=key_frame",
        "-of",
        "json",
        str(path),
    )
    return sum(1 for frame in frames["frames"] if frame["key_frame"] == 1)


def _decimal_seconds_to_ns(value: str) -> int:
    return int(Decimal(value) * NANOS_PER_SECOND)


def _timestamp(nanoseconds: int) -> str:
    sign = "-" if nanoseconds < 0 else ""
    absolute = abs(nanoseconds)
    whole, fraction = divmod(absolute, 1_000_000_000)
    return f"{sign}{whole}:{fraction}"


def _timerange(start_ns: int, end_ns: int) -> str:
    return f"[{_timestamp(start_ns)}_{_timestamp(end_ns)})"
