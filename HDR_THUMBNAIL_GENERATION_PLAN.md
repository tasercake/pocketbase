# HDR Thumbnail Generation Plan for PocketBase Fork

## Goal

Add first-class HDR thumbnail generation to PocketBase file fields. Gallery thumbnails must remain HDR-capable derivatives, not SDR replacements.

The fork should generate resized thumbnails that preserve or regenerate the HDR representation required by the source format. For gain-map images, this means resizing both the base image and gain map, then writing valid gain-map metadata for the derived image.

## Current PocketBase Behavior

Relevant files:

- `apis/file.go`
- `tools/filesystem/filesystem.go`

Current request flow:

1. `GET /api/files/{collection}/{recordId}/{filename}?thumb={size}` enters `apis/file.go`.
2. PocketBase validates `{size}` against the file field's configured `Thumbs`.
3. If the thumbnail is missing, it calls `api.createThumb(...)`.
4. `api.createThumb(...)` delegates actual generation to `fsys.CreateThumb(...)`.
5. `tools/filesystem/filesystem.go:System.CreateThumb(...)` performs:
   - `imaging.Decode(...)`
   - `imaging.Resize(...)`, `imaging.Fit(...)`, or `imaging.Fill(...)`
   - `imaging.Encode(...)`

This pipeline is not HDR-capable because it converts the image into ordinary Go image buffers and re-encodes without HDR/gain-map metadata.

## Required New Behavior

When a thumbnail request targets an HDR-capable source image:

1. Detect HDR source format and HDR encoding model.
2. Route to an HDR-aware thumbnail backend.
3. Generate resized HDR derivative.
4. Store derivative in PocketBase's existing thumbnail cache path.
5. Serve derivative via the normal `?thumb=` URL.
6. Preserve PocketBase API shape and collection configuration.

Existing URLs should continue to work:

```text
/api/files/photos/{recordId}/{filename}?thumb=1200x0
```

But for HDR sources, the generated thumbnail must be HDR-capable.

## HDR Formats to Support

### Phase 1: Ultra HDR JPEG / JPEG_R gain maps

Target first because it has a concrete library path:

- Google `libultrahdr`
- Android Ultra HDR / JPEG_R compatible gain-map model

Required behavior:

- Detect Ultra HDR JPEG.
- Decode base image and gain map.
- Resize base image to requested dimensions.
- Resize gain map consistently.
- Re-encode as valid Ultra HDR JPEG.
- Preserve/update gain-map metadata.

### Phase 2: Apple/Adobe gain-map JPEGs

Target after Phase 1 validation.

Required behavior:

- Detect gain-map metadata in JPEG APP/XMP/MPF structures.
- Extract base image and gain-map payload.
- Resize both layers.
- Rebuild metadata with correct dimensions.
- Encode valid HDR-capable JPEG derivative.

If Apple's exact structure differs from Ultra HDR, add format-specific adapter.

### Phase 3: HDR HEIC/AVIF/JPEG XL

Target once JPEG gain-map path works.

Candidate libraries:

- `libheif`
- `libavif`
- `libjxl`
- `libvips` as a higher-level adapter where it preserves required metadata correctly

## Library Stack

### Keep current pure-Go path for non-HDR images

Existing `imaging` path remains for ordinary non-HDR images.

### Add cgo HDR backend

Add optional build tag:

```text
hdr_thumbs
```

Suggested packages/layout:

```text
tools/hdrthumb/
  detect.go
  geometry.go
  backend.go
  backend_disabled.go
  backend_ultrahdr.go
  backend_vips.go
  ultrahdr_cgo.go
  vips_cgo.go
```

Build modes:

```text
# default build: existing behavior, HDR thumb requests return explicit unsupported error for HDR source
# HDR build: enables cgo backend
CGO_ENABLED=1 go build -tags hdr_thumbs ./examples/base
```

### `libultrahdr`

Use for JPEG_R / Ultra HDR:

```text
https://github.com/google/libultrahdr
```

Needed wrapper functions:

- detect Ultra HDR JPEG
- decode base image
- decode gain map / HDR representation
- encode Ultra HDR JPEG from resized layers
- expose metadata needed to rebuild output

### `libvips` via Go binding

Use for high-quality resizing and color/profile handling:

Candidates:

- `github.com/davidbyttow/govips/v2/vips`
- `github.com/h2non/bimg`

Prefer `govips` for direct API control.

Use cases:

- resize base raster
- resize gain map raster when represented as normal image buffer
- future HEIC/AVIF/JXL support if build delegates preserve required HDR metadata

## API/Config Design

Extend file field thumbnail options without breaking existing config.

Proposed field additions:

```json
{
  "thumbs": ["400x0", "1200x0"],
  "thumbsHdr": true,
  "thumbsHdrPolicy": "require"
}
```

Policy values:

- `require`: for HDR source, thumbnail generation must produce HDR output or return error.
- `off`: use existing behavior.

For this fork, use `require` for the photo gallery collection.

If schema changes are too invasive initially, use an app-level setting first:

```text
PB_HDR_THUMBS=require
```

Then later expose field-level settings in admin UI.

## Code Changes

### 1. Add HDR detection before current thumbnail generation

File:

```text
tools/filesystem/filesystem.go
```

Current function:

```go
func (s *System) CreateThumb(originalName string, thumbName string, thumbSize string) (*blob.Attributes, error)
```

New shape:

```go
func (s *System) CreateThumb(originalName string, thumbName string, thumbSize string) (*blob.Attributes, error) {
    // open original
    // detect content type + HDR type
    // if HDR and HDR thumbnails required:
    //     return hdrthumb.Create(...)
    // else:
    //     existing imaging path
}
```

### 2. Implement detection

File:

```text
tools/hdrthumb/detect.go
```

Detection returns:

```go
type Kind string

const (
    KindNone Kind = "none"
    KindUltraHDRJPEG Kind = "ultrahdr_jpeg"
    KindAdobeGainMapJPEG Kind = "adobe_gain_map_jpeg"
    KindAppleGainMapJPEG Kind = "apple_gain_map_jpeg"
    KindHDRHEIC Kind = "hdr_heic"
    KindHDRAVIF Kind = "hdr_avif"
    KindHDRJXL Kind = "hdr_jxl"
)
```

Initial detection methods:

- JPEG marker scan
- XMP string scan for gain-map namespaces
- MPF APP2 scan
- `libultrahdr` detection if available

### 3. Implement geometry parsing once

File:

```text
tools/hdrthumb/geometry.go
```

Reuse PocketBase thumbnail grammar:

```text
WxH
WxHt
WxHb
WxHf
0xH
Wx0
```

Return target dimensions and crop/fit mode.

### 4. Implement Ultra HDR backend

Files:

```text
tools/hdrthumb/backend_ultrahdr.go
tools/hdrthumb/ultrahdr_cgo.go
```

Pipeline:

1. Read original bytes.
2. Use `libultrahdr` to decode JPEG_R.
3. Extract base image + gain map representation.
4. Resize both with same crop/fit transform.
5. Encode new JPEG_R.
6. Write to PocketBase blob storage with content type `image/jpeg`.

### 5. Integrate with file API content-type allowlist

File:

```text
apis/file.go
```

Current thumbnailable types:

```go
image/png
image/jpg
image/jpeg
image/gif
image/webp
```

Add types once corresponding backend exists:

```go
image/heic
image/heif
image/avif
image/jxl
```

JPEG HDR path works under existing `image/jpeg`.

### 6. Add tests

Test locations:

```text
tools/hdrthumb/*_test.go
tools/filesystem/filesystem_test.go
apis/file_test.go
```

Test assets:

```text
tests/data/hdr/ultrahdr-sample.jpg
tests/data/hdr/apple-gainmap-sample.jpg
tests/data/hdr/adobe-gainmap-sample.jpg
```

Assertions:

- generated thumbnail has target dimensions
- output still detected as HDR/gain-map
- output has valid gain-map dimensions/metadata
- output content type is correct
- `?thumb=` endpoint returns HDR derivative
- unsupported HDR source with `require` policy returns error, not destructive output

## Admin UI Changes

PocketBase admin UI lives under:

```text
ui/
```

Need expose file-field HDR controls eventually:

- checkbox: `Generate HDR thumbnails when source is HDR`
- select: `HDR policy: require/off`

Initial implementation may use environment variable to avoid UI work during proof of concept.

## Build/Deployment Impact

Default upstream-compatible build remains pure Go.

HDR build requires native dependencies:

```text
libvips
libultrahdr
C/C++ compiler
pkg-config
CGO_ENABLED=1
```

For `tasercake-cms`, install native deps on VM and build fork binary there.

Expected build command:

```bash
CGO_ENABLED=1 go build -tags hdr_thumbs -o pocketbase ./examples/base
```

## Verification Plan

1. Use one known HDR original from current photo collection.
2. Generate `1200x0` thumbnail through PocketBase API.
3. Download generated thumbnail.
4. Verify with:
   - `exiftool`
   - JPEG marker scanner
   - `libultrahdr` decode test
   - browser/device display test if needed
5. Confirm generated file remains HDR-capable.
6. Run all 32 uploaded images through thumbnail generation.

## Risk

High. Correct gain-map thumbnail generation is format-specific. This is not metadata-copy work. It is paired-image derivative generation.

Main risk areas:

- Apple gain-map format details
- browser support variance
- native dependency packaging
- cache invalidation when thumbnail backend changes
- keeping upstream PocketBase mergeable

## Success Criteria

- Fork builds a PocketBase binary with HDR thumbnail support.
- Existing `photos.image` field can request `?thumb=1200x0`.
- For HDR source images, returned thumbnail remains HDR-capable.
- Metadata validators detect HDR/gain-map data in generated thumbnail.
- No manual external derivative upload needed for normal CMS use.
