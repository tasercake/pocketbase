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
- Support gain-map based HDR images, starting with the format used by the current photo archive.
- Keep PocketBase's existing thumbnail URL/API shape.
- Store generated thumbnails in PocketBase's existing file storage backend, including R2/S3.
- Keep original uploaded files byte-preserved.
- Fail clearly when HDR thumbnail generation is required but unsupported for a given image.
- Allow non-HDR images to continue using normal thumbnail generation.

## Required User Experience

- Upload HDR photo in PocketBase admin.
- Configure thumbnail sizes on the file field as usual.
- Use normal `?thumb=` URL in frontend gallery.
- Receive HDR thumbnail, not a flattened derivative.
- No external manual thumbnail upload step.
- No separate gallery-specific media workflow.

## Supported Environment

Initial target deployment:

- Self-hosted PocketBase fork
- Linux VM
- SQLite database
- R2/S3 file storage
- Photo collection with image file field

Native image-processing dependencies are acceptable if needed.

## Initial Success Criteria

- One known HDR source photo produces a resized HDR thumbnail through PocketBase.
- Generated thumbnail is verified as HDR-capable by metadata/format inspection.
- Thumbnail is served from the existing PocketBase `?thumb=` endpoint.
- Generated thumbnail is stored in R2 through PocketBase storage.
- Existing non-HDR thumbnail behavior remains functional.

## Out of Scope Initially

- Full rewrite of PocketBase media system.
- Replacing PocketBase admin UI.
- Separate CMS/gallery product.
- Manual pre-generated thumbnail upload workflow.
- Support for every HDR image format on day one.

## Expansion Goals

After the first working HDR format:

- Broaden detection/support to Apple, Adobe, Ultra HDR, HEIC/AVIF/JPEG XL as needed.
- Add admin UI controls for HDR thumbnail policy.
- Add automated tests with fixture images.
- Document build/deployment requirements for HDR-enabled PocketBase.
