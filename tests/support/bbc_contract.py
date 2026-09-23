"""Validate HTTP payloads against the unmodified, vendored BBC contract."""

import json
from functools import cache

import yaml
from jsonschema import Draft202012Validator
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

from tests.support.paths import BBC_API_SPEC_PATH


@cache
def bbc_spec() -> dict:
    return yaml.safe_load(BBC_API_SPEC_PATH.read_text())


@cache
def bbc_registry() -> Registry:
    resources = [(BBC_API_SPEC_PATH.as_uri(), bbc_spec())]
    resources.extend(
        (path.as_uri(), json.loads(path.read_text()))
        for path in sorted((BBC_API_SPEC_PATH.parent / "schemas").glob("*.json"))
    )
    return Registry().with_resources(
        (uri, Resource.from_contents(content, default_specification=DRAFT202012))
        for uri, content in resources
    )


def bbc_validator(reference: str) -> Draft202012Validator:
    uri = (
        BBC_API_SPEC_PATH.as_uri() + reference
        if reference.startswith("#")
        else (BBC_API_SPEC_PATH.parent / "schemas" / reference).as_uri()
    )
    return Draft202012Validator({"$ref": uri}, registry=bbc_registry())


def response_validator(path: str, method: str, status: int) -> Draft202012Validator:
    escaped_path = path.replace("~", "~0").replace("/", "~1")
    return bbc_validator(
        f"#/paths/{escaped_path}/{method.lower()}/responses/{status}"
        "/content/application~1json/schema"
    )
