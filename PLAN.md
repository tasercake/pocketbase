# Plan: HDR Thumbnail Support in PocketBase Fork

## Immutable Requirements for This Plan

Do not edit the approved scope file. This plan is self-contained so implementers do not need to read it.

Required outcome:

- PocketBase generates HDR-capable thumbnails for HDR source images through the normal file thumbnail URL shape:

  ```text
  /api/files/{collection}/{recordId}/{filename}?thumb={size}
  ```

- HDR source thumbnails remain HDR-capable.
- Original uploaded files remain byte-preserved.
- Generated thumbnails are stored through PocketBase's existing file storage abstraction, including R2/S3.
- HDR thumbnail generation supports common gain-map HDR formats.
- If HDR output is required but cannot be produced, PocketBase fails clearly and never silently returns a flattened derivative.
- Non-HDR image thumbnails keep current behavior.
- File fields get admin UI controls for HDR thumbnail policy.
- Native image-processing dependencies are allowed, but install/build must be deterministic and scriptable.
- Automated tests use 2–3 fixtures from the current photo collection.
- Build/deployment docs are part of delivery.

## Strategy

Add HDR thumbnail generation inside PocketBase's existing lazy thumbnail flow.

Current PocketBase flow:

1. `apis/file.go` handles `GET /api/files/...`.
2. Requested `thumb` value is checked against default sizes or the file field's `Thumbs`.
3. If cached thumbnail is missing, `api.createThumb(...)` calls `fsys.CreateThumb(...)`.
4. `tools/filesystem/filesystem.go:System.CreateThumb(...)` decodes, resizes, encodes, and writes the derived object through the active storage backend.

New flow:

1. Same API URL and same field `Thumbs` configuration.
2. If file field HDR policy is enabled and source image is HDR, route to HDR backend.
3. HDR backend generates HDR-capable derivative and writes it through the same storage abstraction.
4. If HDR output is required and generation cannot happen, return a typed error that `apis/file.go` turns into a non-2xx response.
5. Existing non-HDR path remains upstream-compatible.

## Phase 1: Fixture and Format Analysis

### 1.1 Select fixtures from current collection

Use 2–3 images from `tasercake-cms` `photos` collection that are known or suspected HDR images.

Create:

```text
tests/data/hdr/current-photo-1.jpg
tests/data/hdr/current-photo-2.jpg
tests/data/hdr/current-photo-3.jpg
```

Also create immutable metadata snapshots:

```text
tests/data/hdr/current-photo-1.exiftool.json
tests/data/hdr/current-photo-1.markers.txt
tests/data/hdr/current-photo-1.identify.txt
```

### 1.2 Identify exact HDR encoding model

Use:

```text
exiftool -a -G1 -s -json
identify -verbose
custom JPEG marker scanner
libultrahdr probe
```

Classify each fixture as one of:

- Ultra HDR / JPEG_R gain map
- Apple gain-map JPEG
- Adobe gain-map JPEG
- HEIC/HEIF HDR or gain-map image
- AVIF HDR or gain-map image
- JPEG XL HDR
- ICC/profile-only HDR
- unknown HDR structure

### 1.3 Commit fixture analysis document

Create:

```text
docs/hdr-thumbnails/fixture-analysis.md
```

Must include:

- PocketBase record ID
- source filename
- original dimensions
- source MIME type
- detected HDR kind
- evidence for detection
- expected output MIME type
- expected verification commands

## Phase 2: File Field HDR Policy and Admin UI

### 2.1 Extend file field model

File:

```text
core/field_file.go
```

Add fields:

```go
HdrThumbs bool `form:"hdrThumbs" json:"hdrThumbs"`
HdrThumbsPolicy string `form:"hdrThumbsPolicy" json:"hdrThumbsPolicy"`
```

Supported policy values:

```text
off      - existing behavior
require  - HDR source must produce HDR-capable thumbnail or fail clearly
```

Default behavior:

- missing/empty `hdrThumbsPolicy` normalizes to `off`
- `hdrThumbs=false` forces effective policy to `off`
- old collection JSON imports successfully
- exports include normalized fields

Validation:

- reject non-empty policy values other than `off` and `require`
- accept empty value only as backward-compatible `off`

### 2.2 Update field tests

Files:

```text
core/field_file_test.go
core/collection_model_test.go
```

Test:

- defaults on empty field
- import of old JSON without HDR fields
- export/import with `hdrThumbs=true`, `hdrThumbsPolicy=require`
- invalid policy rejected
- `hdrThumbs=false` behaves as effective `off`

### 2.3 Update admin UI controls

Files to inspect/update:

```text
ui/src/fields/file/settings.js
ui/src/fields/file/init.js
ui/src/base/fieldSettings.js
ui/src/apiPreview/fieldsInfo.js
```

Add file field settings:

- checkbox: `Generate HDR thumbnails for HDR images`
- select: `HDR thumbnail policy`
  - `Off`
  - `Require HDR output`

UI behavior:

- when checkbox disabled, policy select is disabled or set to `off`
- submitted JSON always includes normalized values
- existing field forms load without errors

### 2.4 Update admin MIME presets

If supported HDR formats include HEIC/HEIF/AVIF/JXL, update file field MIME helper/preset UI so users can allow those formats without hand-entering MIME types.

Relevant starting file:

```text
ui/src/fields/file/settings.js
```

## Phase 3: HDR Detection, Errors, and Backend Abstraction

### 3.1 Add package

Create:

```text
tools/hdrthumb/
```

Files:

```text
tools/hdrthumb/types.go
tools/hdrthumb/detect.go
tools/hdrthumb/geometry.go
tools/hdrthumb/backend.go
tools/hdrthumb/backend_disabled.go
tools/hdrthumb/backend_ultrahdr.go
tools/hdrthumb/backend_vips.go
tools/hdrthumb/errors.go
```

### 3.2 Define types

```go
type Kind string

const (
    KindNone Kind = "none"
    KindUltraHDRJPEG Kind = "ultrahdr_jpeg"
    KindAppleGainMapJPEG Kind = "apple_gain_map_jpeg"
    KindAdobeGainMapJPEG Kind = "adobe_gain_map_jpeg"
    KindHDRHEIC Kind = "hdr_heic"
    KindHDRAVIF Kind = "hdr_avif"
    KindHDRJXL Kind = "hdr_jxl"
    KindUnknownHDR Kind = "unknown_hdr"
)

type Detection struct {
    Kind Kind
    ContentType string
    Evidence []string
}

type Options struct {
    Size string
    OriginalName string
    ThumbName string
    OriginalContentType string
}

type Result struct {
    ContentType string
    Bytes []byte
    Evidence []string
}
```

Entrypoints:

```go
func DetectBytes(input []byte, contentType string) (Detection, error)
func Create(input []byte, opts Options) (Result, error)
func Available() bool
```

### 3.3 Require pure-Go detection for blocking flattening

Even without native dependencies, default builds must detect supported/claimed HDR source classes well enough to avoid flattening when `require` is active.

Default build behavior:

- detect known JPEG gain-map markers/XMP/MPF where possible
- detect obvious HEIC/AVIF/JXL containers by signature/MIME
- if HDR detected and `require`, return `ErrBackendUnavailable` or `ErrUnsupportedHDRKind`
- do not invoke SDR thumbnail generation for detected HDR source with `require`

Native dependencies may be required for generation, but not for deciding to block destructive thumbnailing.

### 3.4 Define typed errors

Add typed errors:

```go
ErrHDRBackendUnavailable
ErrUnsupportedHDRKind
ErrHDRGenerationFailed
ErrHDRRequired
```

Errors must carry:

- HDR kind
- source filename
- requested thumb size
- human-readable reason

`apis/file.go` uses these typed errors to avoid fallback-to-original behavior.

## Phase 4: Integrate with PocketBase File API and Storage

### 4.1 Change thumbnail creation API

Current `CreateThumb(originalName, thumbName, thumbSize)` lacks field policy.

Add options struct:

```go
type ThumbOptions struct {
    Size string
    HdrEnabled bool
    HdrPolicy string
    SourceContentType string
}
```

Add method:

```go
CreateThumbWithOptions(originalName, thumbName string, opts ThumbOptions) (*blob.Attributes, error)
```

Keep old `CreateThumb` as compatibility wrapper using `HdrPolicy=off`.

### 4.2 Pass file-field policy from API layer

File:

```text
apis/file.go
```

At download time, `fileField` is available. Pass effective HDR policy into thumbnail creation.

For view collections, resolve both storage path and effective file-field settings explicitly:

- If the request is served through a view collection, resolve the original source record and source file field when possible.
- Use the original/source file field as the authoritative field for `Thumbs`, `Protected`, MIME expectations, and HDR policy when it can be resolved.
- If the source field cannot be resolved, use the requested view field and document this fallback.
- Add tests where view field and base field differ in `Thumbs` and HDR policy so behavior is unambiguous.

### 4.3 Override current fallback behavior for HDR-required errors

Current behavior logs thumbnail errors, mutates `event.ServedPath` back to the original, and continues through download hooks and filesystem serving.

Required new behavior:

- normal non-HDR thumbnail error: keep existing fallback behavior
- HDR source + effective policy `require` + typed HDR error: immediately return non-2xx JSON/API error
- no fallback to original
- no serving stale/generated non-HDR derivative
- no continuing into `OnFileDownloadRequest`/`fsys.Serve` for the failed HDR-required thumbnail request

Implementation in `apis/file.go` must inspect typed errors from `createThumb`/`CreateThumbWithOptions` and short-circuit before any original or cached SDR object can be served.

Tests must prove that HDR-required failures return an API error and hooks cannot accidentally serve the original/stale SDR thumbnail for that request.

### 4.4 Preserve original files

HDR backend must only read original bytes and write derived thumbnail object. It must never modify/delete/rewrite original objects.

Tests verify original content hash before/after thumbnail generation.

### 4.5 Use existing storage abstraction for all generated thumbs

All thumbnail writes must go through `tools/filesystem.System` bucket writer, same as current thumbnails.

No direct R2/S3 client inside HDR backend unless wrapped by existing filesystem abstraction.

## Phase 5: Cache Correctness

### 5.1 Prevent stale flattened thumbnails

Current cache key:

```text
thumbs_{filename}/{size}_{filename}
```

This can serve stale non-HDR thumbnails after HDR policy is enabled.

Mandatory fix: cache must be policy/backend aware for HDR-required fields.

Acceptable implementation options:

1. Add HDR cache namespace:

   ```text
   thumbs_hdr_{filename}/{size}_{filename}
   ```

2. Add backend/version suffix:

   ```text
   thumbs_{filename}/{size}_hdrv1_{filename}
   ```

3. Store sidecar metadata and validate before serving.

Choose option 1 unless code inspection reveals a better upstream-compatible path.

### 5.2 Serve correct cache path

In `apis/file.go`, when effective HDR policy is `require` and source is HDR, compute HDR cache path before checking existence.

This means API must detect source HDR before deciding served thumb path, or must validate existing thumb before serving it.

### 5.3 Delete/replace cleanup for HDR cache namespaces

Current file deletion cleanup removes only:

```text
thumbs_{filename}/
```

If HDR thumbnails use a namespace such as `thumbs_hdr_{filename}/`, deletion/replacement must remove every thumbnail namespace for that file.

Update cleanup in:

```text
core/field_file.go
```

Required behavior:

- deleting a file deletes normal thumbnail cache
- deleting a file deletes HDR thumbnail cache
- replacing a file deletes stale HDR thumbnail cache for the old file
- cleanup supports any selected backend/versioned HDR cache prefix

Add tests for orphan/stale HDR thumb cleanup.

### 5.4 Cache invalidation docs

Document:

- how to clear old thumbnail cache
- when cache naming changes
- how to regenerate HDR thumbnails

Docs are not substitute for correct cache behavior.

## Phase 6: MIME and Inline Serving Support

### 6.1 Thumbnail eligibility MIME types

File:

```text
apis/file.go
```

Update thumbnailable types for every supported HDR format:

```text
image/jpeg
image/jpg
image/heic
image/heif
image/avif
image/jxl
```

Keep existing non-HDR types.

### 6.2 Inline file serving MIME types

Inspect/update filesystem serving allowlist so HDR image types are served inline, not as attachments, when safe.

Starting file:

```text
tools/filesystem/filesystem.go
```

Add support for output MIME types generated by HDR backend.

### 6.3 Output MIME policy

For each supported source kind, define output:

- Ultra HDR / JPEG_R → `image/jpeg`
- Apple/Adobe gain-map JPEG → `image/jpeg`
- HEIC/HEIF → preserve source format only after backend support exists
- AVIF → `image/avif` after backend support exists
- JXL → `image/jxl` after backend support exists

Do not claim support for a format until detection, generation, serving, and tests all pass.

## Phase 7: HDR Geometry and Orientation

### 7.1 Match existing thumbnail size grammar

Existing size modes must behave identically:

```text
WxH   center crop
WxHt  top crop
WxHb  bottom crop
WxHf  fit inside box
0xH   resize to height preserving aspect ratio
Wx0   resize to width preserving aspect ratio
```

### 7.2 Match orientation behavior

Current path uses auto-orientation. HDR backend must produce the same visual orientation as current PocketBase thumbnails.

For gain-map sources, base layer and gain map must receive identical orientation/crop/resize transform.

### 7.3 Tests

Add dimension/orientation tests for each size mode. For HDR fixtures, verify base and gain map dimensions stay consistent.

## Phase 8: Format-Specific HDR Generation

### 8.1 Ultra HDR / JPEG_R

Use Google `libultrahdr`:

```text
https://github.com/google/libultrahdr
```

Tasks:

- deterministic build script for pinned commit/release
- cgo wrapper
- probe/detect Ultra HDR JPEG
- decode base + gain map/HDR representation
- resize both with same geometry
- encode valid Ultra HDR JPEG
- return bytes + `image/jpeg`

Verification:

- generated thumbnail decodes with `libultrahdr`
- marker scanner detects gain map
- dimensions match requested thumb size

### 8.2 Apple/Adobe gain-map JPEG

Support common gain-map JPEGs.

Tasks:

- parse JPEG APP/XMP/MPF structures
- detect Apple/Adobe gain-map namespaces
- extract gain-map payload
- decode base and gain map
- apply same transform to both
- rebuild metadata with correct derived dimensions
- encode valid JPEG with gain map

If a reliable open library supports this, wrap it. If not, implement parser/rewriter for fixture-proven formats.

### 8.3 HEIC/AVIF/JXL

After JPEG gain-map path works, add support as needed:

- `libheif` / `libavif` / `libjxl`
- or `libvips` with required delegates if it preserves required HDR metadata correctly

Each format only becomes supported when tests prove:

- detection
- generation
- output metadata
- API serving
- cache correctness

## Phase 9: Deterministic Native Dependency Setup

Add scripts:

```text
scripts/hdrthumb/install-deps-ubuntu.sh
scripts/hdrthumb/build-libultrahdr.sh
scripts/hdrthumb/build-libvips.sh
scripts/hdrthumb/build-pocketbase-hdr.sh
```

Requirements:

- pinned versions or commit hashes
- idempotent
- no manual steps
- no browser/login interactions
- fail fast with useful messages
- usable on `tasercake-cms` Linux VM

Build commands:

Default build:

```bash
go test ./...
go build ./examples/base
```

HDR build:

```bash
scripts/hdrthumb/install-deps-ubuntu.sh
scripts/hdrthumb/build-pocketbase-hdr.sh
CGO_ENABLED=1 go test -tags hdr_thumbs ./...
```

## Phase 10: Automated Tests

### 10.1 Unit tests

Add tests for:

- pure-Go HDR detection
- thumbnail geometry parser
- typed HDR errors
- file field policy validation/defaulting
- schema import/export compatibility
- MIME eligibility
- HDR cache path selection

### 10.2 Integration tests

Add tests for:

- `System.CreateThumbWithOptions` HDR path
- API `GET /api/files/...?...thumb=...` HDR path
- no fallback to original on HDR-required errors
- no stale non-HDR cache served when HDR policy is required
- original object hash unchanged
- original-file preservation checks ignore all thumbnail namespaces while still verifying originals are unchanged
- deleted/replaced originals remove associated HDR thumbnails
- non-HDR thumbnail path unchanged
- view collection file download policy behavior, including differing base/view `Thumbs` and HDR policy

### 10.3 Storage tests

Hard requirement: generated HDR thumbs must use existing storage abstraction and work with S3/R2-style storage.

Test strategy:

- unit/integration test using filesystem abstraction fake/memory/local bucket
- one reproducible S3-compatible test using MinIO or equivalent in CI/dev script
- deployment verification against actual R2 on `tasercake-cms`

### 10.4 Fixture tests

For each of 2–3 fixtures:

- original detected as HDR
- generated thumb detected as HDR
- generated dimensions correct
- output decodes successfully
- gain-map metadata exists and matches derived dimensions
- output content type correct

## Phase 11: Documentation

Add:

```text
docs/hdr-thumbnails/overview.md
docs/hdr-thumbnails/build.md
docs/hdr-thumbnails/testing.md
docs/hdr-thumbnails/operations.md
```

Must document:

- supported formats
- unsupported formats and exact failure behavior
- native dependencies
- build commands
- file field settings
- cache behavior
- clearing/regenerating thumbs
- verification commands
- deployment to `tasercake-cms`

## Phase 12: Deployment to `tasercake-cms`

### 12.1 Backup

Backup before replacing binary:

```text
/var/lib/pocketbase/pb_data
```

Store timestamped backup outside app dir.

### 12.2 Build HDR binary

On VM:

```bash
scripts/hdrthumb/install-deps-ubuntu.sh
scripts/hdrthumb/build-pocketbase-hdr.sh
```

### 12.3 Replace binary

```bash
sudo systemctl stop pocketbase
sudo cp /opt/pocketbase/pocketbase /opt/pocketbase/pocketbase.backup.$(date +%Y%m%d%H%M%S)
sudo cp ./pocketbase /opt/pocketbase/pocketbase
sudo systemctl start pocketbase
```

### 12.4 Configure `photos.image`

Set:

```text
HDR thumbnails: enabled
Policy: require
```

### 12.5 Verify on deployed data

Request:

```text
/api/files/photos/{recordId}/{filename}?thumb=1200x0
```

Verify:

- HTTP response is generated thumbnail, not original fallback
- thumbnail is HDR-capable
- thumbnail stored in R2
- original object hash unchanged
- non-HDR thumbnails still work

## Delivery Milestones

### Milestone 1: Fixtures and Detection

- 2–3 fixtures committed
- current HDR format identified
- pure-Go detector blocks flattening in default build

### Milestone 2: Policy, UI, and Routing

- file field HDR controls exist
- API passes field policy into thumbnail creation
- typed HDR errors return non-2xx when policy requires HDR

### Milestone 3: Cache and Storage Correctness

- HDR-required cache cannot serve stale non-HDR thumbnails
- generated thumbs use existing storage abstraction
- S3-compatible verification exists

### Milestone 4: First HDR Format Works

- known HDR fixture produces verified HDR thumbnail through `?thumb=`

### Milestone 5: Common Gain-Map Coverage

- common gain-map formats in target fixtures pass automated tests

### Milestone 6: VM Deployment

- HDR-enabled fork runs on `tasercake-cms`
- `photos.image` policy is enabled/required
- live `?thumb=1200x0` URLs return HDR-capable thumbnails from R2-backed storage

## Definition of Done

- The normal PocketBase `?thumb=` flow returns HDR-capable thumbnails for HDR source images.
- Common gain-map HDR formats required by the fixture set are supported.
- HDR-required unsupported cases fail clearly with non-2xx API error.
- Original files are byte-preserved.
- Generated thumbnails are stored through the existing storage backend and verified with R2/S3.
- Non-HDR thumbnail behavior remains functional.
- Admin UI exposes HDR thumbnail policy on file fields.
- Native dependency setup and build are deterministic and scripted.
- Automated fixture tests pass.
- Build/deployment documentation exists.
- HDR-enabled binary is deployed and verified on `tasercake-cms`.
