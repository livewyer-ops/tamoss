from __future__ import annotations

import subprocess
import sys
from pathlib import Path

from tests.support.paths import REPO_ROOT

SCRIPT = REPO_ROOT / ".github/scripts/release-metadata.py"


def run_metadata(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [sys.executable, str(SCRIPT), *args],
        cwd=REPO_ROOT,
        check=False,
        text=True,
        capture_output=True,
    )


def parse_output(output: str) -> dict[str, str]:
    items: dict[str, str] = {}
    for line in output.splitlines():
        key, _, value = line.partition("=")
        items[key] = value
    return items


def test_release_metadata_emits_previous_schema_revision() -> None:
    result = run_metadata("8.0.0-oss2")

    assert result.returncode == 0, result.stderr
    metadata = parse_output(result.stdout)
    assert metadata["version"] == "8.0.0-oss2"
    assert metadata["schema_revision"] == "8.0.0-oss1"
    assert metadata["previous_schema_revision"] == "8.0.0-oss1"
    assert metadata["tams_api"] == "8.0"


def test_release_metadata_emits_corrected_80_release_metadata() -> None:
    result = run_metadata("8.0.0-oss3")

    assert result.returncode == 0, result.stderr
    metadata = parse_output(result.stdout)
    assert metadata["version"] == "8.0.0-oss3"
    assert metadata["schema_revision"] == "8.0.0-oss1"
    assert metadata["previous_schema_revision"] == "8.0.0-oss1"
    assert metadata["tams_api"] == "8.0"
    assert metadata["upgrade_from"] == "8.0.0-oss1"


def test_release_metadata_rejects_multiple_previous_schema_revisions(
    tmp_path: Path,
) -> None:
    manifest = tmp_path / "compatibility.json"
    manifest.write_text(
        """
        {
          "releases": [
            {
              "version": "8.0.0-oss1",
              "schemaRevision": "schema-a",
              "tamsAPI": "8.0",
              "upgrade": {"class": "Initial", "from": []}
            },
            {
              "version": "8.0.0-oss2",
              "schemaRevision": "schema-b",
              "tamsAPI": "8.0",
              "upgrade": {"class": "Schema", "from": ["8.0.0-oss1"]}
            },
            {
              "version": "8.0.0-oss3",
              "schemaRevision": "schema-c",
              "tamsAPI": "8.0",
              "upgrade": {
                "class": "Schema",
                "from": ["8.0.0-oss1", "8.0.0-oss2"]
              }
            }
          ]
        }
        """,
        encoding="utf-8",
    )

    result = run_metadata("8.0.0-oss3", str(manifest))

    assert result.returncode == 1
    assert "references multiple schema revisions" in result.stderr
