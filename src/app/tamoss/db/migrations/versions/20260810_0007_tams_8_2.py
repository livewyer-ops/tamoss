from __future__ import annotations

from alembic import op

revision = "20260810_0007"
down_revision = "20260610_0006"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.execute(
        """
        ALTER TABLE tamoss_storage_backends
        ADD COLUMN IF NOT EXISTS tags JSONB NOT NULL DEFAULT '{}'::jsonb;
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

        ALTER TABLE tamoss_sources ADD COLUMN IF NOT EXISTS created TIMESTAMPTZ;
        UPDATE tamoss_sources
        SET created = COALESCE(
            NULLIF(record->>'created', '')::timestamptz,
            created_at
        )
        WHERE created IS NULL;
        UPDATE tamoss_sources
        SET record = jsonb_set(
            record,
            '{created}',
            to_jsonb(created),
            true
        )
        WHERE NULLIF(record->>'created', '') IS NULL;
        ALTER TABLE tamoss_sources ALTER COLUMN created SET NOT NULL;
        CREATE INDEX IF NOT EXISTS idx_tamoss_sources_created
        ON tamoss_sources(created, id);
        CREATE INDEX IF NOT EXISTS idx_tamoss_sources_updated
        ON tamoss_sources(metadata_updated, id);
        CREATE INDEX IF NOT EXISTS idx_tamoss_sources_label
        ON tamoss_sources(label, id);

        ALTER TABLE tamoss_flows ADD COLUMN IF NOT EXISTS profile_id UUID;
        ALTER TABLE tamoss_flows ADD COLUMN IF NOT EXISTS status TEXT;
        ALTER TABLE tamoss_flows
            ADD COLUMN IF NOT EXISTS init_segments BOOLEAN NOT NULL DEFAULT FALSE;
        ALTER TABLE tamoss_flows ADD COLUMN IF NOT EXISTS label TEXT;
        ALTER TABLE tamoss_flows ADD COLUMN IF NOT EXISTS created TIMESTAMPTZ;
        ALTER TABLE tamoss_flows
            ADD COLUMN IF NOT EXISTS flow_collection_ids UUID[]
            NOT NULL DEFAULT ARRAY[]::UUID[];

        -- TAMS 8.1 allowed these 8.2 fields as arbitrary JSON extensions. It
        -- had no Profile resources, so every legacy profile_id is necessarily
        -- dangling and is removed. Project the other canonical values, then
        -- make client data, record envelope, and typed columns agree so
        -- hydration cannot bypass the guarded conversions.
        -- Text booleans are deliberately limited to exact lowercase
        -- "true"/"false" rather than PostgreSQL's permissive boolean aliases.
        WITH normalized AS (
            SELECT
                flow.id,
                flow.format,
                flow.record,
                flow.record->'data' AS source_data,
                NULL::uuid AS profile_id,
                CASE
                    WHEN flow.record->'data'->>'status' IN (
                        'awaiting_content',
                        'ingesting',
                        'replication_in_progress',
                        'closed_complete'
                    )
                    THEN flow.record->'data'->>'status'
                    ELSE NULL
                END AS status,
                CASE
                    WHEN flow.format NOT IN (
                        'urn:x-nmos:format:video',
                        'urn:x-nmos:format:audio',
                        'urn:x-nmos:format:data',
                        'urn:x-nmos:format:multi'
                    )
                    THEN FALSE
                    WHEN EXISTS (
                        SELECT 1
                        FROM tamoss_segments AS segment
                        WHERE segment.flow_id = flow.id
                    )
                    THEN FALSE
                    WHEN jsonb_typeof(
                        flow.record->'data'->'essence_parameters'
                            ->'init_segments'
                    ) = 'boolean'
                    THEN flow.record->'data'->'essence_parameters'
                             ->'init_segments' = 'true'::jsonb
                    WHEN jsonb_typeof(
                        flow.record->'data'->'essence_parameters'
                            ->'init_segments'
                    ) = 'string'
                     AND flow.record->'data'->'essence_parameters'
                             ->>'init_segments' IN ('true', 'false')
                    THEN flow.record->'data'->'essence_parameters'
                             ->>'init_segments' = 'true'
                    ELSE FALSE
                END AS init_segments,
                COALESCE(
                    NULLIF(flow.record->'data'->>'created', '')::timestamptz,
                    flow.created_at
                ) AS created
            FROM tamoss_flows AS flow
            WHERE flow.created IS NULL
        ),
        profile_data AS (
            SELECT
                normalized.*,
                normalized.source_data - 'profile_id' AS data_with_profile
            FROM normalized
        ),
        status_data AS (
            SELECT
                profile_data.*,
                CASE
                    WHEN profile_data.status IS NULL
                    THEN profile_data.data_with_profile - 'status'
                    ELSE profile_data.data_with_profile || jsonb_build_object(
                        'status', profile_data.status
                    )
                END AS data_with_status
            FROM profile_data
        ),
        canonical AS (
            SELECT
                status_data.id,
                status_data.format,
                status_data.record,
                status_data.profile_id,
                status_data.status,
                status_data.init_segments,
                status_data.created,
                (
                    CASE
                        WHEN status_data.format = 'urn:x-tam:format:image'
                         AND jsonb_typeof(
                            status_data.data_with_status->'essence_parameters'
                         ) = 'object'
                        THEN jsonb_set(
                            status_data.data_with_status,
                            '{essence_parameters}',
                            (
                                status_data.data_with_status
                                    ->'essence_parameters'
                            ) - 'init_segments',
                            false
                        )
                        WHEN jsonb_typeof(
                            status_data.data_with_status->'essence_parameters'
                        ) = 'object'
                         AND status_data.format IN (
                            'urn:x-nmos:format:video',
                            'urn:x-nmos:format:audio',
                            'urn:x-nmos:format:data',
                            'urn:x-nmos:format:multi'
                         )
                         AND status_data.data_with_status->'essence_parameters'
                                 ? 'init_segments'
                        THEN jsonb_set(
                            status_data.data_with_status,
                            '{essence_parameters,init_segments}',
                            to_jsonb(status_data.init_segments),
                            false
                        )
                        ELSE status_data.data_with_status
                    END
                ) || jsonb_build_object(
                    'created', status_data.created
                ) AS data
            FROM status_data
        )
        UPDATE tamoss_flows AS flow
        SET profile_id = canonical.profile_id,
            status = canonical.status,
            init_segments = canonical.init_segments,
            record = canonical.record || jsonb_build_object(
                'data', canonical.data,
                'profile_id', canonical.profile_id::text,
                'status', canonical.status,
                'init_segments', canonical.init_segments,
                'created', canonical.created
            ),
            label = canonical.data->>'label',
            created = canonical.created
        FROM canonical
        WHERE flow.id = canonical.id;
        UPDATE tamoss_flows AS flow
        SET flow_collection_ids = ARRAY(
            SELECT (item.value->>'id')::uuid
            FROM jsonb_array_elements(
                CASE
                    WHEN jsonb_typeof(flow.record->'data'->'flow_collection') = 'array'
                    THEN flow.record->'data'->'flow_collection'
                    ELSE '[]'::jsonb
                END
            ) WITH ORDINALITY AS item(value, ordinality)
            ORDER BY item.ordinality
        );
        ALTER TABLE tamoss_flows ALTER COLUMN created SET NOT NULL;
        DROP INDEX IF EXISTS idx_tamoss_flows_profile_id;
        DROP INDEX IF EXISTS idx_tamoss_flows_status;
        DROP INDEX IF EXISTS idx_tamoss_flows_init_segments;
        CREATE INDEX IF NOT EXISTS idx_tamoss_flows_profile_id_id
        ON tamoss_flows(profile_id, id);
        CREATE INDEX IF NOT EXISTS idx_tamoss_flows_status_id
        ON tamoss_flows(status, id);
        CREATE INDEX IF NOT EXISTS idx_tamoss_flows_init_segments_id
        ON tamoss_flows(init_segments, id);
        CREATE INDEX IF NOT EXISTS idx_tamoss_flows_created
        ON tamoss_flows(created, id);
        CREATE INDEX IF NOT EXISTS idx_tamoss_flows_metadata_updated
        ON tamoss_flows(metadata_updated, id);
        CREATE INDEX IF NOT EXISTS idx_tamoss_flows_label
        ON tamoss_flows(label, id);
        DROP INDEX IF EXISTS idx_tamoss_flows_flow_collection;
        CREATE INDEX idx_tamoss_flows_flow_collection
        ON tamoss_flows USING GIN(flow_collection_ids);

        CREATE INDEX IF NOT EXISTS idx_tamoss_webhooks_url
        ON tamoss_webhooks((record->'data'->>'url'), id);
        UPDATE tamoss_delete_requests
        SET created_at = COALESCE(
            NULLIF(record->>'created', '')::timestamptz,
            created_at
        );
        UPDATE tamoss_delete_requests
        SET record = jsonb_set(
            record,
            '{created}',
            to_jsonb(created_at),
            true
        )
        WHERE NULLIF(record->>'created', '') IS NULL;
        CREATE INDEX IF NOT EXISTS idx_tamoss_delete_requests_created
        ON tamoss_delete_requests(created_at DESC NULLS LAST, id DESC);
        CREATE INDEX IF NOT EXISTS idx_tamoss_delete_requests_expiry
        ON tamoss_delete_requests(
            (CASE WHEN status = 'done' THEN updated END) DESC NULLS LAST,
            id DESC
        );

        ALTER TABLE tamoss_media_objects
            ADD COLUMN IF NOT EXISTS object_kind TEXT NOT NULL DEFAULT 'unassigned';
        ALTER TABLE tamoss_media_objects
            ADD COLUMN IF NOT EXISTS content_type TEXT;
        ALTER TABLE tamoss_media_objects
            DROP CONSTRAINT IF EXISTS chk_tamoss_media_objects_kind;
        ALTER TABLE tamoss_media_objects
            ADD CONSTRAINT chk_tamoss_media_objects_kind
            CHECK (object_kind IN ('unassigned', 'media', 'init'));

        WITH media_types AS (
            SELECT
                segment.object_id,
                MIN(flow.container) FILTER (WHERE flow.container IS NOT NULL)
                    AS content_type,
                COUNT(DISTINCT flow.container) FILTER (
                    WHERE flow.container IS NOT NULL
                ) AS content_type_count
            FROM tamoss_segments AS segment
            JOIN tamoss_flows AS flow ON flow.id = segment.flow_id
            GROUP BY segment.object_id
        )
        UPDATE tamoss_media_objects AS media_object
        SET object_kind = 'media',
            content_type = CASE
                WHEN media_types.content_type_count = 1
                THEN media_types.content_type
                ELSE NULL
            END,
            record = jsonb_set(
                jsonb_set(
                    media_object.record,
                    '{object_kind}',
                    '"media"'::jsonb,
                    true
                ),
                '{content_type}',
                CASE
                    WHEN media_types.content_type_count = 1
                    THEN to_jsonb(media_types.content_type)
                    ELSE 'null'::jsonb
                END,
                true
            )
        FROM media_types
        WHERE media_types.object_id = media_object.id;

        UPDATE tamoss_media_objects AS media_object
        SET content_type = flow.container,
            record = jsonb_set(
                media_object.record,
                '{content_type}',
                to_jsonb(flow.container),
                true
            )
        FROM tamoss_flows AS flow
        WHERE media_object.object_kind = 'unassigned'
          AND media_object.record->>'allocated_by_flow' = flow.id::text
          AND flow.container IS NOT NULL;

        ALTER TABLE tamoss_segments ADD COLUMN IF NOT EXISTS init_object_id TEXT;
        CREATE INDEX IF NOT EXISTS idx_tamoss_segments_init_object_id
        ON tamoss_segments(init_object_id);
        """
    )


def downgrade() -> None:
    raise RuntimeError("TAMOSS schema downgrades are not supported")
