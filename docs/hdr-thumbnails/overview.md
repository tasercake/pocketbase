# HDR thumbnails overview

PocketBase HDR thumbnail support keeps the normal file URL contract:

```text
/api/files/{collection}/{recordId}/{filename}?thumb={size}
```

When a file field enables HDR thumbnails with policy `require`, HDR source images are routed through the HDR backend before the thumbnail is stored and served. Non-HDR sources keep the standard PocketBase thumbnail path.

## Supported formats

The current supported HDR output path is Ultra HDR / JPEG_R gain-map JPEG (`image/jpeg`) as represented by the target `photos` fixtures in `tests/data/hdr/`.

Detection evidence includes:

- JPEG APP2 `MPF` metadata with multiple images.
- JPEG APP2 `urn:iso:std:iso:ts:21496:-1` marker.
- XMP `hdrgm:Version` gain-map metadata.

The generated thumbnail remains an SDR-compatible JPEG with Ultra HDR gain-map metadata, so clients without HDR support can still decode the base image.

## Unsupported formats and failures

The pure-Go detector can identify several gain-map structures, but the native generation backend currently only writes Ultra HDR / JPEG_R JPEG thumbnails. With policy `require`:

- HDR source + supported backend unavailable: request fails with a non-2xx API response.
- HDR source + unsupported HDR kind: request fails with a non-2xx API response.
- HDR source + native generation error: request fails with a non-2xx API response.
- Non-HDR source: normal thumbnail generation is used.

The HDR-required path never silently returns a flattened SDR derivative for a detected HDR source.

## File field settings

File fields expose:

- `hdrThumbs`: enables HDR thumbnail generation controls.
- `hdrThumbsPolicy`: `off` or `require`.

Effective behavior:

- Missing or empty policy normalizes to `off`.
- `hdrThumbs=false` forces policy `off`.
- `hdrThumbs=true` and `hdrThumbsPolicy=require` require HDR output for detected HDR sources.

## Cache and storage behavior

Generated thumbnails are written through PocketBase's existing filesystem abstraction, including S3/R2-backed storage.

Cache directories are separated by policy/source kind:

- Standard thumbnails: `thumbs_{filename}/`.
- HDR-required thumbnails for detected HDR sources: `thumbs_hdr_{filename}/`.

This prevents stale SDR thumbnails from being served after enabling `require` for HDR sources.

## Fixtures

See `docs/hdr-thumbnails/fixture-analysis.md` for the current `photos` record IDs, source filenames, dimensions, and marker evidence used by automated tests.
