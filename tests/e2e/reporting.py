from __future__ import annotations

import pytest

_E2E_CHECK_IDS = {
    "test_deployed_default_install_has_playable_demo_media": "e2e api.demo-media",
    "test_deployed_storage_object_lifecycle_and_async_delete": (
        "e2e api.storage-object-lifecycle"
    ),
    "test_deployed_rejects_duplicate_controlled_object_instance": (
        "e2e api.duplicate-controlled-object"
    ),
    "test_deployed_webhook_registration_and_event_status": (
        "e2e webhook.registration-delivery"
    ),
    "test_deployed_ui_ingress_authenticates_and_proxies_api": (
        "e2e ui.ingress-auth-proxy"
    ),
    "test_deployed_cert_manager_certificates_are_ready": "e2e platform.certificates",
    "test_deployed_oauth2_client_credentials_token_grants_api_access": (
        "e2e auth.oauth2-client-scoped"
    ),
    "test_deployed_oauth2_client_credentials_without_scope_grants_api_access": (
        "e2e auth.oauth2-client-default"
    ),
    "test_deployed_oidc_metadata_and_jwks_are_ready": "e2e auth.oidc-discovery",
    "test_deployed_ui_ingest_uploads_and_registers_media": "e2e ui.ingest-upload",
    "test_deployed_ui_playback_preview_buffers_demo_media": "e2e ui.playback-preview",
    "test_operator_kind_zero_to_ready_api_ingest_and_ui_load": (
        "e2e operator.kind-install"
    ),
    "test_operator_upgrade_preserves_workload_pods_and_observes_generation": (
        "e2e operator.upgrade"
    ),
}


def pytest_runtest_protocol(item: pytest.Item, nextitem: pytest.Item | None) -> None:
    _ = nextitem
    terminal = item.config.pluginmanager.get_plugin("terminalreporter")
    message = _e2e_check_id(item.name)
    if terminal is not None:
        terminal.write_line(f"\n{message}")
    else:
        print(f"\n{message}")
    return


def _e2e_check_id(test_name: str) -> str:
    return _E2E_CHECK_IDS.get(
        test_name,
        f"e2e unclassified.{test_name.removeprefix('test_').replace('_', '-')}",
    )
