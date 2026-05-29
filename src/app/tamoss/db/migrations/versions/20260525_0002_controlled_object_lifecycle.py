from __future__ import annotations

revision = "20260525_0002"
down_revision = "20260524_0001"
branch_labels = None
depends_on = None


def upgrade() -> None:
    # Object upload is validated at first segment registration.
    pass


def downgrade() -> None:
    raise RuntimeError("TAMOSS schema downgrades are not supported")
