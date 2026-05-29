from __future__ import annotations

import os
import shutil
from pathlib import Path
from typing import Any
from uuid import uuid4

import pytest

from tests.e2e.client import E2EClient
from tests.e2e.commands import run_command as run
from tests.e2e.target import E2ETarget
from tests.support.fixtures import load_json_fixture
from tests.support.paths import REPO_ROOT

KIND_TARGET_ENV = REPO_ROOT / "tests/targets/kind.env"

pytestmark = [
    pytest.mark.e2e,
    pytest.mark.operator_kind,
    pytest.mark.slow,
    pytest.mark.skipif(
        os.getenv("TAMOSS_OPERATOR_KIND_E2E") != "1",
        reason="set TAMOSS_OPERATOR_KIND_E2E=1 to run the Kind operator e2e",
    ),
]


def test_operator_kind_zero_to_ready_api_ingest_and_ui_load(
    tmp_path: Path,
) -> None:
    cluster_name = os.getenv("TAMOSS_OPERATOR_KIND_NAME", "tamoss-operator-kind")
    kubeconfig = tmp_path / "kind.kubeconfig"

    run(["kind", "delete", "cluster", "--name", cluster_name], check=False)
    try:
        run(
            [
                "env",
                f"PROJECT_NAME={cluster_name}",
                f"KUBECONFIG={kubeconfig}",
                "task",
                "kind:up",
            ],
            timeout=1800,
        )
        run(
            [
                "kubectl",
                "--kubeconfig",
                str(kubeconfig),
                "-n",
                "tams",
                "wait",
                "--for=condition=Ready",
                "tamoss/tamoss-kind",
                "--timeout=15m",
            ],
            timeout=900,
        )

        target = E2ETarget.from_file(_target_env(tmp_path, kubeconfig))
        client = E2EClient(target)
        client.wait_ready(timeout_seconds=300)

        _smoke_ingest(client)
        ui = client.request(
            "GET",
            "/",
            base="ui",
            auth=False,
            allow_redirects=False,
            expected=target.ui_expected_statuses,
        )
        assert ui.status_code in target.ui_expected_statuses
    finally:
        run(["kind", "delete", "cluster", "--name", cluster_name], check=False)


def _smoke_ingest(client: E2EClient) -> None:
    backends = client.request_json("GET", "/service/storage-backends")
    storage_backend = next((item for item in backends if item["default_storage"]), None)
    assert storage_backend is not None

    flow_id = str(uuid4())
    source_id = str(uuid4())
    object_id = f"operator-kind-e2e/{uuid4()}.ts"
    deleted = False

    try:
        client.request_json(
            "PUT",
            f"/flows/{flow_id}",
            json=_video_flow_payload(
                flow_id,
                source_id,
                label=f"operator kind e2e {flow_id[:8]}",
            ),
            expected=201,
        )
        allocated = client.request_json(
            "POST",
            f"/flows/{flow_id}/storage",
            json=_storage_allocation_payload(object_id, storage_backend["id"]),
            expected=201,
        )
        put_request = allocated["media_objects"][0]["put_url"]
        client.upload_put_url(
            put_request["url"],
            body=b"tamoss operator kind e2e\n",
            headers=put_request.get("headers") or {},
        )
        client.request(
            "POST",
            f"/flows/{flow_id}/segments",
            json=_segment_payload(object_id),
            expected=201,
        )
        segment_list = client.request_json("GET", f"/flows/{flow_id}/segments")
        assert segment_list[0]["object_id"] == object_id

        accepted = client.request_json("DELETE", f"/flows/{flow_id}", expected=202)
        client.poll_delete_request(accepted["id"])
        deleted = True
    finally:
        if not deleted:
            cleanup = client.request(
                "DELETE", f"/flows/{flow_id}", expected={202, 204, 404}
            )
            if cleanup.status_code == 202:
                client.poll_delete_request(cleanup.json()["id"])


def _target_env(tmp_path: Path, kubeconfig: Path) -> Path:
    target = tmp_path / "kind.env"
    shutil.copyfile(KIND_TARGET_ENV, target)
    with target.open("a", encoding="utf-8") as handle:
        handle.write(f"\nKUBECONFIG={kubeconfig}\n")
    return target


def _video_flow_payload(flow_id: str, source_id: str, *, label: str) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/video_flow_payload.json")
    payload["id"] = flow_id
    payload["source_id"] = source_id
    payload["label"] = label
    return payload


def _segment_payload(object_id: str) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/segment_payload.json")
    payload["object_id"] = object_id
    return payload


def _storage_allocation_payload(object_id: str, storage_id: str) -> dict[str, Any]:
    payload: dict[str, Any] = load_json_fixture("bbc/storage_allocation.json")
    payload["object_ids"] = [object_id]
    payload["storage_id"] = storage_id
    return payload
