from __future__ import annotations

from alembic import op

revision = "20260924_0008"
down_revision = "20260810_0007"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.execute(
        """
        ALTER TABLE tamoss_segments
            ALTER COLUMN timerange_start TYPE NUMERIC
                USING timerange_start::numeric,
            ALTER COLUMN timerange_end TYPE NUMERIC
                USING timerange_end::numeric;
        """
    )
    # Invalid row estimates from parallel GIN builds can prevent autoanalyze.
    op.execute("SET LOCAL lock_timeout = '10s'")
    op.execute("SET LOCAL statement_timeout = '5min'")
    op.execute(
        "ANALYZE tamoss_segments, tamoss_storage_backends, tamoss_sources, "
        "tamoss_flows, tamoss_media_objects, tamoss_webhooks"
    )


def downgrade() -> None:
    raise RuntimeError("TAMOSS schema downgrades are not supported")
