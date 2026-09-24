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
    op.execute("ANALYZE tamoss_segments")


def downgrade() -> None:
    raise RuntimeError("TAMOSS schema downgrades are not supported")
