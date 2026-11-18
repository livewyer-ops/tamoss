from __future__ import annotations

import importlib.util
from pathlib import Path
from types import ModuleType

import pytest

ROOT = Path(__file__).resolve().parents[1]


def _policy_module() -> ModuleType:
    path = ROOT / ".tasks/lib/chainsaw_labels.py"
    spec = importlib.util.spec_from_file_location("chainsaw_labels", path)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _write_test(tmp_path: Path, operation: str) -> Path:
    path = tmp_path / "chainsaw-test.yaml"
    path.write_text(
        "apiVersion: chainsaw.kyverno.io/v1alpha1\n"
        "kind: Test\n"
        "spec:\n"
        "  steps:\n"
        "    - try:\n"
        f"{operation}",
        encoding="utf-8",
    )
    return path


@pytest.mark.parametrize("platform", [None, "managed", "simulated", "typo"])
def test_optional_platform_label_is_validated(
    tmp_path: Path, platform: str | None
) -> None:
    module = _policy_module()
    labels = {axis: sorted(values)[0] for axis, values in module.AXES.items()}
    labels.update(target="kind", profile="none")
    if platform is not None:
        labels["platform"] = platform
    (tmp_path / "chainsaw-test.yaml").write_text(
        "apiVersion: chainsaw.kyverno.io/v1alpha1\n"
        "kind: Test\nmetadata:\n  labels:\n"
        + "".join(
            f"    test.tamoss.io/{axis}: {value}\n" for axis, value in labels.items()
        )
        + "spec:\n  steps: []\n"
    )

    assert module.validate_labels(tmp_path) == (1 if platform == "typo" else 0)


def test_inline_scripts_are_rejected(tmp_path: Path) -> None:
    module = _policy_module()
    path = _write_test(
        tmp_path,
        "        - script:\n            content: kubectl get pods\n",
    )

    failures = module._validate_native_operations(path)

    assert any("instead of script" in failure for failure in failures)


def test_shell_and_unchecked_commands_are_rejected(tmp_path: Path) -> None:
    module = _policy_module()
    path = _write_test(
        tmp_path,
        "        - command:\n"
        "            entrypoint: bash\n"
        "            args:\n"
        "              - -c\n"
        "              - kubectl wait deployment/example\n",
    )

    failures = module._validate_native_operations(path)

    assert any("shell command entrypoints" in failure for failure in failures)
    assert any("explicit check" in failure for failure in failures)


def test_shell_entrypoints_in_resource_fixtures_are_rejected(tmp_path: Path) -> None:
    module = _policy_module()
    resource = tmp_path / "resources" / "job.yaml"
    resource.parent.mkdir()
    resource.write_text(
        "apiVersion: batch/v1\n"
        "kind: Job\n"
        "spec:\n"
        "  template:\n"
        "    spec:\n"
        "      containers:\n"
        "        - name: check\n"
        "          command:\n"
        "            - /bin/sh\n",
        encoding="utf-8",
    )

    failures = module._validate_no_shell_resources(tmp_path)

    assert any("Chainsaw resources" in failure for failure in failures)


def test_missing_operation_file_references_are_rejected(tmp_path: Path) -> None:
    module = _policy_module()
    path = _write_test(
        tmp_path,
        "        - apply:\n            file: resources/missing.yaml\n",
    )

    failures = module._validate_file_references(path)

    assert any("referenced file does not exist" in failure for failure in failures)


def test_embedded_fixture_programmes_are_rejected(tmp_path: Path) -> None:
    module = _policy_module()
    resource = tmp_path / "fixture.yaml"
    resource.write_text(
        "apiVersion: v1\nkind: ConfigMap\ndata:\n"
        "  check.py: |\n    print('embedded')\n"
        "---\napiVersion: v1\nkind: Pod\nspec:\n"
        "  containers:\n    - name: fixture\n      command:\n"
        "        - /app/.venv/bin/python\n        - -u\n        - -c\n"
        "        - print('embedded')\n",
        encoding="utf-8",
    )

    failures = module._validate_no_shell_resources(tmp_path)

    assert any("keep fixture programmes in source files" in item for item in failures)
    assert any("invoke a Python source file" in item for item in failures)


def test_fixture_source_entrypoints_are_allowed(tmp_path: Path) -> None:
    module = _policy_module()
    resource = tmp_path / "fixture.yaml"
    resource.write_text(
        "apiVersion: v1\nkind: Pod\nspec:\n"
        "  containers:\n    - name: fixture\n      command:\n"
        "        - /app/.venv/bin/python\n        - /tests/issuer.py\n"
        "        - -c\n        - fixture.toml\n"
        "    - name: check\n      command:\n"
        "        - python\n        - -m\n        - pytest\n"
        "        - -c\n        - pytest.ini\n",
        encoding="utf-8",
    )

    assert module._validate_no_shell_resources(tmp_path) == []


def test_kubernetes_orchestration_commands_are_rejected(tmp_path: Path) -> None:
    module = _policy_module()
    path = _write_test(
        tmp_path,
        "        - command:\n"
        "            entrypoint: kubectl\n"
        "            args:\n"
        "              - patch\n"
        "              - deployment/example\n"
        "            check:\n"
        "              ($error == null): true\n",
    )

    failures = module._validate_native_operations(path)

    assert any(
        "exec, logs, and operator environment" in failure for failure in failures
    )


def test_inline_remote_scripts_are_rejected(tmp_path: Path) -> None:
    module = _policy_module()
    path = _write_test(
        tmp_path,
        "        - command:\n"
        "            entrypoint: kubectl\n"
        "            args:\n"
        "              - exec\n"
        "              - deployment/example\n"
        "              - --\n"
        "              - python\n"
        "              - -c\n"
        "              - print('not a direct executable')\n"
        "            check:\n"
        "              ($error == null): true\n",
    )

    failures = module._validate_native_operations(path)

    assert any(
        "exec, logs, and operator environment" in failure for failure in failures
    )


def test_documented_external_boundaries_are_allowed(tmp_path: Path) -> None:
    module = _policy_module()
    path = _write_test(
        tmp_path,
        "        - command:\n"
        "            entrypoint: kubectl\n"
        "            args:\n"
        "              - set\n"
        "              - env\n"
        "              - deployment/operator-controller-manager\n"
        "              - WATCH_NAMESPACES=tams\n"
        "            check:\n"
        "              ($error == null): true\n"
        "        - command:\n"
        "            entrypoint: kubectl\n"
        "            args:\n"
        "              - exec\n"
        "              - deployment/postgresql\n"
        "              - --\n"
        "              - psql\n"
        "            check:\n"
        "              ($error == null): true\n",
    )

    assert module._validate_native_operations(path) == []
