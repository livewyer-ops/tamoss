#!/usr/bin/env sh
set -eu

namespace="${NAMESPACE:?NAMESPACE is required}"

kubectl -n "$namespace" exec deployment/tamoss-api -- python - <<'PY'
import json
import urllib.error
import urllib.parse
import urllib.request
import uuid

BASE = "http://127.0.0.1:8000"
DEFAULT_BACKEND_ID = "f1ab5b54-9703-42ed-b181-11ba1c794a7f"
ARCHIVE_BACKEND_ID = "11111111-1111-5111-8111-111111111111"
BACKENDS = [
    (DEFAULT_BACKEND_ID, "tamoss.us-east-1:s3:tamoss"),
    (ARCHIVE_BACKEND_ID, "tamoss.us-east-1:s3:archive"),
]


def request(method, path, payload=None):
    data = None
    headers = {}
    if payload is not None:
        data = json.dumps(payload).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=20) as response:
            body = response.read()
            if not body:
                return None
            return json.loads(body.decode())
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode(errors="replace")
        raise SystemExit(f"{method} {path} failed: {exc.code} {detail}") from exc


def upload(put_request, payload):
    headers = dict(put_request.get("headers") or {})
    content_type = put_request.get("content-type")
    if content_type:
        headers["Content-Type"] = content_type
    req = urllib.request.Request(
        put_request["url"],
        data=payload,
        headers=headers,
        method="PUT",
    )
    with urllib.request.urlopen(req, timeout=20) as response:
        response.read()


def read_back(get_url):
    with urllib.request.urlopen(get_url, timeout=20) as response:
        return response.read()


service_backends = request("GET", "/service/storage-backends")
service_ids = {item["id"] for item in service_backends}
for backend_id, _label in BACKENDS:
    if backend_id not in service_ids:
        raise SystemExit(f"backend {backend_id} missing from service list: {service_backends!r}")

for index, (backend_id, label) in enumerate(BACKENDS):
    flow_id = str(uuid.uuid4())
    source_id = str(uuid.uuid4())
    object_id = f"chainsaw/{backend_id}.ts"
    body = f"tamoss chainsaw payload {backend_id}\n".encode()

    request(
        "PUT",
        f"/flows/{flow_id}",
        {
            "id": flow_id,
            "source_id": source_id,
            "format": "urn:x-nmos:format:video",
            "codec": "video/h264",
            "container": "video/mp2t",
            "essence_parameters": {
                "frame_width": 1920,
                "frame_height": 1080,
                "frame_rate": {"numerator": 25, "denominator": 1},
            },
        },
    )

    allocation = request(
        "POST",
        f"/flows/{flow_id}/storage",
        {"object_ids": [object_id], "storage_id": backend_id},
    )
    media_object = allocation["media_objects"][0]
    upload(media_object["put_url"], body)

    request(
        "POST",
        f"/flows/{flow_id}/segments",
        [{"object_id": object_id, "timerange": f"[{index}:0_{index + 1}:0)"}],
    )

    query = urllib.parse.urlencode(
        {
            "accept_storage_ids": backend_id,
            "presigned": "true",
            "verbose_storage": "true",
        }
    )
    segments = request("GET", f"/flows/{flow_id}/segments?{query}")
    if len(segments) != 1:
        raise SystemExit(f"expected one segment for {backend_id}, got {segments!r}")
    segment = segments[0]
    if segment["object_id"] != object_id:
        raise SystemExit(f"unexpected segment object for {backend_id}: {segment!r}")
    get_urls = segment.get("get_urls") or []
    if len(get_urls) != 1:
        raise SystemExit(f"expected one get_url for {backend_id}, got {segment!r}")
    get_url = get_urls[0]
    if get_url.get("storage_id") != backend_id or get_url.get("label") != label:
        raise SystemExit(f"segment mapped to wrong backend: {segment!r}")
    if read_back(get_url["url"]) != body:
        raise SystemExit(f"read-back payload mismatch for {backend_id}")
PY
