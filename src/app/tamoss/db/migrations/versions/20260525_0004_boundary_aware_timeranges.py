from __future__ import annotations

from alembic import op
from mediatimestamp import TimeRange
from sqlalchemy import text
from tamoss.domain.timeranges import finite_normalized_timerange_bounds

revision = "20260525_0004"
down_revision = "20260525_0003"
branch_labels = None
depends_on = None


def upgrade() -> None:
    connection = op.get_bind()
    rows = connection.execute(
        text(
            """
            SELECT flow_id, object_id, timerange
            FROM tamoss_segments
            """
        )
    ).mappings()
    for row in rows:
        try:
            timerange = TimeRange.from_str(row["timerange"])
            bounds = finite_normalized_timerange_bounds(timerange)
        except Exception as exc:
            raise RuntimeError(
                "Cannot migrate invalid Flow Segment timerange "
                f"{row['timerange']!r} for object {row['object_id']!r}"
            ) from exc
        connection.execute(
            text(
                """
                UPDATE tamoss_segments
                SET timerange_start = :timerange_start,
                    timerange_end = :timerange_end
                WHERE flow_id = :flow_id
                  AND object_id = :object_id
                  AND timerange = :timerange
                """
            ),
            {
                "flow_id": row["flow_id"],
                "object_id": row["object_id"],
                "timerange": row["timerange"],
                "timerange_start": bounds.start,
                "timerange_end": bounds.end,
            },
        )


def downgrade() -> None:
    raise RuntimeError("TAMOSS schema downgrades are not supported")
