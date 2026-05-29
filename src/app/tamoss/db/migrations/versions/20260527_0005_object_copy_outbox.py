from __future__ import annotations

from alembic import op

revision = "20260527_0005"
down_revision = "20260525_0004"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.execute(
        """
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
        )
        """
    )
    op.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_tamoss_object_copies_object_id
        ON tamoss_object_copies(object_id)
        """
    )
    op.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_tamoss_object_copies_destination
        ON tamoss_object_copies(object_id, destination_storage_backend_id)
        """
    )
    op.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_tamoss_object_copies_claim
        ON tamoss_object_copies(status, claim_expires_at)
        """
    )


def downgrade() -> None:
    raise RuntimeError("TAMOSS schema downgrades are not supported")
