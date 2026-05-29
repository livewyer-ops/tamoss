from __future__ import annotations

from collections.abc import Iterable, Mapping

from tamoss.contract.payloads import JsonPayload, without_none
from tamoss.domain.model import (
    MediaObjectRecord,
    ObjectGetUrlBatchKey,
    ObjectGetUrlRequest,
    ObjectInstance,
    StorageBackend,
)
from tamoss.ports.object_storage import ObjectStorage


def objects_get_urls(
    media_objects: Iterable[MediaObjectRecord],
    *,
    object_storage: ObjectStorage,
    accept_get_urls: set[str] | None = None,
    accept_storage_ids: set[str] | None = None,
    presigned: bool | None = None,
    verbose_storage: bool = False,
) -> dict[str, list[JsonPayload]]:
    media_object_list = list(media_objects)
    if accept_get_urls is not None and not accept_get_urls:
        return {media_object.id: [] for media_object in media_object_list}
    controlled_get_urls = _controlled_get_urls_by_object(
        media_object_list,
        object_storage=object_storage,
        accept_get_urls=accept_get_urls,
        accept_storage_ids=accept_storage_ids,
        presigned=presigned,
    )
    return {
        media_object.id: _object_get_urls_from_payloads(
            media_object,
            controlled_get_urls=controlled_get_urls,
            accept_get_urls=accept_get_urls,
            accept_storage_ids=accept_storage_ids,
            presigned=presigned,
            verbose_storage=verbose_storage,
        )
        for media_object in media_object_list
    }


def _object_get_urls_from_payloads(
    media_object: MediaObjectRecord,
    *,
    controlled_get_urls: dict[ObjectGetUrlBatchKey, list[JsonPayload]],
    accept_get_urls: set[str] | None,
    accept_storage_ids: set[str] | None,
    presigned: bool | None,
    verbose_storage: bool,
) -> list[JsonPayload]:
    get_urls: list[JsonPayload] = []
    seen: set[tuple[str | None, str | None, str | None, bool, bool]] = set()
    for instance in media_object.instances:
        for payload in _instance_get_urls(
            object_id=media_object.id,
            instance=instance,
            controlled_get_urls=controlled_get_urls,
        ):
            if not _payload_matches(
                payload,
                accept_get_urls=accept_get_urls,
                accept_storage_ids=accept_storage_ids,
                presigned=presigned,
            ):
                continue
            dedupe_key = _payload_dedupe_key(payload)
            if dedupe_key in seen:
                continue
            seen.add(dedupe_key)
            get_urls.append(_payload_response(payload, verbose_storage=verbose_storage))
    return get_urls


def _instance_get_urls(
    *,
    object_id: str,
    instance: ObjectInstance,
    controlled_get_urls: dict[ObjectGetUrlBatchKey, list[JsonPayload]],
) -> list[JsonPayload]:
    if instance.controlled and instance.storage_backend is not None:
        return controlled_get_urls.get((instance.storage_backend.id, object_id), [])
    if instance.url is None:
        return []
    return [_uncontrolled_get_url_payload(instance)]


def _controlled_get_urls_by_object(
    media_objects: Iterable[MediaObjectRecord],
    *,
    object_storage: ObjectStorage,
    accept_get_urls: set[str] | None,
    accept_storage_ids: set[str] | None,
    presigned: bool | None,
) -> dict[ObjectGetUrlBatchKey, list[JsonPayload]]:
    requests: dict[ObjectGetUrlBatchKey, ObjectGetUrlRequest] = {}
    backends_by_key: dict[ObjectGetUrlBatchKey, StorageBackend] = {}
    for media_object in media_objects:
        for instance in media_object.instances:
            if not instance.controlled or instance.storage_backend is None:
                continue
            storage_id = str(instance.storage_backend.id)
            if accept_storage_ids is not None and storage_id not in accept_storage_ids:
                continue
            label = instance.storage_backend.label
            label_accepted = accept_get_urls is None or label in accept_get_urls
            include_direct = presigned is not True and label_accepted
            include_presigned = presigned is not False and label_accepted
            if not include_direct and not include_presigned:
                continue
            key = (instance.storage_backend.id, media_object.id)
            existing = requests.get(key)
            if existing is None:
                requests[key] = ObjectGetUrlRequest(
                    object_id=media_object.id,
                    backend=instance.storage_backend,
                    include_direct=include_direct,
                    include_presigned=include_presigned,
                )
            else:
                existing.include_direct = existing.include_direct or include_direct
                existing.include_presigned = (
                    existing.include_presigned or include_presigned
                )
            backends_by_key[key] = instance.storage_backend
    if not requests:
        return {}
    raw_get_urls: Mapping[
        ObjectGetUrlBatchKey,
        Iterable[Mapping[str, object]],
    ] = object_storage.build_get_urls_batch(requests.values())
    return {
        key: [
            _controlled_get_url_payload(
                get_url,
                storage_backend=backends_by_key[key],
            )
            for get_url in get_urls
        ]
        for key, get_urls in raw_get_urls.items()
    }


def _controlled_get_url_payload(
    get_url: Mapping[str, object],
    *,
    storage_backend: StorageBackend,
) -> JsonPayload:
    url = get_url.get("url")
    label = get_url.get("label") or storage_backend.label
    return {
        "url": str(url) if url is not None else None,
        "label": str(label) if label is not None else None,
        "storage_id": str(storage_backend.id),
        "presigned": bool(get_url.get("presigned", False)),
        "controlled": True,
        "store_type": storage_backend.store_type,
        "provider": storage_backend.provider,
        "region": storage_backend.region,
        "store_product": storage_backend.store_product,
    }


def _uncontrolled_get_url_payload(instance: ObjectInstance) -> JsonPayload:
    return {
        "url": instance.url,
        "label": instance.label,
        "presigned": instance.presigned,
        "controlled": False,
    }


def _payload_matches(
    payload: Mapping[str, object],
    *,
    accept_get_urls: set[str] | None,
    accept_storage_ids: set[str] | None,
    presigned: bool | None,
) -> bool:
    if presigned is not None and bool(payload.get("presigned", False)) != presigned:
        return False
    label = payload.get("label")
    if accept_get_urls is not None and (
        not isinstance(label, str) or label not in accept_get_urls
    ):
        return False
    storage_id = payload.get("storage_id")
    return not (
        accept_storage_ids is not None
        and (not isinstance(storage_id, str) or storage_id not in accept_storage_ids)
    )


def _payload_dedupe_key(
    payload: Mapping[str, object],
) -> tuple[str | None, str | None, str | None, bool, bool]:
    return (
        _optional_string(payload.get("label")),
        _optional_string(payload.get("storage_id")),
        _optional_string(payload.get("url")),
        bool(payload.get("presigned", False)),
        bool(payload.get("controlled", False)),
    )


def _payload_response(
    payload: Mapping[str, object],
    *,
    verbose_storage: bool,
) -> JsonPayload:
    response = {
        "url": payload.get("url"),
        "label": payload.get("label"),
        "presigned": bool(payload.get("presigned", False)),
    }
    if verbose_storage:
        response.update(
            {
                "storage_id": payload.get("storage_id"),
                "controlled": bool(payload.get("controlled", False)),
                "store_type": payload.get("store_type"),
                "provider": payload.get("provider"),
                "region": payload.get("region"),
                "store_product": payload.get("store_product"),
            }
        )
    return without_none(response)


def _optional_string(value: object) -> str | None:
    return str(value) if value is not None else None
