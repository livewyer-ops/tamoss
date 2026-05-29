from __future__ import annotations

import base64
import subprocess

import pytest


def kubectl(
    *,
    kubeconfig: str | None,
    args: list[str],
    input_text: str | None = None,
) -> subprocess.CompletedProcess[str]:
    command = ["kubectl"]
    if kubeconfig:
        command.extend(["--kubeconfig", kubeconfig])
    command.extend(args)
    return subprocess.run(
        command,
        input=input_text,
        text=True,
        check=True,
        capture_output=True,
    )


def load_secret_value(
    *,
    kubeconfig: str | None,
    namespace: str,
    secret_name: str,
    key: str,
) -> str:
    try:
        completed = kubectl(
            kubeconfig=kubeconfig,
            args=[
                "-n",
                namespace,
                "get",
                "secret",
                secret_name,
                "-o",
                f"jsonpath={{.data.{key}}}",
            ],
        )
    except (OSError, subprocess.CalledProcessError) as exc:
        output = getattr(exc, "output", None)
        detail = _process_output(output) or str(exc)
        raise pytest.UsageError(
            f"Unable to load {key} from secret {namespace}/{secret_name}: {detail}"
        ) from exc
    value = base64.b64decode(completed.stdout.strip(), validate=True).decode().strip()
    if not value:
        raise pytest.UsageError(
            f"Secret {namespace}/{secret_name} did not contain {key}."
        )
    return value


def load_jsonpath(
    *,
    kubeconfig: str | None,
    namespace: str,
    resource: str,
    jsonpath: str,
) -> str:
    try:
        completed = kubectl(
            kubeconfig=kubeconfig,
            args=["-n", namespace, "get", resource, "-o", f"jsonpath={jsonpath}"],
        )
    except (OSError, subprocess.CalledProcessError) as exc:
        output = getattr(exc, "output", None)
        detail = _process_output(output) or str(exc)
        raise pytest.UsageError(
            f"Unable to load {jsonpath} from {namespace}/{resource}: {detail}"
        ) from exc
    return completed.stdout.strip()


def _process_output(output: object) -> str:
    if isinstance(output, bytes):
        return output.decode(errors="replace").strip()
    if isinstance(output, str):
        return output.strip()
    return ""
