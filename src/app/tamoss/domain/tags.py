from __future__ import annotations

from collections.abc import Mapping

TagValue = str | list[str]


def valid_tag_value(value: TagValue) -> bool:
    if isinstance(value, str):
        return True
    return isinstance(value, list) and all(isinstance(item, str) for item in value)


def parse_tag_value_list(value: str) -> set[str]:
    if value == "":
        return set()
    parts = value.split(",")
    if any(part == "" for part in parts):
        raise ValueError("tag value lists must not contain empty members")
    return set(parts)


def parse_tag_filters(
    query_params: Mapping[str, str],
) -> tuple[dict[str, set[str]], dict[str, bool]]:
    values: dict[str, set[str]] = {}
    exists: dict[str, bool] = {}
    for key, value in query_params.items():
        if key.startswith("tag."):
            tag_name = key.removeprefix("tag.")
            if tag_name == "{name}":
                continue
            values[tag_name] = parse_tag_value_list(value)
        elif key.startswith("tag_exists."):
            tag_name = key.removeprefix("tag_exists.")
            if tag_name == "{name}":
                continue
            exists[tag_name] = parse_bool_filter(value)
    return values, exists


def parse_bool_filter(value: str) -> bool:
    lowered = value.lower()
    if lowered == "true":
        return True
    if lowered == "false":
        return False
    raise ValueError("Input should be a valid boolean")


def tags_match(
    tags: Mapping[str, TagValue],
    value_filters: Mapping[str, set[str]],
    existence_filters: Mapping[str, bool],
) -> bool:
    for name, expected_values in value_filters.items():
        actual = tags.get(name)
        if actual is None:
            return False
        actual_values = set(actual if isinstance(actual, list) else [actual])
        if not expected_values.intersection(actual_values):
            return False

    for name, should_exist in existence_filters.items():
        if (name in tags) != should_exist:
            return False
    return True
