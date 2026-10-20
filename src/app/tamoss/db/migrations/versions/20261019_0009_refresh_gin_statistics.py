from __future__ import annotations

from alembic import op

revision = "20261019_0009"
down_revision = "20260924_0008"
branch_labels = None
depends_on = None


def upgrade() -> None:
    # Invalid row estimates from parallel GIN builds can prevent autoanalyze.
    op.execute("SET LOCAL lock_timeout = '10s'")
    op.execute("SET LOCAL statement_timeout = '5min'")
    op.execute(
        "ANALYZE tamoss_storage_backends, tamoss_sources, tamoss_flows, "
        "tamoss_media_objects, tamoss_webhooks"
    )


def downgrade() -> None:
    raise RuntimeError("TAMOSS schema downgrades are not supported")
