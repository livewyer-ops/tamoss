import os
import time
import uuid
from pathlib import Path
from urllib.parse import parse_qs, quote, urlparse

import boto3
from botocore import UNSIGNED
from botocore.config import Config
from botocore.exceptions import ClientError
from media_requests import request

DEFAULT = "f1ab5b54-9703-42ed-b181-11ba1c794a7f"
ADDITIONAL = "11111111-1111-5111-8111-111111111111"


def test_storage_credentials():
    expect_session = os.environ["EXPECT_SESSION"] == "true"
    sts = boto3.client(
        "sts",
        endpoint_url="http://rustfs-svc:9000",
        region_name="us-east-1",
        config=Config(signature_version=UNSIGNED),
    )

    def assume(token):
        return sts.assume_role_with_web_identity(
            RoleArn="arn:aws:iam::123456789012:role/tamoss",
            RoleSessionName="tamoss-test",
            WebIdentityToken=token,
        )["Credentials"]

    if expect_session:
        for name in ("expired", "wrong-audience", "wrong-issuer"):
            try:
                assume(Path("/identity", name).read_text())
            except ClientError as error:
                if error.response["Error"]["Code"] != "AccessDenied":
                    raise AssertionError(
                        f"STS rejection for {name} was not AccessDenied"
                    ) from None
            else:
                raise AssertionError(f"STS accepted {name} token")
        token = Path("/identity/token").read_text()
        parts = token.split(".")
        parts[2] = ("A" if parts[2][0] != "A" else "B") + parts[2][1:]
        try:
            assume(".".join(parts))
        except ClientError as error:
            if error.response["Error"]["Code"] != "AccessDenied":
                raise AssertionError(
                    "STS signature rejection was not AccessDenied"
                ) from None
        else:
            raise AssertionError("STS accepted invalid signature")
        credentials = assume(Path("/identity/readonly").read_text())
        readonly = boto3.client(
            "s3",
            endpoint_url="http://rustfs-svc:9000",
            region_name="us-east-1",
            aws_access_key_id=credentials["AccessKeyId"],
            aws_secret_access_key=credentials["SecretAccessKey"],
            aws_session_token=credentials["SessionToken"],
        )
        try:
            readonly.put_object(Bucket="tamoss", Key="denied-object", Body=b"denied")
        except ClientError as error:
            assert error.response["Error"]["Code"] == "AccessDenied"
        else:
            raise AssertionError("readonly identity wrote an object")

    admin = boto3.client(
        "s3",
        endpoint_url="http://rustfs-svc:9000",
        region_name="us-east-1",
        aws_access_key_id=os.environ["FIXTURE_ACCESS_KEY"],
        aws_secret_access_key=os.environ["FIXTURE_SECRET_KEY"],
    )
    for source, destination in ((DEFAULT, ADDITIONAL), (ADDITIONAL, DEFAULT)):
        flow = str(uuid.uuid4())
        object_id = "identity/" + str(uuid.uuid4())
        body = b"web identity media roundtrip\n"
        request(
            "PUT",
            f"/flows/{flow}",
            {
                "id": flow,
                "source_id": str(uuid.uuid4()),
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
            f"/flows/{flow}/storage",
            {"object_ids": [object_id], "storage_id": source},
        )
        put = allocation["media_objects"][0]["put_url"]
        query = parse_qs(urlparse(put["url"]).query)
        assert ("X-Amz-Security-Token" in query) == (
            expect_session and source == DEFAULT
        )
        request("PUT", put["url"], body, put.get("headers"))
        request(
            "POST",
            f"/flows/{flow}/segments",
            [{"object_id": object_id, "timerange": "[0:0_1:0)"}],
        )
        path = "/objects/" + quote(object_id, safe="")
        media = request("GET", path + "?presigned=true")
        assert request("GET", media["get_urls"][0]["url"]) == body
        request("POST", path + "/instances", {"storage_id": destination})
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            media = request("GET", path + "?presigned=true&verbose_storage=true")
            urls = {
                entry["storage_id"]: entry["url"] for entry in media.get("get_urls", [])
            }
            if source in urls and destination in urls:
                break
            time.sleep(1)
        else:
            raise AssertionError("worker copy did not complete")
        for backend, url in urls.items():
            assert request("GET", url) == body
            assert ("X-Amz-Security-Token" in parse_qs(urlparse(url).query)) == (
                expect_session and backend == DEFAULT
            )
        deleted = request("DELETE", f"/flows/{flow}")
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            status = request("GET", "/flow-delete-requests/" + deleted["id"])["status"]
            assert status != "error"
            if status == "done":
                break
            time.sleep(1)
        else:
            raise AssertionError("worker deletion did not complete")
        for bucket in ("tamoss", "archive"):
            try:
                admin.head_object(Bucket=bucket, Key=object_id)
            except ClientError as error:
                assert error.response["ResponseMetadata"]["HTTPStatusCode"] == 404
            else:
                raise AssertionError("media object remains after deletion")
