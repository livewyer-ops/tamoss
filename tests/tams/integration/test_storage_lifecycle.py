from __future__ import annotations

from collections.abc import Iterator
from uuid import UUID, uuid4

import psycopg
import pytest
import requests
from botocore.exceptions import BotoCoreError, ClientError
from fastapi.testclient import TestClient
from psycopg import sql
from tamoss.adapters.object_storage import ConfiguredObjectStorage
from tamoss.adapters.postgres import PostgresRepository
from tamoss.app import create_app
from tamoss.application.use_cases import TamossUseCases
from tamoss.domain.model import StorageBackend
from tamoss.settings import Settings

from tests.adapters.postgres.support import (
    SCHEMA_ASSETS_DIR,
    database_url,
    execute_sql_file,
)
from tests.support.s3_storage import (
    checksum_value,
    empty_and_delete_bucket,
    ensure_bucket,
    s3_backend_record,
    s3_client,
    s3_settings_backend,
)
from tests.tams.support import (
    allocate_objects,
    create_video_flow,
    segment_payload,
)

pytestmark = [
    pytest.mark.tams_conformance,
    pytest.mark.needs_db,
    pytest.mark.needs_s3,
]


@pytest.fixture()
def postgres_connection() -> Iterator[psycopg.Connection]:
    schema = f"tamoss_tams_conformance_{uuid4().hex}"
    try:
        admin = psycopg.connect(database_url(), connect_timeout=2)
    except psycopg.OperationalError as exc:
        pytest.skip(f"Postgres test database is unavailable: {exc}")
    admin.autocommit = True
    with admin.cursor() as cur:
        cur.execute(sql.SQL("CREATE SCHEMA {}").format(sql.Identifier(schema)))

    conn = psycopg.connect(database_url(), connect_timeout=2)
    conn.autocommit = True
    with conn.cursor() as cur:
        cur.execute(sql.SQL("SET search_path TO {}").format(sql.Identifier(schema)))
    execute_sql_file(conn, SCHEMA_ASSETS_DIR / "schema.sql")

    try:
        yield conn
    finally:
        conn.close()
        with admin.cursor() as cur:
            cur.execute(
                sql.SQL("DROP SCHEMA IF EXISTS {} CASCADE").format(
                    sql.Identifier(schema)
                )
            )
        admin.close()


@pytest.fixture()
def s3_backend() -> Iterator[StorageBackend]:
    backend = s3_backend_record(
        id=UUID("44444444-4444-4444-8444-444444444444"),
        label="tamoss.storage.conformance",
        bucket_name=f"tamoss-conformance-{uuid4().hex[:12]}",
    )
    try:
        s3_client(backend).list_buckets()
    except (BotoCoreError, ClientError) as exc:
        pytest.skip(f"S3-compatible test endpoint is unavailable: {exc}")
    ensure_bucket(backend)
    try:
        yield backend
    finally:
        empty_and_delete_bucket(backend)


@pytest.fixture()
def real_storage_client(
    postgres_connection: psycopg.Connection,
    s3_backend: StorageBackend,
) -> Iterator[TestClient]:
    settings = Settings(
        auth_required=False,
        s3_presign_ttl_seconds=120,
        s3_connect_timeout_seconds=2,
        s3_read_timeout_seconds=2,
        storage_backend=s3_settings_backend(s3_backend),
    )
    object_storage = ConfiguredObjectStorage(settings)
    repository = PostgresRepository(
        connection=postgres_connection,
        storage_backend=s3_backend,
        register_storage_backend=True,
    )
    app = create_app(
        settings,
        use_cases=TamossUseCases(
            repository=repository,
            object_storage=object_storage,
            settings=settings,
        ),
    )
    with TestClient(app) as client:
        yield client


def test_allocated_put_url_validates_content_md5(
    real_storage_client: TestClient,
    s3_backend: StorageBackend,
) -> None:
    flow_id, _, _ = create_video_flow(real_storage_client)
    body = b"tamoss checksum passthrough\n"
    object_id = f"tams/conformance/{uuid4()}/checksum.ts"
    bad_object_id = f"tams/conformance/{uuid4()}/bad-checksum.ts"
    put_url = allocate_objects(real_storage_client, flow_id, [object_id])[0]["put_url"]
    headers = dict(put_url["headers"])
    headers["Content-MD5"] = checksum_value(body, "md5")

    put_response = requests.put(put_url["url"], data=body, headers=headers, timeout=5)
    assert put_response.status_code in {200, 201, 204}, put_response.text
    registered = real_storage_client.post(
        f"/flows/{flow_id}/segments", json=segment_payload(object_id, "[0:0_10:0)")
    )
    assert registered.status_code == 201, registered.text

    bad_put_url = allocate_objects(real_storage_client, flow_id, [bad_object_id])[0][
        "put_url"
    ]
    bad_headers = dict(bad_put_url["headers"])
    bad_headers["Content-MD5"] = checksum_value(b"different body", "md5")
    bad_put_response = requests.put(
        bad_put_url["url"], data=body, headers=bad_headers, timeout=5
    )
    assert bad_put_response.status_code == 400, bad_put_response.text
    assert "BadDigest" in bad_put_response.text
    storage = real_storage_client.app.state.tamoss_use_cases.object_storage
    assert storage.object_metadata(bad_object_id, backend=s3_backend) is None
    missing_object = real_storage_client.post(
        f"/flows/{flow_id}/segments", json=segment_payload(bad_object_id, "[10:0_20:0)")
    )
    assert missing_object.status_code == 400


def test_stale_allocation_cleanup_removes_postgres_row_and_stored_bytes(
    real_storage_client: TestClient,
    postgres_connection: psycopg.Connection,
    s3_backend: StorageBackend,
) -> None:
    use_cases = real_storage_client.app.state.tamoss_use_cases
    flow_id, _, _ = create_video_flow(real_storage_client)
    object_id = f"tams/conformance/{uuid4()}/stale-allocation.ts"
    body = b"stale allocation bytes\n"

    allocation = allocate_objects(real_storage_client, flow_id, [object_id])[0]
    put_url = allocation["put_url"]
    uploaded = requests.put(
        put_url["url"],
        data=body,
        headers=put_url["headers"],
        timeout=5,
    )
    assert uploaded.status_code in {200, 201, 204}, uploaded.text
    assert use_cases.object_storage.read(object_id, backend=s3_backend) == body

    media_object = use_cases.repository.object_repository.get_object(object_id)
    assert media_object is not None
    with postgres_connection.cursor() as cur:
        cur.execute(
            """
            UPDATE tamoss_media_objects
            SET created_at = NOW() - (%s * INTERVAL '1 second')
            WHERE id = %s
            """,
            (use_cases.settings.min_object_timeout_seconds() + 1, object_id),
        )

    queued = use_cases.deletion.queue_stale_allocated_object_cleanups(max_objects=10)
    processed = use_cases.deletion.process_pending_object_cleanups(
        worker_id="cleanup-worker",
        lease_seconds=60,
    )
    completed_cleanups = use_cases.repository.deletion_repository.list_object_cleanups(
        statuses={"done"}
    )

    assert queued == 1
    assert processed == 1
    assert use_cases.repository.object_repository.get_object(object_id) is None
    assert use_cases.object_storage.read(object_id, backend=s3_backend) is None
    assert [cleanup.object_id for cleanup in completed_cleanups] == [object_id]


def test_http_validation_preserves_persisted_media(
    real_storage_client: TestClient,
) -> None:
    flow_id, _, original = create_video_flow(real_storage_client)
    object_id = f"validation/{flow_id}.ts"
    put_url = allocate_objects(real_storage_client, flow_id, [object_id])[0]["put_url"]
    uploaded = requests.put(
        put_url["url"], data=b"validation", headers=put_url["headers"], timeout=5
    )
    assert uploaded.status_code in {200, 201, 204}
    registered = real_storage_client.post(
        f"/flows/{flow_id}/segments",
        json=segment_payload(object_id, "[1000000000:1_1000000000:2)"),
    )
    assert registered.status_code == 201
    flow_path = f"/flows/{flow_id}"
    before = real_storage_client.get(flow_path).json()
    for name in ("max_bit_rate", "avg_bit_rate"):
        assert (
            real_storage_client.put(f"{flow_path}/{name}", json=True).status_code == 400
        )
    assert real_storage_client.put(flow_path, json={}).status_code == 400
    assert real_storage_client.get(flow_path).json() == before
    assert before["id"] == original["id"]
    for method in ("GET", "HEAD"):
        assert (
            real_storage_client.request(
                method, "/flows", params={"codec": "invalid"}
            ).status_code
            == 400
        )
        assert (
            real_storage_client.request(
                method, f"{flow_path}/segments", params={"timerange": "(1000000000:1)"}
            ).status_code
            == 400
        )
    assert (
        real_storage_client.delete(
            f"{flow_path}/segments", params={"timerange": "(1000000000:1)"}
        ).status_code
        == 400
    )
    segments = real_storage_client.get(
        f"{flow_path}/segments", params={"timerange": "[1000000000:1]"}
    )
    assert segments.status_code == 200
    assert len(segments.json()) == 1
    media_object = real_storage_client.get(
        f"/objects/{object_id}", params={"presigned": True}
    )
    downloaded = requests.get(media_object.json()["get_urls"][0]["url"], timeout=5)
    assert downloaded.status_code == 200
    assert downloaded.content == b"validation"
