import json
from http.client import HTTPException
from urllib.error import HTTPError
from urllib.parse import urlparse
from urllib.request import Request, urlopen

BASE = "http://tamoss-api:8000"


def request(method, url, payload=None, headers=None):
    if url.startswith("/"):
        url = BASE + url
    try:
        scheme = urlparse(url).scheme
    except ValueError:
        raise AssertionError("media request requires a valid HTTP(S) URL") from None
    if scheme not in {"http", "https"}:
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
            is_json = response.headers.get_content_type() == "application/json"
    except HTTPError as error:
        raise AssertionError(
            f"HTTP operation failed with status {error.code}"
        ) from None
    except HTTPException, OSError, ValueError:
        raise AssertionError("HTTP operation failed") from None
    return json.loads(data) if data and is_json else data
