from __future__ import annotations

import json
from collections.abc import Iterable, Mapping
from typing import TypeGuard, get_args, get_origin

from pydantic import BaseModel


def strict_contract_model[ModelT: BaseModel](
    model_type: type[ModelT],
    payload: object,
    *,
    non_nullable_fields: Iterable[str] = (),
    recursive_non_nullable_fields: Iterable[str] = (),
) -> ModelT:
    if isinstance(payload, Mapping):
        reject_explicit_nulls(payload, non_nullable_fields)
    selected_recursive_fields = tuple(recursive_non_nullable_fields)
    if selected_recursive_fields:
        reject_model_explicit_nulls(
            model_type,
            payload,
            field_names=selected_recursive_fields,
        )

    # JSON strict mode accepts contract encodings such as UUID strings while
    # rejecting JSON scalar coercion (for example, 0 as false).
    return model_type.model_validate_json(json.dumps(payload), strict=True)


def reject_explicit_nulls(
    payload: Mapping[str, object],
    field_names: Iterable[str],
) -> None:
    for field_name in field_names:
        if field_name not in payload:
            continue
        if payload[field_name] is None:
            raise ValueError(f"{field_name} must not be null")


def reject_model_explicit_nulls(
    model_type: type[BaseModel],
    payload: object,
    *,
    field_names: Iterable[str] | None = None,
) -> None:
    selected_fields = frozenset(field_names) if field_names is not None else None
    _reject_annotation_nulls(model_type, payload, field_names=selected_fields)


def _reject_annotation_nulls(
    annotation: object,
    value: object,
    *,
    field_names: frozenset[str] | None = None,
) -> None:
    if value is None:
        raise ValueError("contract field must not be null")

    if _is_model_type(annotation):
        if getattr(annotation, "__pydantic_root_model__", False):
            root_field = annotation.model_fields["root"]
            _reject_annotation_nulls(
                root_field.annotation,
                value,
                field_names=field_names,
            )
            return
        if not isinstance(value, Mapping):
            return
        for name, item in value.items():
            if field_names is not None and name not in field_names:
                continue
            model_field = annotation.model_fields.get(name)
            if model_field is None:
                continue
            _reject_annotation_nulls(model_field.annotation, item)
        return

    origin = get_origin(annotation)
    arguments = get_args(annotation)
    if origin is list:
        if isinstance(value, list) and arguments:
            for item in value:
                _reject_annotation_nulls(arguments[0], item)
        return
    if origin is dict:
        if isinstance(value, Mapping) and len(arguments) == 2:
            for item in value.values():
                _reject_annotation_nulls(arguments[1], item)
        return
    if arguments:
        for nested_annotation in arguments:
            if nested_annotation is type(None):
                continue
            _reject_annotation_nulls(
                nested_annotation,
                value,
                field_names=field_names,
            )


def _is_model_type(annotation: object) -> TypeGuard[type[BaseModel]]:
    return isinstance(annotation, type) and issubclass(annotation, BaseModel)
