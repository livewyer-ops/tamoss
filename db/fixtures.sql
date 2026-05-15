-- TAMOSS deterministic demo data for local development and integration tests.
--
-- The rows preserve BBC API resource shapes in the JSONB records while keeping
-- SQL indexes focused on query keys used by the runtime repository.

INSERT INTO tamoss_storage_backends (
    id,
    label,
    provider,
    region,
    store_product,
    store_type,
    default_storage,
    bucket_name,
    endpoint_url,
    public_endpoint_url,
    record,
    updated_at
) VALUES (
    'f1ab5b54-9703-42ed-b181-11ba1c794a7f',
    'tamoss.us-east-1:s3:tamoss',
    'tamoss',
    'us-east-1',
    's3',
    'http_object_store',
    TRUE,
    'tamoss',
    NULL,
    'https://s3.tamoss.localtest.me',
    jsonb_build_object(
        'id', 'f1ab5b54-9703-42ed-b181-11ba1c794a7f',
        'label', 'tamoss.us-east-1:s3:tamoss',
        'provider', 'tamoss',
        'region', 'us-east-1',
        'store_product', 's3',
        'store_type', 'http_object_store',
        'default_storage', TRUE,
        'bucket_name', 'tamoss',
        'endpoint_url', NULL,
        'public_endpoint_url', 'https://s3.tamoss.localtest.me'
    ),
    NOW()
)
ON CONFLICT (id) DO UPDATE SET
    label = EXCLUDED.label,
    provider = EXCLUDED.provider,
    region = EXCLUDED.region,
    store_product = EXCLUDED.store_product,
    store_type = EXCLUDED.store_type,
    default_storage = EXCLUDED.default_storage,
    bucket_name = EXCLUDED.bucket_name,
    endpoint_url = EXCLUDED.endpoint_url,
    public_endpoint_url = EXCLUDED.public_endpoint_url,
    record = EXCLUDED.record,
    updated_at = NOW();

WITH source_rows (
    id,
    format,
    description,
    label,
    created,
    tags
) AS (
    VALUES
    (
        '1c049232-88e5-4d68-9bd7-1ff0787c135e',
        'urn:x-nmos:format:video',
        'Bunny video essence source',
        'Bunny Video Source',
        '2025-11-19T17:16:58Z',
        '{"fixture": "bunny", "format": "video"}'::jsonb
    ),
    (
        'b66ee238-63e4-4507-a4d3-b4f1bc5a536a',
        'urn:x-nmos:format:audio',
        'Bunny audio essence source',
        'Bunny Audio Source',
        '2025-11-19T17:17:41Z',
        '{"fixture": "bunny", "format": "audio"}'::jsonb
    ),
    (
        'd9c3b2dc-967b-4d98-8ba8-b930606c2c6e',
        'urn:x-nmos:format:multi',
        'Bunny combined audio/video source',
        'Bunny Combined Source',
        '2025-11-19T17:19:21Z',
        '{"fixture": "bunny", "format": "multi"}'::jsonb
    ),
    (
        'f9360f46-7c04-4d7a-85aa-aa22911599f2',
        'urn:x-nmos:format:video',
        'Standalone fixture video source',
        'Fixture Video Source',
        '2025-11-19T17:20:00Z',
        '{"fixture": "standalone", "format": "video"}'::jsonb
    )
)
INSERT INTO tamoss_sources (
    id,
    format,
    label,
    tags,
    record,
    metadata_updated,
    updated_at
)
SELECT
    id::uuid,
    format,
    label,
    tags,
    jsonb_build_object(
        'id', id,
        'format', format,
        'label', label,
        'description', description,
        'tags', tags,
        'created', created,
        'metadata_updated', created
    ),
    created::timestamptz,
    NOW()
FROM source_rows
ON CONFLICT (id) DO UPDATE SET
    format = EXCLUDED.format,
    label = EXCLUDED.label,
    tags = EXCLUDED.tags,
    record = EXCLUDED.record,
    metadata_updated = EXCLUDED.metadata_updated,
    updated_at = NOW();

WITH flow_rows (
    id,
    source_id,
    format,
    description,
    label,
    created,
    segments_updated,
    read_only,
    avg_bit_rate,
    max_bit_rate,
    container,
    codec,
    tags,
    essence_parameters,
    flow_collection,
    segment_duration
) AS (
    VALUES
    (
        'b5f213bb-8722-43da-aee0-2ec49e3aeb35',
        '1c049232-88e5-4d68-9bd7-1ff0787c135e',
        'urn:x-nmos:format:video',
        'Bunny video essence flow',
        'Bunny Video',
        '2025-11-19T17:16:58Z',
        '2025-11-19T17:21:10Z',
        FALSE,
        5000::bigint,
        5000::bigint,
        'video/mp2t',
        'video/h264',
        '{"fixture": "bunny", "format": "video"}'::jsonb,
        '{"frame_width": 1920, "frame_height": 1080, "frame_rate": {"numerator": 50, "denominator": 1}}'::jsonb,
        NULL::jsonb,
        '{"numerator": 10}'::jsonb
    ),
    (
        'aa014016-5aa6-40bd-8498-cefa9bef25c4',
        'b66ee238-63e4-4507-a4d3-b4f1bc5a536a',
        'urn:x-nmos:format:audio',
        'Bunny audio essence flow',
        'Bunny Audio',
        '2025-11-19T17:17:41Z',
        '2025-11-19T17:20:25Z',
        FALSE,
        192::bigint,
        192::bigint,
        'video/mp2t',
        'audio/aac',
        '{"fixture": "bunny", "format": "audio"}'::jsonb,
        '{"channels": 2, "sample_rate": 48000}'::jsonb,
        NULL::jsonb,
        NULL::jsonb
    ),
    (
        'b719a70d-4cb2-4e41-a5e4-a67c7bccd137',
        'd9c3b2dc-967b-4d98-8ba8-b930606c2c6e',
        'urn:x-nmos:format:multi',
        'Bunny combined audio/video flow',
        'Bunny Combined',
        '2025-11-19T17:19:21Z',
        NULL,
        TRUE,
        NULL::bigint,
        NULL::bigint,
        NULL,
        NULL,
        '{"fixture": "bunny", "format": "multi"}'::jsonb,
        NULL::jsonb,
        jsonb_build_array(
            jsonb_build_object(
                'id', 'b5f213bb-8722-43da-aee0-2ec49e3aeb35',
                'role', 'video'
            ),
            jsonb_build_object(
                'id', 'aa014016-5aa6-40bd-8498-cefa9bef25c4',
                'role', 'audio'
            )
        ),
        NULL::jsonb
    )
),
flow_payloads AS (
    SELECT
        *,
        jsonb_strip_nulls(
            jsonb_build_object(
                'id', id,
                'source_id', source_id,
                'format', format,
                'description', description,
                'label', label,
                'created_by', 'server-to-server',
                'created', created,
                'metadata_updated', created,
                'segments_updated', segments_updated,
                'read_only', read_only,
                'avg_bit_rate', avg_bit_rate,
                'max_bit_rate', max_bit_rate,
                'container', container,
                'codec', codec,
                'tags', tags,
                'essence_parameters', essence_parameters,
                'flow_collection', flow_collection,
                'segment_duration', segment_duration
            )
        ) AS data
    FROM flow_rows
)
INSERT INTO tamoss_flows (
    id,
    source_id,
    format,
    container,
    read_only,
    tags,
    record,
    metadata_updated,
    segments_updated,
    updated_at
)
SELECT
    id::uuid,
    source_id::uuid,
    format,
    container,
    read_only,
    tags,
    jsonb_build_object(
        'id', id,
        'data', data,
        'source_id', source_id,
        'format', format,
        'container', container,
        'read_only', read_only,
        'tags', tags,
        'created', created,
        'metadata_updated', created,
        'segments_updated', segments_updated
    ),
    created::timestamptz,
    segments_updated::timestamptz,
    NOW()
FROM flow_payloads
ON CONFLICT (id) DO UPDATE SET
    source_id = EXCLUDED.source_id,
    format = EXCLUDED.format,
    container = EXCLUDED.container,
    read_only = EXCLUDED.read_only,
    tags = EXCLUDED.tags,
    record = EXCLUDED.record,
    metadata_updated = EXCLUDED.metadata_updated,
    segments_updated = EXCLUDED.segments_updated,
    updated_at = NOW();

WITH segment_rows (
    flow_id,
    object_id,
    timerange,
    timerange_start,
    timerange_end,
    created
) AS (
    VALUES
    (
        'b5f213bb-8722-43da-aee0-2ec49e3aeb35',
        '02145d18-8c07-490d-b598-aa6ec6329578',
        '[0:0_10:0)',
        0::bigint,
        10000000000::bigint,
        '2025-11-19T17:21:10Z'
    ),
    (
        'b5f213bb-8722-43da-aee0-2ec49e3aeb35',
        'd67ba606-3ecf-418c-ae0a-9caf8f20d8c0',
        '[10:0_20:0)',
        10000000000::bigint,
        20000000000::bigint,
        '2025-11-19T17:21:10Z'
    ),
    (
        'b5f213bb-8722-43da-aee0-2ec49e3aeb35',
        '0c6c4185-b455-4a09-9ba1-465d87914633',
        '[20:0_30:0)',
        20000000000::bigint,
        30000000000::bigint,
        '2025-11-19T17:21:10Z'
    ),
    (
        'aa014016-5aa6-40bd-8498-cefa9bef25c4',
        '9dd59ad2-e11d-48da-822a-77331470310d',
        '[0:0_10:0)',
        0::bigint,
        10000000000::bigint,
        '2025-11-19T17:20:25Z'
    ),
    (
        'aa014016-5aa6-40bd-8498-cefa9bef25c4',
        '6fd18817-a604-4481-827d-4b141fab43fa',
        '[10:0_20:0)',
        10000000000::bigint,
        20000000000::bigint,
        '2025-11-19T17:20:25Z'
    ),
    (
        'aa014016-5aa6-40bd-8498-cefa9bef25c4',
        'e50f8047-f3f3-494b-a6dc-3277f98f24bd',
        '[20:0_30:0)',
        20000000000::bigint,
        30000000000::bigint,
        '2025-11-19T17:20:25Z'
    )
),
object_records AS (
    SELECT
        flow_id,
        object_id,
        timerange,
        timerange_start,
        timerange_end,
        created,
        'f1ab5b54-9703-42ed-b181-11ba1c794a7f' AS storage_backend_id,
        'tamoss.us-east-1:s3:tamoss' AS storage_label,
        'https://s3.tamoss.localtest.me/tamoss/' || object_id AS object_url
    FROM segment_rows
)
INSERT INTO tamoss_media_objects (
    id,
    first_referenced_by_flow,
    referenced_by_flows,
    record,
    updated_at
)
SELECT
    object_id,
    flow_id::uuid,
    ARRAY[flow_id]::text[],
    jsonb_build_object(
        'id', object_id,
        'timerange', timerange,
        'first_referenced_by_flow', flow_id,
        'referenced_by_flows', jsonb_build_array(flow_id),
        'instances', jsonb_build_array(
            jsonb_build_object(
                'storage_backend_id', storage_backend_id,
                'url', object_url,
                'label', storage_label,
                'controlled', TRUE,
                'presigned', FALSE
            )
        ),
        'key_frame_count', 1,
        'bytes_written', 0
    ),
    NOW()
FROM object_records
ON CONFLICT (id) DO UPDATE SET
    first_referenced_by_flow = EXCLUDED.first_referenced_by_flow,
    referenced_by_flows = EXCLUDED.referenced_by_flows,
    record = EXCLUDED.record,
    updated_at = NOW();

WITH segment_rows (
    flow_id,
    object_id,
    timerange,
    timerange_start,
    timerange_end,
    created
) AS (
    VALUES
    (
        'b5f213bb-8722-43da-aee0-2ec49e3aeb35',
        '02145d18-8c07-490d-b598-aa6ec6329578',
        '[0:0_10:0)',
        0::bigint,
        10000000000::bigint,
        '2025-11-19T17:21:10Z'
    ),
    (
        'b5f213bb-8722-43da-aee0-2ec49e3aeb35',
        'd67ba606-3ecf-418c-ae0a-9caf8f20d8c0',
        '[10:0_20:0)',
        10000000000::bigint,
        20000000000::bigint,
        '2025-11-19T17:21:10Z'
    ),
    (
        'b5f213bb-8722-43da-aee0-2ec49e3aeb35',
        '0c6c4185-b455-4a09-9ba1-465d87914633',
        '[20:0_30:0)',
        20000000000::bigint,
        30000000000::bigint,
        '2025-11-19T17:21:10Z'
    ),
    (
        'aa014016-5aa6-40bd-8498-cefa9bef25c4',
        '9dd59ad2-e11d-48da-822a-77331470310d',
        '[0:0_10:0)',
        0::bigint,
        10000000000::bigint,
        '2025-11-19T17:20:25Z'
    ),
    (
        'aa014016-5aa6-40bd-8498-cefa9bef25c4',
        '6fd18817-a604-4481-827d-4b141fab43fa',
        '[10:0_20:0)',
        10000000000::bigint,
        20000000000::bigint,
        '2025-11-19T17:20:25Z'
    ),
    (
        'aa014016-5aa6-40bd-8498-cefa9bef25c4',
        'e50f8047-f3f3-494b-a6dc-3277f98f24bd',
        '[20:0_30:0)',
        20000000000::bigint,
        30000000000::bigint,
        '2025-11-19T17:20:25Z'
    )
),
segment_records AS (
    SELECT
        flow_id,
        object_id,
        timerange,
        timerange_start,
        timerange_end,
        created,
        'f1ab5b54-9703-42ed-b181-11ba1c794a7f' AS storage_backend_id,
        'tamoss.us-east-1:s3:tamoss' AS storage_label,
        'https://s3.tamoss.localtest.me/tamoss/' || object_id AS object_url
    FROM segment_rows
)
INSERT INTO tamoss_segments (
    flow_id,
    object_id,
    timerange,
    timerange_start,
    timerange_end,
    record,
    created,
    updated_at
)
SELECT
    flow_id::uuid,
    object_id,
    timerange,
    timerange_start,
    timerange_end,
    jsonb_build_object(
        'flow_id', flow_id,
        'object_id', object_id,
        'timerange', timerange,
        'ts_offset', NULL,
        'last_duration', NULL,
        'object_timerange', timerange,
        'sample_offset', NULL,
        'sample_count', NULL,
        'get_urls', jsonb_build_array(
            jsonb_build_object(
                'url', object_url,
                'label', storage_label,
                'storage_id', storage_backend_id
            )
        ),
        'key_frame_count', 1,
        'created', created
    ),
    created::timestamptz,
    NOW()
FROM segment_records
ON CONFLICT (flow_id, object_id, timerange) DO UPDATE SET
    timerange_start = EXCLUDED.timerange_start,
    timerange_end = EXCLUDED.timerange_end,
    record = EXCLUDED.record,
    created = EXCLUDED.created,
    updated_at = NOW();
