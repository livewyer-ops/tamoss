from __future__ import annotations

from typing import Any

from pydantic import BaseModel


def contract_dump(
    model: BaseModel | list[BaseModel],
    *,
    exclude_unset: bool = False,
) -> Any:
    if isinstance(model, list):
        return [contract_dump(item, exclude_unset=exclude_unset) for item in model]
    return model.model_dump(
        mode="json",
        by_alias=True,
        exclude_none=True,
        exclude_unset=exclude_unset,
    )
