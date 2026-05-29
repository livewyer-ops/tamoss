from __future__ import annotations

from alembic import op

revision = "20260525_0003"
down_revision = "20260525_0002"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.execute(
        """
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
        )
        """
    )
    op.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_tamoss_object_cleanups_delete_request_id
        ON tamoss_object_cleanups(delete_request_id)
        """
    )
    op.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_tamoss_object_cleanups_object_backend
        ON tamoss_object_cleanups(object_id, storage_backend_id)
        """
    )
    op.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_tamoss_object_cleanups_claim
        ON tamoss_object_cleanups(status, claim_expires_at)
        """
    )


def downgrade() -> None:
    raise RuntimeError("TAMOSS schema downgrades are not supported")
