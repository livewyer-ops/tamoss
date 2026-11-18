#!/usr/bin/env python3
"""Refuse to overwrite a published release; fail closed on API errors."""

from __future__ import annotations

import json
import os
from http.client import HTTPSConnection
from urllib.parse import quote, urlsplit


def verify_unpublished(*, api_url: str, repository: str, tag: str, token: str) -> None:
    parsed = urlsplit(api_url)
    if (
        parsed.scheme != "https"
        or not parsed.hostname
        or parsed.username
        or parsed.password
        or parsed.query
        or parsed.fragment
    ):
        raise ValueError("GitHub API URL must be an HTTPS origin with an optional path")
    path = (
        f"{parsed.path.rstrip('/')}/repos/{repository}/releases/tags/"
        f"{quote(tag, safe='')}"
    )
    connection = HTTPSConnection(parsed.hostname, parsed.port, timeout=30)
    try:
        connection.request(
            "GET",
            path,
            headers={
                "Authorization": f"Bearer {token}",
                "Accept": "application/vnd.github+json",
                "User-Agent": "tamoss-release-preflight",
            },
        )
        response = connection.getresponse()
        if response.status == 404:
            return
        if response.status != 200:
            raise SystemExit(
                f"GitHub release lookup failed with HTTP {response.status}"
            )
        release = json.load(response)
    finally:
        connection.close()
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
