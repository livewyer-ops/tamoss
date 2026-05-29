from __future__ import annotations

from importlib import resources

from alembic import op

revision = "20260524_0001"
down_revision = None
branch_labels = None
depends_on = None


def upgrade() -> None:
    _execute_asset("schema.sql")
    _execute_asset("bootstrap.sql")


def downgrade() -> None:
    raise RuntimeError("TAMOSS schema downgrades are not supported")


def _execute_asset(name: str) -> None:
    asset = resources.files("tamoss.db.migrations").joinpath("assets", name)
    op.execute(asset.read_text(encoding="utf-8"))
