from __future__ import annotations

import os
from pathlib import Path

import pytest

from tests.e2e import browser


def test_browser_executable_prefers_explicit_override(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("TAMOSS_E2E_BROWSER_EXECUTABLE", "/opt/chrome")

    assert browser._browser_executable() == "/opt/chrome"


def test_empty_browser_executable_forces_playwright_chromium(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("TAMOSS_E2E_BROWSER_EXECUTABLE", "")

    assert browser._browser_executable() is None


def test_browser_executable_ignores_unpinned_chrome_on_path(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    monkeypatch.delenv("TAMOSS_E2E_BROWSER_EXECUTABLE", raising=False)
    system_chrome = tmp_path / "google-chrome"
    system_chrome.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
    system_chrome.chmod(0o755)
    monkeypatch.setenv("PATH", str(tmp_path), prepend=os.pathsep)

    assert browser._browser_executable() is None
