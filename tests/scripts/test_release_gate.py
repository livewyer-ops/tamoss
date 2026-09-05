from __future__ import annotations

import hashlib
import json
import sys
from unittest.mock import Mock

import pytest
import yaml

from tests.support.paths import REPO_ROOT, load_python_module


def test_release_publication_requires_every_image_and_tag_validation() -> None:
    workflows = REPO_ROOT / ".github/workflows"
    build = yaml.safe_load((workflows / "docker-hub.yaml").read_text())
    publish = yaml.safe_load((workflows / "operator-release.yaml").read_text())
    # PyYAML's YAML 1.1 loader represents GitHub's `on` key as True.
    assert set(publish[True]) == {"workflow_call"}
    jobs = build["jobs"]
    images = {"tamoss-api", "tamoss-ui", "tamoss-console-api", "tamoss-operator"}
    gates = {
        "dependency-audit",
        "release-preflight",
        "release-tests",
        "release-operator-tests",
        "release-kind-e2e",
    }
    assert set(jobs["release-assets"]["needs"]) == images | gates
    assert jobs["release-assets"]["if"] == "github.ref_type == 'tag'"
    for job in images:
        assert jobs[job]["outputs"]["digest"] == "${{ steps.build.outputs.digest }}"
        assert "release-preflight" in jobs[job]["needs"]
        assert "needs.dependency-audit.result == 'success'" in jobs[job]["if"]
    for filename in ("test.yaml", "operator-ci.yaml"):
        workflow = yaml.safe_load((workflows / filename).read_text())
        assert "workflow_call" in workflow[True]
        assert workflow["concurrency"]["group"] != build["concurrency"]["group"]
    assert (
        build["concurrency"]["cancel-in-progress"] == "${{ github.ref_type != 'tag' }}"
    )


@pytest.mark.parametrize("draft", [True, False, None])
def test_release_preflight_only_allows_drafts(monkeypatch, draft) -> None:
    module = load_python_module(
        "release_preflight", REPO_ROOT / ".github/scripts/release-preflight.py"
    )
    connection = Mock()
    connection.getresponse.return_value = Mock(
        status=200, read=lambda: json.dumps({"draft": draft})
    )
    monkeypatch.setattr(module, "HTTPSConnection", Mock(return_value=connection))
    arguments = {
        "api_url": "https://api.github.com",
        "repository": "org/repo",
        "tag": "8.2.0-oss1-rc5",
        "token": "secret",
    }
    if draft is True:
        module.verify_unpublished(**arguments)
    else:
        with pytest.raises(SystemExit, match="already published"):
            module.verify_unpublished(**arguments)
    connection.close.assert_called_once()


@pytest.mark.parametrize("status", [302, 307, 401, 403, 404, 429, 500])
def test_release_preflight_fails_closed_except_for_missing_release(
    monkeypatch, status
) -> None:
    module = load_python_module(
        "release_preflight", REPO_ROOT / ".github/scripts/release-preflight.py"
    )
    connection = Mock()
    connection.getresponse.return_value.status = status
    monkeypatch.setattr(module, "HTTPSConnection", Mock(return_value=connection))
    arguments = {
        "api_url": "https://api.github.com",
        "repository": "org/repo",
        "tag": "8.2.0-oss1-rc5",
        "token": "secret",
    }
    if status == 404:
        module.verify_unpublished(**arguments)
    else:
        with pytest.raises(SystemExit, match=f"HTTP {status}"):
            module.verify_unpublished(**arguments)
    connection.request.assert_called_once()
    connection.close.assert_called_once()


@pytest.mark.parametrize(
    "api_url",
    [
        "http://api.github.com",
        "file:///tmp/release",
        "https://user:password@api.github.com",
        "https://api.github.com?token=value",
    ],
)
def test_release_preflight_rejects_unsafe_api_urls(monkeypatch, api_url) -> None:
    module = load_python_module(
        "release_preflight", REPO_ROOT / ".github/scripts/release-preflight.py"
    )
    connect = Mock()
    monkeypatch.setattr(module, "HTTPSConnection", connect)
    with pytest.raises(ValueError, match="HTTPS origin"):
        module.verify_unpublished(
            api_url=api_url, repository="org/repo", tag="8.2.0-oss1", token="secret"
        )
    connect.assert_not_called()


def test_release_preflight_preserves_enterprise_api_prefix_and_encodes_tag(
    monkeypatch,
) -> None:
    module = load_python_module(
        "release_preflight", REPO_ROOT / ".github/scripts/release-preflight.py"
    )
    connection = Mock()
    connection.getresponse.return_value.status = 404
    connect = Mock(return_value=connection)
    monkeypatch.setattr(module, "HTTPSConnection", connect)
    module.verify_unpublished(
        api_url="https://github.example:8443/api/v3/",
        repository="org/repo",
        tag="release/8.2.0",
        token="secret",
    )
    connect.assert_called_once_with("github.example", 8443, timeout=30)
    assert connection.request.call_args.args == (
        "GET",
        "/api/v3/repos/org/repo/releases/tags/release%2F8.2.0",
    )


def test_release_record_rejects_any_missing_or_malformed_image_digest() -> None:
    module = load_python_module(
        "release_record", REPO_ROOT / ".github/scripts/release-record.py"
    )
    environment = {
        f"{component.upper()}_DIGEST": "sha256:" + "a" * 64
        for component in module.IMAGES
    }
    references = module.image_references(environment)
    assert len(references) == 4
    assert all(
        reference.endswith("@sha256:" + "a" * 64) for reference in references.values()
    )
    for key in environment:
        for invalid in ("", "latest", "sha256:short", "sha256:" + "z" * 64):
            with pytest.raises(ValueError, match="image digest"):
                module.image_references({**environment, key: invalid})


def test_release_record_includes_assets_specification_and_worker_identity(
    tmp_path, monkeypatch
) -> None:
    module = load_python_module(
        "release_record", REPO_ROOT / ".github/scripts/release-record.py"
    )
    monkeypatch.chdir(tmp_path)
    install = tmp_path / "dist/operator-release/install.yaml"
    compatibility = tmp_path / "operator/compatibility.yaml"
    for path in (install, compatibility):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("release fixture\n")
    compatibility.write_text((REPO_ROOT / "operator/compatibility.yaml").read_text())
    for name, value in {
        "GITHUB_REF_NAME": "8.2.0-oss1-rc5",
        "GITHUB_SERVER_URL": "https://github.com",
        "GITHUB_REPOSITORY": "livewyer-ops/tamoss",
        "GITHUB_RUN_ID": "1234",
        "GITHUB_RUN_ATTEMPT": "2",
        "SOURCE_COMMIT": "a" * 40,
        "BBC_TAMS_COMMIT": "b" * 40,
        **{f"{name.upper()}_DIGEST": "sha256:" + "c" * 64 for name in module.IMAGES},
    }.items():
        monkeypatch.setenv(name, value)
    output = tmp_path / "release.json"
    monkeypatch.setattr(sys, "argv", ["release-record.py", "--output", str(output)])
    module.main()
    record = json.loads(output.read_text())
    assert record["sourceCommit"] == "a" * 40
    assert record["bbcTamsCommit"] == "b" * 40
    assert record["compatibility"]["tams_api"] == "8.2"
    assert record["workerImage"] == record["images"]["api"]
    assert record["validationRun"].endswith("/actions/runs/1234")
    assert record["validationRunAttempt"] == "2"
    assert record["artifacts"] == {
        path.name: {"sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
        for path in (install, compatibility)
    }
