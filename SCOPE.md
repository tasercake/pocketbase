# Scope: HDR Thumbnail Support in PocketBase Fork

## Objective

PocketBase must generate HDR-capable thumbnails for HDR source images using the normal PocketBase file thumbnail flow.

A photo uploaded through the PocketBase admin UI should be usable in a gallery with thumbnail URLs like:

```text
/api/files/photos/{recordId}/{filename}?thumb=1200x0
```

For HDR originals, that thumbnail response must itself remain HDR-capable.

## Required Functionality

- Detect when an uploaded image contains HDR data.
- Generate resized thumbnails that preserve/regenerate HDR representation.
- Support gain-map based HDR images – need full support for the most common formats.
- Keep PocketBase's existing thumbnail URL/API shape.
- Store generated thumbnails in PocketBase's existing file storage backend, including R2/S3.
- Keep original uploaded files byte-preserved.
- Fail clearly when HDR thumbnail generation is required but unsupported for a given image.
- Allow non-HDR images to continue using normal thumbnail generation.
- Admin UI controls for HDR thumbnail policy on `file` fields.

## Required User Experience

- Upload HDR photo in PocketBase admin.
- Configure thumbnail sizes on the file field as usual.
- Use normal `?thumb=` URL in frontend gallery.
- Receive HDR thumbnail, not a flattened derivative.
- No external manual thumbnail upload step.
- No separate gallery-specific media workflow.

## Supported Environment

- Self-hosted PocketBase fork
- Linux VM
- SQLite database
- R2/S3 file storage
- Photo collection with image file field

Native image-processing dependencies are acceptable if needed, but must be installed at/before build time programmatically and deterministically – no manual intervention should be needed.

## Success Criteria

- One known HDR source photo produces a resized HDR thumbnail through PocketBase.
- Generated thumbnail is verified as HDR-capable by metadata/format inspection.
- Thumbnail is served from the existing PocketBase `?thumb=` endpoint.
- Generated thumbnail is stored in R2 through PocketBase storage.
- Existing non-HDR thumbnail behavior remains functional.
- Automated tests with fixture images (get 2-3 from current photo collection).
- Document build/deployment requirements for HDR-enabled PocketBase.

## Out of Scope

- Separate CMS/gallery product.
- Manual pre-generated thumbnail upload workflow.
