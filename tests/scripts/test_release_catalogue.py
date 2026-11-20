from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys

import pytest
import yaml

from tests.support.paths import REPO_ROOT


@pytest.mark.parametrize(
    "version", ["8.2.0-oss2", "8.2.0-oss2-rc3", "8.2.0-oss3", "8.2.0-oss3-rc1"]
)
def test_release_catalogue_preserves_published_runtimes(tmp_path, monkeypatch, version):
    output = tmp_path / "catalogue.json"
    monkeypatch.setenv("GITHUB_REF_NAME", version)
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
    assert current["version"] == version
    assert current["sourceCommit"] == "a" * 40
    assert current["schema"]["databaseRevision"] == "20260924_0008"
    assert current["images"]["api"] == "livewyer/tamoss-api@sha256:" + "0" * 64
    assert current["images"]["ui"] == "livewyer/tamoss-ui@sha256:" + "1" * 64
    assert (
        current["images"]["console"] == "livewyer/tamoss-console-api@sha256:" + "2" * 64
    )
    if version.startswith("8.2.0-oss2"):
        rc2 = next(entry for entry in entries if entry["version"] == "8.2.0-oss2-rc2")
        assert rc2["schema"]["version"] == "8.2.0-oss1"
        assert rc2["schema"]["databaseRevision"] == "20260810_0007"
    else:
        assert [entry["version"] for entry in entries] == ["8.2.0-oss2", version]
        assert (
            current["schema"]["version"]
            == current["schema"]["previousVersion"]
            == "8.2.0-oss2"
        )


@pytest.fixture(scope="module")
def kustomize():
    subprocess.run(["make", "-C", "operator", "kustomize"], cwd=REPO_ROOT, check=True)
    return next((REPO_ROOT / "operator/bin").glob("kustomize-v*"))


@pytest.mark.parametrize("version", ["8.2.0-oss2", "8.2.0-oss3-rc1"])
def test_published_install_mounts_matching_release_catalogue(
    tmp_path, version, kustomize
):
    environment = {
        **os.environ,
        "GITHUB_REF_NAME": version,
        "SOURCE_COMMIT": "a" * 40,
        **{
            f"{name}_DIGEST": "sha256:" + str(index) * 64
            for index, name in enumerate(("API", "UI", "CONSOLE_API", "OPERATOR"))
        },
    }
    output = tmp_path / "dist/operator-release"
    subprocess.run(
        [
            sys.executable,
            ".github/scripts/release-catalogue.py",
            "--output",
            str(output / "catalogue.json"),
        ],
        cwd=REPO_ROOT,
        env=environment,
        check=True,
    )
    shutil.copytree(REPO_ROOT / "operator/config", tmp_path / "operator/config")
    binaries = tmp_path / "operator/bin"
    binaries.mkdir()
    (binaries / kustomize.name).symlink_to(kustomize)
    workflow = yaml.safe_load(
        (REPO_ROOT / ".github/workflows/operator-release.yaml").read_text()
    )
    step = next(
        step
        for step in workflow["jobs"]["build-and-release"]["steps"]
        if step["name"] == "Build release install manifest"
    )
    subprocess.run(
        ["bash", "-euo", "pipefail", "-c", step["run"]],
        cwd=tmp_path,
        env=environment,
        check=True,
    )
    resources = list(yaml.safe_load_all((output / "install.yaml").read_text()))
    deployment = next(
        item
        for item in resources
        if item["kind"] == "Deployment"
        and item["metadata"]["name"] == "operator-controller-manager"
    )
    pod = deployment["spec"]["template"]["spec"]
    manager = next(item for item in pod["containers"] if item["name"] == "manager")
    assert (
        manager["image"] == "livewyer/tamoss-operator@" + environment["OPERATOR_DIGEST"]
    )
    mount = next(
        item for item in manager["volumeMounts"] if item["name"] == "release-catalogue"
    )
    catalogue_path = next(
        item["value"]
        for item in manager["env"]
        if item["name"] == "TAMOSS_RELEASE_CATALOGUE"
    )
    assert catalogue_path == mount["mountPath"] + "/catalogue.json"
    config_name = next(
        item["configMap"]["name"]
        for item in pod["volumes"]
        if item["name"] == "release-catalogue"
    )
    config = next(
        item
        for item in resources
        if item["kind"] == "ConfigMap" and item["metadata"]["name"] == config_name
    )
    assert config["immutable"] is True
    entries = json.loads(config["data"]["catalogue.json"])
    assert entries == json.loads((output / "catalogue.json").read_text())
    assert "dev" not in [entry["version"] for entry in entries]
    current = next(entry for entry in entries if entry["version"] == version)
    for component, digest in (("api", "API"), ("ui", "UI"), ("console", "CONSOLE_API")):
        assert current["images"][component].endswith(
            "@" + environment[f"{digest}_DIGEST"]
        )


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
