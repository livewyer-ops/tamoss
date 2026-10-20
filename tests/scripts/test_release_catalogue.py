from __future__ import annotations

import json
import os
import subprocess
import sys

from tests.support.paths import REPO_ROOT


def test_release_catalogue_preserves_published_runtimes(tmp_path, monkeypatch):
    output = tmp_path / "catalogue.json"
    monkeypatch.setenv("GITHUB_REF_NAME", "8.2.0-oss2-rc3")
    monkeypatch.setenv("SOURCE_COMMIT", "a" * 40)
    for index, name in enumerate(("API", "UI", "CONSOLE_API", "OPERATOR")):
        monkeypatch.setenv(f"{name}_DIGEST", "sha256:" + str(index) * 64)
    subprocess.run(
        [
            sys.executable,
            ".github/scripts/release-catalogue.py",
            "--output",
            str(output),
        ],
        cwd=REPO_ROOT,
        env=os.environ,
        check=True,
    )
    entries = json.loads(output.read_text())
    for entry in entries[:-1]:
        published = REPO_ROOT / "operator/releases" / f"{entry['version']}.json"
        assert entry == json.loads(published.read_text())
    current = entries[-1]
    assert current["version"] == "8.2.0-oss2-rc3"
    assert current["sourceCommit"] == "a" * 40
    assert current["schema"]["databaseRevision"] == "20260924_0008"
    assert current["images"]["api"] == "livewyer/tamoss-api@sha256:" + "0" * 64
    assert current["images"]["ui"] == "livewyer/tamoss-ui@sha256:" + "1" * 64
    assert (
        current["images"]["console"] == "livewyer/tamoss-console-api@sha256:" + "2" * 64
    )
    rc2 = next(entry for entry in entries if entry["version"] == "8.2.0-oss2-rc2")
    assert rc2["schema"]["version"] == "8.2.0-oss1"
    assert rc2["schema"]["databaseRevision"] == "20260810_0007"


def test_development_catalogue_is_current(tmp_path, monkeypatch):
    monkeypatch.delenv("SOURCE_COMMIT", raising=False)
    output = tmp_path / "catalogue.json"
    subprocess.run(
        [
            sys.executable,
            ".github/scripts/release-catalogue.py",
            "--development",
            "--output",
            str(output),
        ],
        cwd=REPO_ROOT,
        check=True,
    )
    assert (
        output.read_text()
        == (REPO_ROOT / "operator/config/releases/catalogue.json").read_text()
    )
