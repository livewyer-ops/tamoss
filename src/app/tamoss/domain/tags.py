from __future__ import annotations

from collections.abc import Mapping

TagValue = str | list[str]


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
            values[tag_name] = {part for part in value.split(",") if part}
        elif key.startswith("tag_exists."):
            tag_name = key.removeprefix("tag_exists.")
            if tag_name == "{name}":
                continue
            exists[tag_name] = value.lower() == "true"
    return values, exists


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
