package controller

func storageBackendRegistrationScript() string {
	return postgresPSQLScript([]string{
		`-v backend_id="${TAMOSS_STORAGE_BACKEND_ID}"`,
		`-v label="${TAMOSS_STORAGE_BACKEND_LABEL}"`,
		`-v provider="${TAMOSS_STORAGE_PROVIDER}"`,
		`-v region="${TAMOSS_STORAGE_REGION}"`,
		`-v store_product="${TAMOSS_STORAGE_PRODUCT}"`,
		`-v store_type="${TAMOSS_STORAGE_TYPE}"`,
		`-v default_storage="${TAMOSS_STORAGE_DEFAULT}"`,
		`-v bucket_name="${TAMOSS_STORAGE_BUCKET}"`,
		`-v endpoint_url="${TAMOSS_STORAGE_ENDPOINT}"`,
		`-v public_endpoint_url="${TAMOSS_STORAGE_PUBLIC_ENDPOINT}"`,
	}, postgresTransactionalSQL(storageBackendRegistrationSQL()))
}

func storageBackendRegistrationSQL() string {
	return `UPDATE tamoss_storage_backends
SET default_storage = FALSE,
    record = jsonb_set(record, '{default_storage}', 'false'::jsonb, true),
    updated_at = NOW()
WHERE :'default_storage'::boolean IS TRUE
  AND id <> :'backend_id'::uuid;
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
)
VALUES (
  :'backend_id'::uuid,
  :'label',
  :'provider',
  :'region',
  :'store_product',
  :'store_type',
  :'default_storage'::boolean,
  :'bucket_name',
  NULLIF(:'endpoint_url', ''),
  NULLIF(:'public_endpoint_url', ''),
  jsonb_build_object(
    'id', :'backend_id',
    'label', :'label',
    'provider', :'provider',
    'region', :'region',
    'store_product', :'store_product',
    'store_type', :'store_type',
    'default_storage', :'default_storage'::boolean,
    'bucket_name', :'bucket_name',
    'endpoint_url', NULLIF(:'endpoint_url', ''),
    'public_endpoint_url', NULLIF(:'public_endpoint_url', '')
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
  updated_at = NOW();`
}

func storageBackendDeregistrationScript() string {
	return postgresPSQLScript([]string{
		`-v backend_id="${TAMOSS_STORAGE_BACKEND_ID}"`,
	}, storageBackendDeregistrationSQL())
}

func storageBackendDeregistrationSQL() string {
	return `SELECT CASE
  WHEN to_regclass('public.tamoss_storage_backends') IS NULL THEN 'false'
  ELSE 'true'
END AS storage_backends_exists \gset
\if :storage_backends_exists
DELETE FROM tamoss_storage_backends
WHERE id = :'backend_id'::uuid;
\endif`
}
