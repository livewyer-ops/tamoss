from __future__ import annotations

from tamoss.application.contexts._shared import (
    UUID,
    Any,
    BadRequest,
    MediaObjectRecord,
    MediaObjectRegistration,
    NotFound,
    ObjectInstance,
    UseCaseContext,
    _append_uncontrolled_instance,
    _controlled_get_url_payload,
    _include_get_url_payload,
    _is_http_url,
    _object_instance_matches,
    _response_get_url_payload,
    _segment_object_timerange,
    _uncontrolled_get_url_payload,
    _union_timerange_strings,
    cast,
)


class ObjectUseCases(UseCaseContext):
    def register_object_instance(
        self, *, object_id: str, registration: MediaObjectRegistration
    ) -> None:
        media_object = self.get_object(object_id)
        has_controlled = registration.storage_id is not None
        has_uncontrolled = (
            registration.url is not None or registration.label is not None
        )
        if has_controlled == has_uncontrolled:
            raise BadRequest("Bad request. Invalid request JSON.")
        if has_controlled:
            raise BadRequest(
                "Bad request. Controlled Object instance registration is not supported."
            )
        self._register_uncontrolled_object_instance(
            media_object, url=registration.url, label=registration.label
        )
        self.repository.save_object(media_object)

    def delete_object_instance(
        self, *, object_id: str, storage_id: UUID | None, label: str | None
    ) -> None:
        if (storage_id is None and label is None) or (
            storage_id is not None and label is not None
        ):
            raise BadRequest("Bad request. Invalid query options.")
        media_object = self.get_object(object_id)
        matches = [
            instance
            for instance in media_object.instances
            if _object_instance_matches(instance, storage_id=storage_id, label=label)
        ]
        controlled_match_backend_ids = {
            instance.storage_backend.id
            for instance in matches
            if instance.controlled and instance.storage_backend is not None
        }
        if controlled_match_backend_ids:
            matches = [
                instance
                for instance in media_object.instances
                if instance in matches
                or (
                    instance.controlled
                    and instance.storage_backend is not None
                    and instance.storage_backend.id in controlled_match_backend_ids
                )
            ]
        if not matches:
            raise NotFound("The requested Object instance does not exist.")
        if len(matches) == len(media_object.instances):
            raise BadRequest(
                "Bad request. All instances would be deleted. "
                "Use Flow Segment deletion instead."
            )

        media_object.instances = [
            instance for instance in media_object.instances if instance not in matches
        ]
        deleted_backend_ids: set[UUID] = set()
        for instance in matches:
            if (
                instance.controlled
                and instance.storage_backend is not None
                and instance.storage_backend.id not in deleted_backend_ids
            ):
                self.object_storage.delete(object_id, backend=instance.storage_backend)
                deleted_backend_ids.add(instance.storage_backend.id)
        self.repository.save_object(media_object)

    def _register_uncontrolled_object_instance(
        self, media_object: MediaObjectRecord, *, url: str | None, label: str | None
    ) -> None:
        if not label or not url or not _is_http_url(url):
            raise BadRequest("Bad request. Invalid request JSON.")
        reserved_labels = {
            backend.label
            for backend in self.repository.list_storage_backends()
            if backend.label is not None
        }
        if label in reserved_labels:
            raise BadRequest("Bad request. Invalid request JSON.")
        try:
            _append_uncontrolled_instance(
                media_object,
                url=url,
                label=label,
                presigned=False,
            )
        except ValueError as exc:
            raise BadRequest("Bad request. Invalid request JSON.") from exc

    def get_object(self, object_id: str) -> MediaObjectRecord:
        media_object = self.repository.get_object(object_id)
        if media_object is None or not media_object.referenced_by_flows:
            raise NotFound("The requested Media Object does not exist.")
        return media_object

    def object_get_urls(
        self,
        media_object: MediaObjectRecord,
        *,
        accept_get_urls: set[str] | None = None,
        accept_storage_ids: set[str] | None = None,
        presigned: bool | None = None,
        verbose_storage: bool = False,
    ) -> list[dict[str, Any]]:
        get_urls: list[dict[str, Any]] = []
        seen: set[
            tuple[
                str | None,
                str | None,
                str | None,
                bool | None,
                bool | None,
            ]
        ] = set()
        for instance in media_object.instances:
            for payload in self._instance_get_urls(
                object_id=media_object.id,
                instance=instance,
                verbose_storage=verbose_storage,
            ):
                if not _include_get_url_payload(
                    payload,
                    accept_get_urls=accept_get_urls,
                    accept_storage_ids=accept_storage_ids,
                    presigned=presigned,
                ):
                    continue
                key = (
                    cast(str | None, payload.get("label")),
                    cast(str | None, payload.get("storage_id")),
                    cast(str | None, payload.get("url")),
                    cast(bool | None, payload.get("presigned")),
                    cast(bool | None, payload.get("controlled")),
                )
                if key in seen:
                    continue
                seen.add(key)
                get_urls.append(
                    _response_get_url_payload(
                        payload,
                        verbose_storage=verbose_storage,
                    )
                )
        return get_urls

    def _instance_get_urls(
        self,
        *,
        object_id: str,
        instance: ObjectInstance,
        verbose_storage: bool,
    ) -> list[dict[str, Any]]:
        if instance.controlled and instance.storage_backend is not None:
            return [
                _controlled_get_url_payload(
                    get_url,
                    storage_backend=instance.storage_backend,
                    verbose_storage=verbose_storage,
                )
                for get_url in self.object_storage.build_get_urls(
                    object_id=object_id,
                    backend=instance.storage_backend,
                )
            ]
        if instance.url is None:
            return []
        return [_uncontrolled_get_url_payload(instance)]

    def _object_timerange(self, media_object: MediaObjectRecord) -> str | None:
        return _union_timerange_strings(
            _segment_object_timerange(segment)
            for flow_id in media_object.referenced_by_flows
            for segment in self.repository.list_segments(flow_id)
            if segment.object_id == media_object.id
        )

    def _delete_controlled_object_content(
        self, media_object: MediaObjectRecord
    ) -> None:
        deleted_backend_ids: set[UUID] = set()
        for instance in media_object.instances:
            if (
                instance.controlled
                and instance.storage_backend is not None
                and instance.storage_backend.id not in deleted_backend_ids
            ):
                self.object_storage.delete(
                    media_object.id,
                    backend=instance.storage_backend,
                )
                deleted_backend_ids.add(instance.storage_backend.id)
