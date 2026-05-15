"""TAMOSS application package."""

from typing import Any

app: Any
create_app: Any


def __getattr__(name: str) -> Any:
    if name == "app":
        from tamoss.app import app

        return app
    if name == "create_app":
        from tamoss.app import create_app

        return create_app
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")


__all__ = ["app", "create_app"]
