#!/usr/bin/env python3
"""Refuse to overwrite a published release; fail closed on API errors."""

from __future__ import annotations

import json
import os
from urllib.error import HTTPError
from urllib.parse import quote
from urllib.request import Request, urlopen


def verify_unpublished(*, api_url: str, repository: str, tag: str, token: str) -> None:
    url = f"{api_url}/repos/{repository}/releases/tags/{quote(tag, safe='')}"
    request = Request(
        url,
        headers={
            "Authorization": f"Bearer {token}",
            "Accept": "application/vnd.github+json",
        },
    )
    try:
        with urlopen(request, timeout=30) as response:
            release = json.load(response)
    except HTTPError as exc:
        if exc.code == 404:
            return
        raise
    if release.get("draft") is not True:
        raise SystemExit(f"Release {tag} is already published. Use a new version tag.")


def main() -> None:
    if os.environ["GITHUB_REF_TYPE"] != "tag":
        raise SystemExit("Release publication requires a version tag.")
    verify_unpublished(
        api_url=os.environ.get("GITHUB_API_URL", "https://api.github.com"),
        repository=os.environ["GITHUB_REPOSITORY"],
        tag=os.environ["GITHUB_REF_NAME"],
        token=os.environ["GH_TOKEN"],
    )


if __name__ == "__main__":
    main()
