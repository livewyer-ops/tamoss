import importlib.util
from pathlib import Path

from tests.support.fixtures import load_json_fixture

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts" / "support_bundle.py"


def load_support_bundle_module():
    spec = importlib.util.spec_from_file_location("support_bundle", SCRIPT)
    assert spec is not None
    assert spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_support_bundle_redaction_matches_golden_fixture():
    support_bundle = load_support_bundle_module()
    source = load_json_fixture("support_bundle_redaction/input.json")
    expected = load_json_fixture("support_bundle_redaction/expected.json")

    actual = support_bundle.redact_document(source)

    assert actual == expected


def test_support_bundle_redacts_sensitive_log_text():
    support_bundle = load_support_bundle_module()

    actual = support_bundle.redact_text(
        "POSTGRES_PASSWORD=secret AWS_SECRET_ACCESS_KEY='secret' "
        "api_key_value=hook-secret Authorization: Bearer token-value ok=true"
    )

    assert (
        actual == "POSTGRES_PASSWORD=<redacted> AWS_SECRET_ACCESS_KEY=<redacted> "
        "api_key_value=<redacted> Authorization: Bearer <redacted> ok=true"
    )


def test_support_bundle_redacts_webhook_api_key_values():
    support_bundle = load_support_bundle_module()

    actual = support_bundle.redact_document(
        load_json_fixture("support_bundle_redaction/webhook_api_key_input.json")
    )

    assert actual == load_json_fixture(
        "support_bundle_redaction/webhook_api_key_expected.json"
    )


def test_support_bundle_summarizes_tamoss_versions():
    support_bundle = load_support_bundle_module()

    summary = support_bundle.tamoss_version_summary(
        load_json_fixture("support_bundle_redaction/tamoss_versions_input.json")
    )

    assert summary == load_json_fixture(
        "support_bundle_redaction/tamoss_versions_expected.json"
    )


def test_support_bundle_summarizes_first_start_lifecycle():
    support_bundle = load_support_bundle_module()

    summary = support_bundle.first_start_summary(
        load_json_fixture("support_bundle_redaction/first_start_tamoss_input.json"),
        load_json_fixture("support_bundle_redaction/first_start_storage_input.json"),
    )

    assert summary == load_json_fixture(
        "support_bundle_redaction/first_start_expected.json"
    )
