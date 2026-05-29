from __future__ import annotations

SEGMENT_OVERLAP_MESSAGE = "Segment timerange overlaps with an existing segment"


class SegmentOverlapError(ValueError):
    """Raised when a segment write conflicts with an existing timerange."""
