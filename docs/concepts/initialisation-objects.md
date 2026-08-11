# Initialisation Objects

Initialisation Objects hold setup bytes needed before fragmented media, such as
the initialisation section of fragmented MP4. They allow multiple media Objects
to share those bytes instead of repeating them in every Segment.

## Flow Capability

A video, audio, data, or multi Flow declares
`essence_parameters.init_segments: true` when its media Objects use
initialisation Objects. Image Flows cannot enable the capability.

The flag is a Flow-wide invariant:

- every registered media Object on an enabled Flow must have an
  `init_object_id`;
- a disabled Flow cannot use a media Object already linked to an initialisation
  Object; and
- the flag cannot change after the Flow has any Segments.

## Object Roles

Media and initialisation are persistent Object roles. An Object previously used
as media cannot later become an initialisation Object, and the reverse is also
rejected. Reusing a media Object cannot change its existing initialisation
association, including changing an absent association into a present one.

The API projects the relationship in both directions relevant to clients:

- Segment and media Object responses include nested `init_object` metadata and
  filtered `get_urls` when present.
- The initialisation Object records the Flows that reference it, but has no
  media timerange of its own.

Storage ID, tag, presigning, and verbosity filters apply consistently to media
and nested initialisation URLs.

## Lifetime and Sharing

An initialisation Object may be shared by multiple media Objects and Flows.
TAMOSS serialises relationship changes and retains the Object until its final
reference is removed.

Copying or deleting one media Object instance does not cascade to the
initialisation Object's instances. Segment and Flow deletion update references;
the deletion worker removes controlled bytes only when no retained relationship
needs them.

## Playback

The UI exposes initialisation Object links on Segment and Object detail pages.
For supported fragmented MP4 previews, its generated HLS manifest emits the
initialisation location as `EXT-X-MAP` before the associated media fragments.
Presigned locations remain transient and are not written to logs, browser
history, or error messages.

Use the [API reference](../reference/api.md) for the exact Flow, storage,
Segment, and Object schemas.
