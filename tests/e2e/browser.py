from __future__ import annotations

import os
from collections.abc import Iterator

import pytest
from playwright.sync_api import Browser, sync_playwright


@pytest.fixture(scope="session")
def e2e_browser() -> Iterator[Browser]:
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=_browser_headless())
        try:
            yield browser
        finally:
            browser.close()


def _browser_headless() -> bool:
    return os.getenv("TAMOSS_E2E_HEADED", "").lower() not in {"1", "true", "yes"}
