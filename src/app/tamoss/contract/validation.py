from __future__ import annotations

import json
from collections.abc import Iterable, Mapping

from pydantic import BaseModel


def strict_contract_model[ModelT: BaseModel](
    model_type: type[ModelT],
    payload: object,
    *,
    non_nullable_fields: Iterable[str] = (),
) -> ModelT:
    if isinstance(payload, Mapping):
        for field_name in non_nullable_fields:
            if field_name in payload and payload[field_name] is None:
                raise ValueError(f"{field_name} must not be null")

    # JSON strict mode accepts contract encodings such as UUID strings while
    # rejecting JSON scalar coercion (for example, 0 as false).
    return model_type.model_validate_json(json.dumps(payload), strict=True)
