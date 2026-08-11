from __future__ import annotations

import os
import shutil
from collections.abc import Iterator

import pytest
from playwright.sync_api import Browser, sync_playwright


@pytest.fixture(scope="session")
def e2e_browser() -> Iterator[Browser]:
    with sync_playwright() as playwright:
        executable_path = _browser_executable()
        browser = playwright.chromium.launch(
            headless=_browser_headless(),
            **({"executable_path": executable_path} if executable_path else {}),
        )
        try:
            yield browser
        finally:
            browser.close()


def _browser_headless() -> bool:
    return os.getenv("TAMOSS_E2E_HEADED", "").lower() not in {"1", "true", "yes"}


def _browser_executable() -> str | None:
    if "TAMOSS_E2E_BROWSER_EXECUTABLE" in os.environ:
        return os.environ["TAMOSS_E2E_BROWSER_EXECUTABLE"] or None
    return shutil.which("google-chrome")
