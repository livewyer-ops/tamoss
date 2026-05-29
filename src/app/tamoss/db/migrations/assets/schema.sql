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
    record JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_tamoss_storage_backends_one_default
ON tamoss_storage_backends(default_storage)
WHERE default_storage IS TRUE;

CREATE TABLE IF NOT EXISTS tamoss_sources (
    id UUID PRIMARY KEY,
    format TEXT,
    label TEXT,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    record JSONB NOT NULL,
    metadata_updated TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tamoss_sources_format ON tamoss_sources(format);
CREATE INDEX IF NOT EXISTS idx_tamoss_sources_tags ON tamoss_sources USING GIN(tags);

CREATE TABLE IF NOT EXISTS tamoss_flows (
    id UUID PRIMARY KEY,
    source_id UUID,
    format TEXT,
    container TEXT,
    read_only BOOLEAN NOT NULL DEFAULT FALSE,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    record JSONB NOT NULL,
    metadata_updated TIMESTAMPTZ NOT NULL,
    segments_updated TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tamoss_flows_source_id ON tamoss_flows(source_id);
CREATE INDEX IF NOT EXISTS idx_tamoss_flows_format ON tamoss_flows(format);
CREATE INDEX IF NOT EXISTS idx_tamoss_flows_tags ON tamoss_flows USING GIN(tags);

CREATE TABLE IF NOT EXISTS tamoss_media_objects (
    id TEXT PRIMARY KEY,
    first_referenced_by_flow UUID,
    referenced_by_flows TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    record JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tamoss_media_objects_referenced_by_flows
ON tamoss_media_objects USING GIN(referenced_by_flows);

CREATE TABLE IF NOT EXISTS tamoss_segments (
    flow_id UUID NOT NULL,
    object_id TEXT NOT NULL,
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
