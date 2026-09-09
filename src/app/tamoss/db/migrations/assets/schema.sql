-- Canonical TAMOSS schema SQL asset.
-- Runtime migrations, local compose bootstrap, and database tests read this
-- file from the Alembic migration assets package.

CREATE TABLE IF NOT EXISTS tamoss_service_metadata (
    id TEXT PRIMARY KEY,
    name TEXT,
    description TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS tamoss_storage_backends (
    id UUID PRIMARY KEY,
    label TEXT NOT NULL,
    provider TEXT NOT NULL,
    region TEXT NOT NULL,
    store_product TEXT NOT NULL,
    store_type TEXT NOT NULL DEFAULT 'http_object_store',
    default_storage BOOLEAN NOT NULL DEFAULT FALSE,
    bucket_name TEXT,
    endpoint_url TEXT,
    public_endpoint_url TEXT,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    record JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tamoss_storage_backends_one_default
ON tamoss_storage_backends(default_storage)
WHERE default_storage IS TRUE;
CREATE INDEX IF NOT EXISTS idx_tamoss_storage_backends_tags
ON tamoss_storage_backends USING GIN(tags);
CREATE INDEX IF NOT EXISTS idx_tamoss_storage_backends_label
ON tamoss_storage_backends(label, id);

CREATE TABLE IF NOT EXISTS tamoss_profiles (
    id UUID PRIMARY KEY,
    format TEXT NOT NULL,
    codec TEXT,
    label TEXT,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    record JSONB NOT NULL,
    created TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tamoss_profiles_format_id
ON tamoss_profiles(format, id);
CREATE INDEX IF NOT EXISTS idx_tamoss_profiles_codec_id
ON tamoss_profiles(codec, id);
CREATE INDEX IF NOT EXISTS idx_tamoss_profiles_label_id
ON tamoss_profiles(label, id);

CREATE TABLE IF NOT EXISTS tamoss_sources (
    id UUID PRIMARY KEY,
    format TEXT,
    label TEXT,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    record JSONB NOT NULL,
    metadata_updated TIMESTAMPTZ NOT NULL,
    created TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tamoss_sources_format ON tamoss_sources(format);
CREATE INDEX IF NOT EXISTS idx_tamoss_sources_tags ON tamoss_sources USING GIN(tags);
CREATE INDEX IF NOT EXISTS idx_tamoss_sources_created ON tamoss_sources(created, id);
CREATE INDEX IF NOT EXISTS idx_tamoss_sources_updated
ON tamoss_sources(metadata_updated, id);
CREATE INDEX IF NOT EXISTS idx_tamoss_sources_label ON tamoss_sources(label, id);

CREATE TABLE IF NOT EXISTS tamoss_flows (
    id UUID PRIMARY KEY,
    source_id UUID,
    format TEXT,
    container TEXT,
    profile_id UUID,
    status TEXT,
    init_segments BOOLEAN NOT NULL DEFAULT FALSE,
    label TEXT,
    flow_collection_ids UUID[] NOT NULL DEFAULT ARRAY[]::UUID[],
    read_only BOOLEAN NOT NULL DEFAULT FALSE,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    record JSONB NOT NULL,
    metadata_updated TIMESTAMPTZ NOT NULL,
    created TIMESTAMPTZ NOT NULL,
    segments_updated TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tamoss_flows_source_id ON tamoss_flows(source_id);
CREATE INDEX IF NOT EXISTS idx_tamoss_flows_format ON tamoss_flows(format);
CREATE INDEX IF NOT EXISTS idx_tamoss_flows_tags ON tamoss_flows USING GIN(tags);
CREATE INDEX IF NOT EXISTS idx_tamoss_flows_profile_id_id
ON tamoss_flows(profile_id, id);
CREATE INDEX IF NOT EXISTS idx_tamoss_flows_status_id
ON tamoss_flows(status, id);
CREATE INDEX IF NOT EXISTS idx_tamoss_flows_init_segments_id
ON tamoss_flows(init_segments, id);
CREATE INDEX IF NOT EXISTS idx_tamoss_flows_created ON tamoss_flows(created, id);
CREATE INDEX IF NOT EXISTS idx_tamoss_flows_metadata_updated
ON tamoss_flows(metadata_updated, id);
CREATE INDEX IF NOT EXISTS idx_tamoss_flows_label ON tamoss_flows(label, id);
CREATE INDEX IF NOT EXISTS idx_tamoss_flows_flow_collection
ON tamoss_flows USING GIN(flow_collection_ids);

CREATE TABLE IF NOT EXISTS tamoss_media_objects (
    id TEXT PRIMARY KEY,
    first_referenced_by_flow UUID,
    referenced_by_flows TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    object_kind TEXT NOT NULL DEFAULT 'unassigned',
    content_type TEXT,
    record JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tamoss_media_objects_referenced_by_flows
ON tamoss_media_objects USING GIN(referenced_by_flows);
CREATE INDEX IF NOT EXISTS idx_tamoss_media_objects_unreferenced
ON tamoss_media_objects(created_at, id)
WHERE cardinality(referenced_by_flows) = 0;
ALTER TABLE tamoss_media_objects
DROP CONSTRAINT IF EXISTS chk_tamoss_media_objects_kind;
ALTER TABLE tamoss_media_objects
ADD CONSTRAINT chk_tamoss_media_objects_kind
CHECK (object_kind IN ('unassigned', 'media', 'init'));

CREATE TABLE IF NOT EXISTS tamoss_segments (
    flow_id UUID NOT NULL,
    object_id TEXT NOT NULL,
    init_object_id TEXT,
    timerange TEXT NOT NULL,
    timerange_start BIGINT NOT NULL,
    timerange_end BIGINT NOT NULL,
    record JSONB NOT NULL,
    created TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_tamoss_segments_timerange_bounds
        CHECK (timerange_start <= timerange_end),
    PRIMARY KEY (flow_id, object_id, timerange),
    FOREIGN KEY (flow_id) REFERENCES tamoss_flows(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tamoss_segments_flow_timerange
ON tamoss_segments(flow_id, timerange_end, timerange_start);
CREATE INDEX IF NOT EXISTS idx_tamoss_segments_flow_object_timerange
ON tamoss_segments(flow_id, object_id, timerange_end, timerange_start);
CREATE INDEX IF NOT EXISTS idx_tamoss_segments_object_id
ON tamoss_segments(object_id);
CREATE INDEX IF NOT EXISTS idx_tamoss_segments_init_object_id
ON tamoss_segments(init_object_id);
CREATE INDEX IF NOT EXISTS idx_tamoss_segments_flow_timerange_start
ON tamoss_segments(flow_id, timerange_start);

CREATE TABLE IF NOT EXISTS tamoss_webhooks (
    id UUID PRIMARY KEY,
    status TEXT NOT NULL,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    record JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tamoss_webhooks_status ON tamoss_webhooks(status);
CREATE INDEX IF NOT EXISTS idx_tamoss_webhooks_tags ON tamoss_webhooks USING GIN(tags);
CREATE INDEX IF NOT EXISTS idx_tamoss_webhooks_url
ON tamoss_webhooks((record->'data'->>'url'), id);

CREATE TABLE IF NOT EXISTS tamoss_webhook_deliveries (
    id UUID PRIMARY KEY,
    webhook_id UUID NOT NULL,
    status TEXT NOT NULL,
    next_attempt_at TIMESTAMPTZ,
    claimed_at TIMESTAMPTZ,
    claimed_by TEXT,
    claim_expires_at TIMESTAMPTZ,
    record JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE tamoss_webhook_deliveries
ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;
ALTER TABLE tamoss_webhook_deliveries
ADD COLUMN IF NOT EXISTS claimed_by TEXT;
ALTER TABLE tamoss_webhook_deliveries
ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_tamoss_webhook_deliveries_webhook_id
ON tamoss_webhook_deliveries(webhook_id);
CREATE INDEX IF NOT EXISTS idx_tamoss_webhook_deliveries_status
ON tamoss_webhook_deliveries(status, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_tamoss_webhook_deliveries_claim
ON tamoss_webhook_deliveries(status, next_attempt_at, claim_expires_at);
CREATE INDEX IF NOT EXISTS idx_tamoss_webhook_deliveries_claimable
ON tamoss_webhook_deliveries(created_at, id)
WHERE status IN ('pending', 'started');

CREATE TABLE IF NOT EXISTS tamoss_delete_requests (
    id UUID PRIMARY KEY,
    flow_id UUID NOT NULL,
    status TEXT NOT NULL,
    claimed_at TIMESTAMPTZ,
    claimed_by TEXT,
    claim_expires_at TIMESTAMPTZ,
    record JSONB NOT NULL,
    updated TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE tamoss_delete_requests
ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;
ALTER TABLE tamoss_delete_requests
ADD COLUMN IF NOT EXISTS claimed_by TEXT;
ALTER TABLE tamoss_delete_requests
ADD COLUMN IF NOT EXISTS claim_expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_tamoss_delete_requests_flow_id
ON tamoss_delete_requests(flow_id);
CREATE INDEX IF NOT EXISTS idx_tamoss_delete_requests_status
ON tamoss_delete_requests(status);
CREATE INDEX IF NOT EXISTS idx_tamoss_delete_requests_claim
ON tamoss_delete_requests(status, claim_expires_at);
CREATE INDEX IF NOT EXISTS idx_tamoss_delete_requests_claimable
ON tamoss_delete_requests(created_at, id)
WHERE status IN ('created', 'started', 'error');
CREATE INDEX IF NOT EXISTS idx_tamoss_delete_requests_created
ON tamoss_delete_requests(created_at DESC NULLS LAST, id DESC);
CREATE INDEX IF NOT EXISTS idx_tamoss_delete_requests_expiry
ON tamoss_delete_requests(
    (CASE WHEN status = 'done' THEN updated END) DESC NULLS LAST,
    id DESC
);

CREATE TABLE IF NOT EXISTS tamoss_object_cleanups (
    id UUID PRIMARY KEY,
    delete_request_id UUID,
    object_id TEXT NOT NULL,
    storage_backend_id UUID NOT NULL,
    status TEXT NOT NULL,
    claimed_at TIMESTAMPTZ,
    claimed_by TEXT,
    claim_expires_at TIMESTAMPTZ,
    record JSONB NOT NULL,
    updated TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tamoss_object_cleanups_delete_request_id
ON tamoss_object_cleanups(delete_request_id);
CREATE INDEX IF NOT EXISTS idx_tamoss_object_cleanups_object_backend
ON tamoss_object_cleanups(object_id, storage_backend_id);
CREATE INDEX IF NOT EXISTS idx_tamoss_object_cleanups_claim
ON tamoss_object_cleanups(status, claim_expires_at);
CREATE INDEX IF NOT EXISTS idx_tamoss_object_cleanups_claimable
ON tamoss_object_cleanups(created_at, id)
WHERE status IN ('pending', 'started', 'error');

CREATE TABLE IF NOT EXISTS tamoss_object_copies (
    id UUID PRIMARY KEY,
    object_id TEXT NOT NULL,
    source_storage_backend_id UUID NOT NULL,
    destination_storage_backend_id UUID NOT NULL,
    status TEXT NOT NULL,
    claimed_at TIMESTAMPTZ,
    claimed_by TEXT,
    claim_expires_at TIMESTAMPTZ,
    record JSONB NOT NULL,
    updated TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tamoss_object_copies_object_id
ON tamoss_object_copies(object_id);
CREATE INDEX IF NOT EXISTS idx_tamoss_object_copies_destination
ON tamoss_object_copies(object_id, destination_storage_backend_id);
CREATE INDEX IF NOT EXISTS idx_tamoss_object_copies_claim
ON tamoss_object_copies(status, claim_expires_at);
CREATE INDEX IF NOT EXISTS idx_tamoss_object_copies_claimable
ON tamoss_object_copies(created_at, id)
WHERE status IN ('pending', 'started', 'error');
