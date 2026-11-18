from __future__ import annotations

from collections.abc import Mapping
from typing import Any

from pydantic import BaseModel
from pydantic.fields import FieldInfo


def contract_dump(
    model: BaseModel | list[BaseModel],
    *,
    exclude_unset: bool = False,
) -> Any:
    if isinstance(model, list):
        return [contract_dump(item, exclude_unset=exclude_unset) for item in model]
    payload = model.model_dump(
        mode="json",
        by_alias=True,
        exclude_none=True,
        exclude_unset=exclude_unset,
    )
    return _restore_null_extensions(model, payload)


def _restore_null_extensions(value: object, payload: Any) -> Any:
    if isinstance(value, BaseModel):
        if getattr(type(value), "__pydantic_root_model__", False):
            return _restore_null_extensions(value.__dict__["root"], payload)
        if not isinstance(payload, dict):
            return payload

        for name, field in type(value).model_fields.items():
            output_name = _output_field_name(name, field)
            if output_name in payload:
                payload[output_name] = _restore_null_extensions(
                    value.__dict__[name],
                    payload[output_name],
                )
        for name, extension_value in (value.model_extra or {}).items():
            if extension_value is None:
                payload[name] = None
            elif name in payload:
                payload[name] = _restore_null_extensions(
                    extension_value,
                    payload[name],
                )
        return payload

    if isinstance(value, list) and isinstance(payload, list):
        return [
            _restore_null_extensions(item, serialized)
            for item, serialized in zip(value, payload, strict=True)
        ]
    if isinstance(value, Mapping) and isinstance(payload, dict):
        for name, item in value.items():
            if name in payload:
                payload[name] = _restore_null_extensions(item, payload[name])
        return payload
    return payload


def _output_field_name(name: str, field: FieldInfo) -> str:
    alias = field.serialization_alias or field.alias
    return alias if isinstance(alias, str) else name
