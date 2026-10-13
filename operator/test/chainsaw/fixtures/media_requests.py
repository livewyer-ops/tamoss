import json
from urllib.error import HTTPError
from urllib.parse import urlparse
from urllib.request import Request, urlopen

BASE = "http://tamoss-api:8000"


def request(method, url, payload=None, headers=None):
    if url.startswith("/"):
        url = BASE + url
    if urlparse(url).scheme not in {"http", "https"}:
        raise AssertionError("media request requires an HTTP(S) URL")
    headers = dict(headers or {})
    if payload is not None and not isinstance(payload, bytes):
        payload = json.dumps(payload).encode()
        headers["Content-Type"] = "application/json"
    try:
        # Only HTTP(S) URLs are accepted above.
        with urlopen(  # nosec B310
            Request(url, data=payload, headers=headers, method=method), timeout=30
        ) as response:
            data = response.read()
            return (
                json.loads(data)
                if data and response.headers.get_content_type() == "application/json"
                else data
            )
    except HTTPError as error:
        raise AssertionError(
            f"HTTP operation failed with status {error.code}"
        ) from None
