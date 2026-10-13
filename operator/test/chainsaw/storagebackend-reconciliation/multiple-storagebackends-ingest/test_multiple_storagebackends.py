import urllib.parse
import uuid

from media_requests import request

BACKENDS = [
    (
        "f1ab5b54-9703-42ed-b181-11ba1c794a7f",
        "tamoss.us-east-1:s3:tamoss",
    ),
    (
        "11111111-1111-5111-8111-111111111111",
        "tamoss.us-east-1:s3:archive",
    ),
]


def test_multiple_storagebackends_ingest():
    service_backends = request("GET", "/service/storage-backends")
    service_ids = {item["id"] for item in service_backends}
    for backend_id, _label in BACKENDS:
        if backend_id not in service_ids:
            raise SystemExit(f"backend {backend_id} missing from service list")

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
        put = media_object["put_url"]
        headers = dict(put.get("headers") or {})
        if content_type := put.get("content-type"):
            headers["Content-Type"] = content_type
        request("PUT", put["url"], body, headers)
        request(
            "POST",
            f"/flows/{flow_id}/segments",
            [
                {
                    "object_id": object_id,
                    "timerange": f"[{index}:0_{index + 1}:0)",
                }
            ],
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
            raise SystemExit(
                f"expected one segment for {backend_id}, got {len(segments)}"
            )
        segment = segments[0]
        if segment["object_id"] != object_id:
            raise SystemExit(f"unexpected segment object for {backend_id}")
        get_urls = segment.get("get_urls") or []
        if len(get_urls) != 1:
            raise SystemExit(
                f"expected one get_url for {backend_id}, got {len(get_urls)}"
            )
        get_url = get_urls[0]
        if get_url.get("storage_id") != backend_id or get_url.get("label") != label:
            raise SystemExit(f"segment mapped to wrong backend: {backend_id}")
        if request("GET", get_url["url"]) != body:
            raise SystemExit(f"read-back payload mismatch for {backend_id}")
