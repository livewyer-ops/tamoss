from __future__ import annotations

import re
from typing import Any
from urllib.parse import urlparse

from mediatimestamp import TimeRange, Timestamp

FLOW_FORMAT_VIDEO = "urn:x-nmos:format:video"
FLOW_FORMAT_AUDIO = "urn:x-nmos:format:audio"
FLOW_FORMAT_DATA = "urn:x-nmos:format:data"
FLOW_FORMAT_MULTI = "urn:x-nmos:format:multi"
FLOW_FORMAT_IMAGE = "urn:x-tam:format:image"

VALID_FLOW_FORMATS = {
    FLOW_FORMAT_VIDEO,
    FLOW_FORMAT_AUDIO,
    FLOW_FORMAT_DATA,
    FLOW_FORMAT_MULTI,
    FLOW_FORMAT_IMAGE,
}

_FORMATS_REQUIRING_CODEC = {
    FLOW_FORMAT_VIDEO,
    FLOW_FORMAT_AUDIO,
    FLOW_FORMAT_DATA,
    FLOW_FORMAT_IMAGE,
}

_MIME_TYPE_RE = re.compile(
    r"^(application|audio|font|example|image|message|model|multipart|text|video|"
    r"x-(?:[0-9A-Za-z!#$%&'*+.^_`|~-]+))/"
    r"([0-9A-Za-z!#$%&'*+.^_`|~-]+)$"
)

_VIDEO_ESSENCE_KEYS = {
    "frame_width",
    "frame_height",
    "bit_depth",
    "interlace_mode",
    "colorspace",
    "transfer_characteristic",
    "aspect_ratio",
    "pixel_aspect_ratio",
    "component_type",
    "horiz_chroma_subs",
    "vert_chroma_subs",
    "unc_parameters",
    "avc_parameters",
    "frame_rate",
    "vfr",
}
_AUDIO_ESSENCE_KEYS = {
    "sample_rate",
    "channels",
    "bit_depth",
    "codec_parameters",
    "unc_parameters",
}
_DATA_ESSENCE_KEYS = {"data_type"}
_IMAGE_ESSENCE_KEYS = {"frame_width", "frame_height", "aspect_ratio"}


def validate_flow_payload(payload: dict[str, Any]) -> None:
    """Validate a Flow body against the BBC concrete flow shapes."""
    if payload.get("source_id") is None:
        raise ValueError("source_id is required")

    format_value = payload.get("format")
    if format_value not in VALID_FLOW_FORMATS:
        raise ValueError("format must be a supported BBC flow format")

    if format_value in _FORMATS_REQUIRING_CODEC:
        codec = payload.get("codec")
        if not isinstance(codec, str) or not codec:
            raise ValueError("codec is required")
        _validate_mime_type(codec, "codec")
    elif payload.get("codec") is not None:
        _validate_mime_type(payload["codec"], "codec")

    if payload.get("container") is not None:
        _validate_mime_type(payload["container"], "container")

    if payload.get("tags") is not None:
        _validate_tags(payload["tags"])

    _validate_optional_nonnegative_int(payload, "generation")
    _validate_optional_nonnegative_int(payload, "avg_bit_rate")
    _validate_optional_nonnegative_int(payload, "max_bit_rate")

    if payload.get("segment_duration") is not None:
        _validate_fraction(
            payload["segment_duration"],
            "segment_duration",
            require_denominator=False,
        )

    if format_value == FLOW_FORMAT_MULTI:
        return

    essence = payload.get("essence_parameters")
    if not isinstance(essence, dict):
        raise ValueError("essence_parameters is required")

    if format_value == FLOW_FORMAT_VIDEO:
        _validate_video_essence(essence)
    elif format_value == FLOW_FORMAT_AUDIO:
        _validate_audio_essence(essence)
    elif format_value == FLOW_FORMAT_DATA:
        _validate_data_essence(essence)
    elif format_value == FLOW_FORMAT_IMAGE:
        _validate_image_essence(essence)


def validate_content_format_filter(value: str | None) -> None:
    if value is not None and value not in VALID_FLOW_FORMATS:
        raise ValueError("format must be a supported BBC content format")


def validate_segment_payload(
    payload: dict[str, Any], *, reserved_get_url_labels: set[str] | None = None
) -> None:
    parse_timerange(payload.get("timerange"), field_name="timerange", finite=True)

    if payload.get("ts_offset") is not None:
        parse_timestamp(payload["ts_offset"], field_name="ts_offset")
    if payload.get("last_duration") is not None:
        last_duration = parse_timestamp(
            payload["last_duration"], field_name="last_duration"
        )
        if int(last_duration.to_nanosec()) < 0:
            raise ValueError("last_duration must not be negative")
    if payload.get("object_timerange") is not None:
        parse_timerange(
            payload["object_timerange"],
            field_name="object_timerange",
            finite=True,
        )

    _validate_optional_nonnegative_int(payload, "sample_offset")
    _validate_optional_nonnegative_int(payload, "sample_count")
    _validate_optional_nonnegative_int(payload, "key_frame_count")

    if payload.get("get_urls") is not None:
        _validate_get_urls(
            payload["get_urls"],
            reserved_labels=reserved_get_url_labels or set(),
        )


def parse_timerange(
    value: Any, *, field_name: str = "timerange", finite: bool = False
) -> TimeRange:
    if not isinstance(value, str) or not value:
        raise ValueError(f"{field_name} must be a timerange string")
    try:
        parsed = TimeRange.from_str(value)
    except Exception as exc:
        raise ValueError(f"{field_name} is invalid") from exc
    if finite and (parsed.start is None or parsed.end is None):
        raise ValueError(f"{field_name} must have finite start and end timestamps")
    return parsed


def parse_timestamp(value: Any, *, field_name: str) -> Timestamp:
    if not isinstance(value, str) or not value:
        raise ValueError(f"{field_name} must be a timestamp string")
    try:
        return Timestamp.from_str(value)
    except Exception as exc:
        raise ValueError(f"{field_name} is invalid") from exc


def _validate_video_essence(essence: dict[str, Any]) -> None:
    _reject_unknown_keys(essence, _VIDEO_ESSENCE_KEYS, "essence_parameters")
    _require_positive_int(essence, "frame_width", "essence_parameters.frame_width")
    _require_positive_int(essence, "frame_height", "essence_parameters.frame_height")
    _validate_optional_positive_int(essence, "bit_depth")
    _validate_optional_positive_int(essence, "horiz_chroma_subs")
    _validate_optional_positive_int(essence, "vert_chroma_subs")
    _validate_optional_enum(
        essence,
        "interlace_mode",
        {"progressive", "interlaced_tff", "interlaced_bff", "interlaced_psf"},
    )
    _validate_optional_enum(
        essence, "colorspace", {"BT601", "BT709", "BT2020", "BT2100"}
    )
    _validate_optional_enum(essence, "transfer_characteristic", {"SDR", "HLG", "PQ"})
    _validate_optional_enum(essence, "component_type", {"YCbCr", "RGB"})

    if essence.get("aspect_ratio") is not None:
        _validate_fraction(essence["aspect_ratio"], "essence_parameters.aspect_ratio")
    if essence.get("pixel_aspect_ratio") is not None:
        _validate_fraction(
            essence["pixel_aspect_ratio"],
            "essence_parameters.pixel_aspect_ratio",
        )
    if essence.get("unc_parameters") is not None:
        _validate_unc_parameters(
            essence["unc_parameters"],
            {
                "planar",
                "YUYV",
                "UYVY",
                "AYUV",
                "v210",
                "v216",
                "RGB",
                "RGBx",
                "xRGB",
                "BGRx",
                "xBGR",
                "RGBA",
                "ARGB",
                "BGRA",
                "ABGR",
                "alpha",
            },
            "essence_parameters.unc_parameters",
        )
    if essence.get("avc_parameters") is not None:
        _validate_avc_parameters(essence["avc_parameters"])

    vfr = essence.get("vfr", False)
    if not isinstance(vfr, bool):
        raise ValueError("essence_parameters.vfr must be a boolean")
    if vfr:
        if "frame_rate" in essence:
            raise ValueError("frame_rate must not be set when vfr is true")
    else:
        if essence.get("frame_rate") is None:
            raise ValueError("frame_rate is required when vfr is false or omitted")
        _validate_fraction(
            essence["frame_rate"],
            "essence_parameters.frame_rate",
            require_denominator=False,
        )


def _validate_audio_essence(essence: dict[str, Any]) -> None:
    _reject_unknown_keys(essence, _AUDIO_ESSENCE_KEYS, "essence_parameters")
    _require_positive_int(essence, "sample_rate", "essence_parameters.sample_rate")
    _require_positive_int(essence, "channels", "essence_parameters.channels")
    _validate_optional_positive_int(essence, "bit_depth")
    if essence.get("unc_parameters") is not None:
        _validate_unc_parameters(
            essence["unc_parameters"],
            {"interleaved", "planar", "pairs"},
            "essence_parameters.unc_parameters",
        )
    codec_parameters = essence.get("codec_parameters")
    if codec_parameters is not None:
        if not isinstance(codec_parameters, dict):
            raise ValueError("essence_parameters.codec_parameters must be an object")
        _validate_optional_int(codec_parameters, "coded_frame_size")
        _validate_optional_int(codec_parameters, "mp4_oti")


def _validate_data_essence(essence: dict[str, Any]) -> None:
    _reject_unknown_keys(essence, _DATA_ESSENCE_KEYS, "essence_parameters")
    if essence.get("data_type") is not None and not isinstance(
        essence["data_type"], str
    ):
        raise ValueError("essence_parameters.data_type must be a string")


def _validate_image_essence(essence: dict[str, Any]) -> None:
    _reject_unknown_keys(essence, _IMAGE_ESSENCE_KEYS, "essence_parameters")
    _require_positive_int(essence, "frame_width", "essence_parameters.frame_width")
    _require_positive_int(essence, "frame_height", "essence_parameters.frame_height")
    if essence.get("aspect_ratio") is not None:
        _validate_fraction(essence["aspect_ratio"], "essence_parameters.aspect_ratio")


def _validate_get_urls(entries: Any, *, reserved_labels: set[str]) -> None:
    if not isinstance(entries, list) or not entries:
        raise ValueError("get_urls must be a non-empty array")
    labels: set[str] = set()
    for entry in entries:
        if not isinstance(entry, dict):
            raise ValueError("get_urls entries must be objects")
        label = entry.get("label")
        if not isinstance(label, str) or not label:
            raise ValueError("get_urls entries require a label")
        if label in labels or label in reserved_labels:
            raise ValueError("get_urls labels must be unique and uncontrolled")
        labels.add(label)
        url = entry.get("url")
        if not isinstance(url, str):
            raise ValueError("get_urls entries require a url")
        parsed = urlparse(url)
        if parsed.scheme.lower() not in {"http", "https"} or not parsed.netloc:
            raise ValueError("get_urls entries require an HTTP URL")


def _validate_tags(tags: Any) -> None:
    if not isinstance(tags, dict):
        raise ValueError("tags must be an object")
    for key, value in tags.items():
        if not isinstance(key, str):
            raise ValueError("tag names must be strings")
        if isinstance(value, str):
            continue
        if isinstance(value, list) and all(isinstance(item, str) for item in value):
            continue
        raise ValueError("tag values must be strings or arrays of strings")


def _validate_mime_type(value: Any, field_name: str) -> None:
    if not isinstance(value, str) or not _MIME_TYPE_RE.match(value):
        raise ValueError(f"{field_name} must be a MIME type")


def _validate_fraction(
    value: Any, field_name: str, *, require_denominator: bool = True
) -> None:
    if not isinstance(value, dict):
        raise ValueError(f"{field_name} must be an object")
    _require_positive_int(value, "numerator", f"{field_name}.numerator")
    if require_denominator or value.get("denominator") is not None:
        _require_positive_int(value, "denominator", f"{field_name}.denominator")


def _validate_unc_parameters(
    value: Any, allowed_values: set[str], field_name: str
) -> None:
    if not isinstance(value, dict):
        raise ValueError(f"{field_name} must be an object")
    unc_type = value.get("unc_type")
    if unc_type not in allowed_values:
        raise ValueError(f"{field_name}.unc_type is invalid")


def _validate_avc_parameters(value: Any) -> None:
    if not isinstance(value, dict):
        raise ValueError("essence_parameters.avc_parameters must be an object")
    for field_name in ("profile", "level", "flags"):
        _require_int(
            value,
            field_name,
            f"essence_parameters.avc_parameters.{field_name}",
        )


def _reject_unknown_keys(
    value: dict[str, Any], allowed_keys: set[str], path: str
) -> None:
    unknown = sorted(set(value) - allowed_keys)
    if unknown:
        raise ValueError(f"{path} contains unsupported fields: {', '.join(unknown)}")


def _validate_optional_enum(
    value: dict[str, Any], field_name: str, allowed_values: set[str]
) -> None:
    if value.get(field_name) is not None and value[field_name] not in allowed_values:
        raise ValueError(f"{field_name} is invalid")


def _validate_optional_nonnegative_int(value: dict[str, Any], field_name: str) -> None:
    if value.get(field_name) is not None:
        _require_nonnegative_int(value, field_name, field_name)


def _validate_optional_positive_int(value: dict[str, Any], field_name: str) -> None:
    if value.get(field_name) is not None:
        _require_positive_int(value, field_name, f"essence_parameters.{field_name}")


def _validate_optional_int(value: dict[str, Any], field_name: str) -> None:
    if value.get(field_name) is not None:
        _require_int(value, field_name, field_name)


def _require_positive_int(value: dict[str, Any], field_name: str, path: str) -> None:
    _require_int(value, field_name, path)
    if value[field_name] <= 0:
        raise ValueError(f"{path} must be greater than zero")


def _require_nonnegative_int(value: dict[str, Any], field_name: str, path: str) -> None:
    _require_int(value, field_name, path)
    if value[field_name] < 0:
        raise ValueError(f"{path} must not be negative")


def _require_int(value: dict[str, Any], field_name: str, path: str) -> None:
    if field_name not in value:
        raise ValueError(f"{path} is required")
    if not isinstance(value[field_name], int) or isinstance(value[field_name], bool):
        raise ValueError(f"{path} must be an integer")
