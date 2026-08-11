from __future__ import annotations

import subprocess
import sys
from pathlib import Path

from tamoss.db.migrations import CURRENT_SCHEMA_REVISION

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


def test_release_metadata_emits_tams_82_schema_upgrade() -> None:
    result = run_metadata("8.2.0-oss1")

    assert result.returncode == 0, result.stderr
    metadata = parse_output(result.stdout)
    assert metadata == {
        "version": "8.2.0-oss1",
        "schema_revision": "8.2.0-oss1",
        "previous_schema_revision": "8.1.0-oss2",
        "tams_api": "8.2",
        "upgrade_class": "SchemaAndAPI",
        "upgrade_from": "8.1.0-oss6",
    }


def test_local_operator_builds_forward_release_metadata() -> None:
    taskfile = (REPO_ROOT / ".tasks/operator.yaml").read_text(encoding="utf-8")
    chainsaw = (REPO_ROOT / ".tasks/lib/operator_chainsaw.sh").read_text(
        encoding="utf-8"
    )
    kind_taskfile = (REPO_ROOT / ".tasks/kind.yaml").read_text(encoding="utf-8")

    for content in (taskfile, chainsaw, kind_taskfile):
        assert "PREVIOUS_SCHEMA_VERSION" in content
        assert "OPERAND_VERSION" in content

    assert 'PREVIOUS_SCHEMA_VERSION: \'{{default "0.0.1"' in kind_taskfile

    for content in (taskfile, chainsaw):
        assert "docker-build" in content
    assert ":operator:image:build" in kind_taskfile


def test_operator_migration_target_matches_api_database_head() -> None:
    operator_schema = (REPO_ROOT / "operator/internal/schema/verify.go").read_text(
        encoding="utf-8"
    )
    kind_taskfile = (REPO_ROOT / ".tasks/kind.yaml").read_text(encoding="utf-8")

    assert f'CurrentDatabaseRevision = "{CURRENT_SCHEMA_REVISION}"' in operator_schema
    kind_revision = CURRENT_SCHEMA_REVISION.replace("_", "-")
    assert f'default "dev-{kind_revision}"' in kind_taskfile


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
