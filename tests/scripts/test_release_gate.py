from __future__ import annotations

import hashlib
import json
import os
import re
import subprocess
import sys
from unittest.mock import Mock

import pytest
import yaml

from tests.support.paths import REPO_ROOT, load_python_module


@pytest.fixture
def image_action():
    return yaml.safe_load(
        (REPO_ROOT / ".github/actions/build-image/action.yaml").read_text()
    )


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
        assert set(jobs[job]["needs"]) == gates
        condition = " ".join(jobs[job]["if"].split())
        successes = " && ".join(
            f"needs.{gate}.result == 'success'" for gate in jobs[job]["needs"]
        )
        assert condition == (
            "${{ !cancelled() && (github.ref_type != 'tag' || (" + successes + ")) }}"
        )
    for filename in ("test.yaml", "operator-ci.yaml"):
        workflow = yaml.safe_load((workflows / filename).read_text())
        assert "workflow_call" in workflow[True]
        assert workflow["concurrency"]["group"] != build["concurrency"]["group"]
    assert (
        build["concurrency"]["cancel-in-progress"] == "${{ github.ref_type != 'tag' }}"
    )


@pytest.mark.parametrize("status", [0, 1, 2, 127])
@pytest.mark.parametrize("task", ["audit:frontend", "audit:frontend:dev", "audit:osv"])
def test_dependency_scanner_failures_are_fatal(tmp_path, status, task) -> None:
    tasks = yaml.safe_load((REPO_ROOT / ".tasks/security.yaml").read_text())["tasks"]
    command = tasks[task]["cmds"][-1]
    result = subprocess.run(
        [
            "bash",
            "-eu",
            "-o",
            "pipefail",
            "-c",
            'npm() { return "$SCANNER_STATUS"; }\n'
            'osv-scanner() { return "$SCANNER_STATUS"; }\n' + command,
        ],
        cwd=tmp_path,
        env={**os.environ, "SCANNER_STATUS": str(status)},
        check=False,
    )
    assert result.returncode == status


def test_shell_lint_checks_later_helpers(tmp_path) -> None:
    helpers = tmp_path / ".tasks/lib"
    helpers.mkdir(parents=True)
    (helpers / "a.sh").write_text("true\n")
    (helpers / "z.sh").write_text("if\n")
    command = yaml.safe_load((REPO_ROOT / ".tasks/lint.yaml").read_text())["tasks"][
        "shell"
    ]["cmds"][0]
    result = subprocess.run(["bash", "-c", command], cwd=tmp_path, check=False)
    assert result.returncode != 0


def test_image_builds_share_steps_without_changing_signing_jobs(image_action) -> None:
    workflow = yaml.safe_load(
        (REPO_ROOT / ".github/workflows/docker-hub.yaml").read_text()
    )
    for component, dockerfile in {
        "tamoss-api": "src/app/tamoss/Dockerfile",
        "tamoss-ui": "src/app/frontend/Dockerfile",
        "tamoss-console-api": "operator/Dockerfile.console-api",
        "tamoss-operator": "operator/Dockerfile",
    }.items():
        job = workflow["jobs"][component]
        assert "uses" not in job
        assert job["permissions"]["id-token"] == "write"
        assert job["steps"][0]["uses"].startswith("actions/checkout@")
        build = next(step for step in job["steps"] if step.get("id") == "build")
        assert build["uses"] == "./.github/actions/build-image"
        inputs = build["with"]
        assert inputs["component"] == component
        assert inputs["file"] == dockerfile
        assert inputs.get("context", ".") == (
            "src/app/frontend" if component == "tamoss-ui" else "."
        )
        assert inputs["push"] == "${{ env.PUSH_IMAGES }}"
        assert inputs["dockerhub-token"] == "${{ secrets.DOCKERHUB_TOKEN }}"
        arguments = {
            line.partition("=")[0] for line in inputs.get("build-args", "").splitlines()
        }
        expected = (
            {"VERSION", "SCHEMA_VERSION", "TAMS_API_VERSION"}
            if component in {"tamoss-api", "tamoss-operator"}
            else set()
        )
        if component == "tamoss-operator":
            expected |= {"PREVIOUS_SCHEMA_VERSION", "OPERAND_VERSION"}
        assert arguments == expected
    assert image_action["runs"]["using"] == "composite"
    assert image_action["outputs"]["digest"]["value"] == (
        "${{ steps.build.outputs.digest }}"
    )
    assert workflow["env"]["PUSH_IMAGES"] == (
        "${{ github.event_name == 'push' || github.event_name == 'workflow_dispatch'"
        " || (github.event_name == 'pull_request' && github.actor != 'dependabot[bot]'"
        " && github.event.pull_request.head.repo.full_name == github.repository) }}"
    )


def test_shared_image_build_keeps_publish_guards_and_attestations(image_action) -> None:
    assert image_action["inputs"]["push"]["default"] == "false"
    steps = image_action["runs"]["steps"]
    for step in steps:
        if "uses" in step:
            assert re.fullmatch(r"[^@]+@[0-9a-f]{40}", step["uses"])
        if "run" in step:
            assert step["shell"] == "bash"
        if step.get("uses", "").startswith(("docker/login-action@", "sigstore/")) or (
            "cosign sign" in step.get("run", "")
        ):
            assert step["if"] == "inputs.push == 'true'"
    build = next(step for step in steps if step.get("id") == "build")["with"]
    assert build["push"] == "${{ inputs.push == 'true' }}"
    assert build["platforms"] == "linux/amd64,linux/arm64"
    assert build["provenance"] == "mode=max"
    assert build["sbom"] is True
    assert build["cache-from"] == "type=gha,scope=${{ inputs.component }}"
    assert build["cache-to"] == "type=gha,mode=max,scope=${{ inputs.component }}"
    metadata = next(step for step in steps if step.get("id") == "meta")["with"]
    assert metadata["images"] == "livewyer/${{ inputs.component }}"
    assert set(metadata["tags"].splitlines()) == {
        "type=schedule",
        "type=ref,event=branch",
        "type=ref,event=tag",
        "type=semver,pattern={{version}}",
        "type=ref,event=pr",
        "type=sha,prefix=sha-",
        "${{ steps.pr-head-tag.outputs.tag }}",
        "type=raw,value=latest,enable={{is_default_branch}}",
    }


@pytest.mark.parametrize("head", ["", "abc1234" + "d" * 33])
def test_shared_image_tags_use_the_pr_head(image_action, tmp_path, head) -> None:
    step = next(
        step
        for step in image_action["runs"]["steps"]
        if step.get("id") == "pr-head-tag"
    )
    output = tmp_path / "output"
    output.touch()
    subprocess.run(
        ["bash", "-e", "-o", "pipefail", "-c", step["run"]],
        env={**os.environ, "PR_HEAD_SHA": head, "GITHUB_OUTPUT": str(output)},
        check=True,
    )
    assert output.read_text() == (
        f"tag=type=raw,value=sha-{head[:7]}\n" if head else ""
    )


@pytest.mark.parametrize("sign_status", [0, 23])
def test_shared_image_signing_signs_each_tag_and_fails_closed(
    image_action, tmp_path, sign_status
) -> None:
    step = image_action["runs"]["steps"][-1]
    digest = "sha256:" + "a" * 64
    tags = ["livewyer/tamoss-api:8.2.0-oss1-rc5", "livewyer/tamoss-api:sha-abc1234"]
    log = tmp_path / "signatures"
    result = subprocess.run(
        [
            "bash",
            "-e",
            "-o",
            "pipefail",
            "-c",
            'cosign() { echo "$*" >> "$SIGN_LOG"; return "$SIGN_STATUS"; }\n'
            + step["run"],
        ],
        env={
            **os.environ,
            "TAGS": "\n".join(tags),
            "DIGEST": digest,
            "SIGN_LOG": str(log),
            "SIGN_STATUS": str(sign_status),
        },
        check=False,
    )
    assert result.returncode == sign_status
    signed = tags if sign_status == 0 else tags[:1]
    assert log.read_text().splitlines() == [
        f"sign --yes {tag}@{digest}" for tag in signed
    ]


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
    assert (
        connection.request.call_args.kwargs["headers"]["User-Agent"]
        == "tamoss-release-preflight"
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
