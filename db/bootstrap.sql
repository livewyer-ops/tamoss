-- TAMOSS operational bootstrap rows.
--
-- BBC API storage-backend resources advertise logical storage metadata; object
-- reachability is resolved by the runtime backend configuration.

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
    NULL,
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
        'public_endpoint_url', NULL
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
