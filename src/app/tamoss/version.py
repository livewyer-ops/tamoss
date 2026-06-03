from __future__ import annotations

from importlib.metadata import PackageNotFoundError, version

BBC_TAMS_API_VERSION = "8.1"
DEVELOPMENT_VERSION = "tamoss-dev"


def tamoss_version() -> str:
    try:
        value = version("tamoss")
    except PackageNotFoundError:
        return DEVELOPMENT_VERSION
    if value in {"", "0.0.0"}:
        return DEVELOPMENT_VERSION
    return value


TAMOSS_VERSION = tamoss_version()
