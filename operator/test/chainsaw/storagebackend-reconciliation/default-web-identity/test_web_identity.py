import json
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
RETAINED_FLOW = "8eecb6ee-59e0-4398-9961-719bdb8ee785"
RETAINED_OBJECT = "identity/retained-media"
BODY = b"web identity media roundtrip\n"


def test_storage_credentials():
    expect_session = os.environ["EXPECT_SESSION"] == "true"
    sts = boto3.client(
        "sts",
        endpoint_url="http://rustfs-svc:9000",
        region_name="us-east-1",
        config=Config(signature_version=UNSIGNED),
    )

    def assume(token, **kwargs):
        return sts.assume_role_with_web_identity(
            RoleArn="arn:aws:iam::123456789012:role/tamoss",
            RoleSessionName="tamoss-test",
            WebIdentityToken=token,
            **kwargs,
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
        readonly = session_client(credentials)
        assert_denied(
            readonly.put_object, Bucket="tamoss", Key="denied-object", Body=b"denied"
        )

    admin = static_client("FIXTURE")
    archive = static_client("ARCHIVE")
    default = (
        session_client(assume(Path("/identity/token").read_text()))
        if expect_session
        else static_client("DEFAULT")
    )
    for client, other_bucket in ((default, "archive"), (archive, "tamoss")):
        key = "identity/denied-" + str(uuid.uuid4())
        assert_denied(client.put_object, Bucket=other_bucket, Key=key, Body=b"denied")
        assert_denied(client.delete_object, Bucket=other_bucket, Key=key)
        assert_absent(admin, other_bucket, key)

    if expect_session:
        restricted = session_client(
            assume(
                Path("/identity/token").read_text(),
                Policy=json.dumps(
                    {
                        "Version": "2012-10-17",
                        "Statement": [
                            {
                                "Effect": "Allow",
                                "Action": ["s3:GetObject", "s3:PutObject"],
                                "Resource": ["arn:aws:s3:::tamoss/*"],
                            }
                        ],
                    }
                ),
            )
        )
        key = "identity/source-read-" + str(uuid.uuid4())
        restricted.put_object(Bucket="tamoss", Key=key, Body=BODY)
        assert restricted.head_object(Bucket="tamoss", Key=key)["ContentLength"] == len(
            BODY
        )
        default.delete_object(Bucket="tamoss", Key=key)
        assert_absent(admin, "tamoss", key)
        archive.put_object(Bucket="archive", Key=key, Body=BODY)
        assert archive.head_object(Bucket="archive", Key=key)["ContentLength"] == len(
            BODY
        )
        assert_denied(
            restricted.copy,
            {"Bucket": "archive", "Key": key},
            "tamoss",
            key,
            SourceClient=archive,
        )
        assert_absent(admin, "tamoss", key)
        archive.delete_object(Bucket="archive", Key=key)
        assert_absent(admin, "archive", key)

    for source, destination in ((DEFAULT, ADDITIONAL), (ADDITIONAL, DEFAULT)):
        flow = str(uuid.uuid4())
        object_id = "identity/" + str(uuid.uuid4())
        create_media(flow, object_id, source, destination, expect_session)
        verify_media(flow, object_id, expect_session)
        delete_media(flow, object_id, admin)


def test_retained_media():
    phase = os.environ["TEST_PHASE"]
    assert phase in {"web", "secret", "reenabled"}
    expect_session = os.environ["EXPECT_SESSION"] == "true"
    if phase == "web":
        create_media(
            RETAINED_FLOW, RETAINED_OBJECT, DEFAULT, ADDITIONAL, expect_session
        )
    verify_media(RETAINED_FLOW, RETAINED_OBJECT, expect_session)
    if phase == "reenabled":
        delete_media(RETAINED_FLOW, RETAINED_OBJECT, static_client("FIXTURE"))


def static_client(prefix):
    return boto3.client(
        "s3",
        endpoint_url="http://rustfs-svc:9000",
        region_name="us-east-1",
        aws_access_key_id=os.environ[prefix + "_ACCESS_KEY"],
        aws_secret_access_key=os.environ[prefix + "_SECRET_KEY"],
    )


def session_client(credentials):
    return boto3.client(
        "s3",
        endpoint_url="http://rustfs-svc:9000",
        region_name="us-east-1",
        aws_access_key_id=credentials["AccessKeyId"],
        aws_secret_access_key=credentials["SecretAccessKey"],
        aws_session_token=credentials["SessionToken"],
    )


def assert_denied(operation, *args, **kwargs):
    try:
        operation(*args, **kwargs)
    except ClientError as error:
        if error.response["Error"]["Code"] != "AccessDenied":
            raise AssertionError("S3 rejection was not AccessDenied") from None
    else:
        raise AssertionError("S3 operation unexpectedly allowed")


def assert_absent(admin, bucket, object_id):
    try:
        admin.head_object(Bucket=bucket, Key=object_id)
    except ClientError as error:
        if error.response["ResponseMetadata"]["HTTPStatusCode"] != 404:
            raise AssertionError("object absence check did not return 404") from None
    else:
        raise AssertionError("media object remains after deletion or denied copy")


def create_media(flow, object_id, source, destination, expect_session):
    request(
        "PUT",
        f"/flows/{flow}",
        {
            "id": flow,
            "source_id": flow,
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
    assert ("X-Amz-Security-Token" in query) == (expect_session and source == DEFAULT)
    request("PUT", put["url"], BODY, put.get("headers"))
    request(
        "POST",
        f"/flows/{flow}/segments",
        [{"object_id": object_id, "timerange": "[0:0_1:0)"}],
    )
    path = "/objects/" + quote(object_id, safe="")
    media = request("GET", path + "?presigned=true")
    assert request("GET", media["get_urls"][0]["url"]) == BODY
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


def verify_media(flow, object_id, expect_session):
    metadata = request("GET", f"/flows/{flow}?include_timerange=true")
    assert metadata["id"] == flow
    assert metadata["source_id"] == flow
    assert metadata["timerange"] == "[0:0_1:0)"
    segments = request("GET", f"/flows/{flow}/segments")
    assert len(segments) == 1
    assert segments[0]["object_id"] == object_id
    assert segments[0]["timerange"] == "[0:0_1:0)"
    path = "/objects/" + quote(object_id, safe="")
    media = request("GET", path + "?presigned=true&verbose_storage=true")
    urls = {entry["storage_id"]: entry["url"] for entry in media["get_urls"]}
    assert set(urls) == {DEFAULT, ADDITIONAL}
    for backend, url in urls.items():
        assert request("GET", url) == BODY
        assert ("X-Amz-Security-Token" in parse_qs(urlparse(url).query)) == (
            expect_session and backend == DEFAULT
        )


def delete_media(flow, object_id, admin):
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
        assert_absent(admin, bucket, object_id)
