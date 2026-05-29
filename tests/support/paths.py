from __future__ import annotations

import importlib.util
import sys
from pathlib import Path
from types import ModuleType

REPO_ROOT = Path(__file__).resolve().parents[2]
BBC_API_SPEC_PATH = REPO_ROOT / "src/vendor/bbc-tams/api/TimeAddressableMediaStore.yaml"
BBC_CONTENT_FORMAT_SCHEMA_PATH = (
    REPO_ROOT / "src/vendor/bbc-tams/api/schemas/content-format.json"
)
SCHEMA_ASSETS_DIR = REPO_ROOT / "src/app/tamoss/db/migrations/assets"


def load_python_module(name: str, path: Path) -> ModuleType:
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"Cannot load {path}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module
