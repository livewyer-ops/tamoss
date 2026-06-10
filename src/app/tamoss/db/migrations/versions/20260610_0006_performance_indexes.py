from __future__ import annotations

from alembic import op

revision = "20260610_0006"
down_revision = "20260527_0005"
branch_labels = None
depends_on = None


def upgrade() -> None:
    # Orphan sweep: list_unreferenced_objects_created_before filters on
    # cardinality(referenced_by_flows) = 0 and orders by (created_at, id);
    # without this partial index every worker poll scans the table.
    op.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_tamoss_media_objects_unreferenced
        ON tamoss_media_objects(created_at, id)
        WHERE cardinality(referenced_by_flows) = 0
        """
    )
    # Worker claim queries order by (created_at, id) over claimable statuses;
    # partial indexes keep the claim scan bounded by live work rather than
    # historical volume.
    op.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_tamoss_webhook_deliveries_claimable
        ON tamoss_webhook_deliveries(created_at, id)
        WHERE status IN ('pending', 'started')
        """
    )
    op.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_tamoss_delete_requests_claimable
        ON tamoss_delete_requests(created_at, id)
        WHERE status IN ('created', 'started', 'error')
        """
    )
    op.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_tamoss_object_cleanups_claimable
        ON tamoss_object_cleanups(created_at, id)
        WHERE status IN ('pending', 'started', 'error')
        """
    )
    op.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_tamoss_object_copies_claimable
        ON tamoss_object_copies(created_at, id)
        WHERE status IN ('pending', 'started', 'error')
        """
    )
    # MIN(timerange_start) aggregates (segment listing page headers and
    # deletion timerange tracking) cannot use the existing
    # (flow_id, timerange_end, timerange_start) index as a single probe.
    op.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_tamoss_segments_flow_timerange_start
        ON tamoss_segments(flow_id, timerange_start)
        """
    )
    # Containment lookups for collected_by relationships
    # (list_flows_collecting) over the flow_collection array.
    op.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_tamoss_flows_flow_collection
        ON tamoss_flows USING GIN((record->'data'->'flow_collection') jsonb_path_ops)
        """
    )


def downgrade() -> None:
    raise RuntimeError("TAMOSS schema downgrades are not supported")
