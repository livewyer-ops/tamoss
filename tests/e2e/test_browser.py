from __future__ import annotations

import pytest

from tests.e2e import browser


def test_browser_executable_prefers_explicit_override(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("TAMOSS_E2E_BROWSER_EXECUTABLE", "/opt/chrome")
    monkeypatch.setattr(browser.shutil, "which", lambda _name: "/usr/bin/chrome")

    assert browser._browser_executable() == "/opt/chrome"


def test_empty_browser_executable_forces_playwright_chromium(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("TAMOSS_E2E_BROWSER_EXECUTABLE", "")
    monkeypatch.setattr(browser.shutil, "which", lambda _name: "/usr/bin/chrome")

    assert browser._browser_executable() is None


def test_browser_executable_uses_installed_stable_chrome(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.delenv("TAMOSS_E2E_BROWSER_EXECUTABLE", raising=False)
    monkeypatch.setattr(browser.shutil, "which", lambda _name: "/usr/bin/chrome")

    assert browser._browser_executable() == "/usr/bin/chrome"
