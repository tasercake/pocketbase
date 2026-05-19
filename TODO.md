# HDR Thumbnail Fork TODO

## Repository

- [x] Fork `pocketbase/pocketbase` to `tasercake/pocketbase`.
- [x] Clone fork to VM.
- [x] Create branch `hdr-thumbnail-generation-plan`.
- [x] Add initial implementation plan.

## Source Investigation

- [x] Identify current thumbnail entrypoint: `apis/file.go`.
- [x] Identify current thumbnail implementation: `tools/filesystem/filesystem.go`.
- [x] Confirm current implementation uses `imaging.Decode/Resize/Encode`.
- [x] Confirm current implementation cannot preserve/generate HDR gain-map thumbnails.

## HDR Sample Analysis

- [ ] Select one uploaded photo known to display HDR.
- [ ] Download original from PocketBase/R2.
- [ ] Inspect with `exiftool`.
- [ ] Write JPEG marker scanner for APP/XMP/MPF/gain-map data.
- [ ] Determine exact HDR type: Ultra HDR, Apple gain map, Adobe gain map, HEIC/AVIF/JXL, or other.
- [ ] Save detection notes under `docs/hdr-thumbnails/sample-analysis.md`.

## Library Evaluation

- [ ] Build/install `libultrahdr` on `tasercake-cms` VM.
- [ ] Prove `libultrahdr` can detect/decode selected sample if Ultra HDR JPEG.
- [ ] Install `libvips` with needed delegates.
- [ ] Test `govips` resize preserving high-bit-depth/color data.
- [ ] Decide between `govips` and direct C wrapper for resize layer.
- [ ] Document native dependency versions.

## Fork Architecture

- [ ] Create package `tools/hdrthumb`.
- [ ] Add `tools/hdrthumb/detect.go`.
- [ ] Add `tools/hdrthumb/geometry.go`.
- [ ] Add disabled default backend for builds without `hdr_thumbs` tag.
- [ ] Add cgo backend behind `hdr_thumbs` build tag.
- [ ] Add Ultra HDR wrapper files.
- [ ] Keep non-HDR path unchanged.

## PocketBase Integration

- [ ] Modify `tools/filesystem/filesystem.go:System.CreateThumb` to route HDR inputs.
- [ ] Add policy flag/env var: `PB_HDR_THUMBS=require`.
- [ ] Make HDR source + required policy fail if HDR backend unavailable.
- [ ] Preserve existing thumbnail cache naming.
- [ ] Preserve existing `?thumb=` API shape.
- [ ] Add content-type support for future HDR formats after backend works.

## Ultra HDR JPEG Backend

- [ ] Detect Ultra HDR JPEG reliably.
- [ ] Decode base image.
- [ ] Decode/extract gain map.
- [ ] Compute PocketBase thumbnail geometry.
- [ ] Resize base image.
- [ ] Resize gain map with same transform.
- [ ] Re-encode valid Ultra HDR JPEG.
- [ ] Verify output with `libultrahdr` decoder.
- [ ] Verify output metadata with `exiftool`/marker scanner.

## Apple/Adobe Gain Map Backend

- [ ] Identify exact metadata in Krishna's originals.
- [ ] Parse gain-map metadata.
- [ ] Extract gain-map image payload.
- [ ] Resize base and gain map together.
- [ ] Rebuild metadata with correct dimensions.
- [ ] Verify output on supported HDR client.

## Tests

- [ ] Add HDR sample fixtures under `tests/data/hdr/`.
- [ ] Add unit tests for geometry parser.
- [ ] Add unit tests for HDR detection.
- [ ] Add integration test for `System.CreateThumb` HDR path.
- [ ] Add API test for `GET /api/files/...?...thumb=...` returning HDR derivative.
- [ ] Add failure test: HDR source + required policy + missing backend.
- [ ] Run `go test ./...`.

## Admin UI / Config

- [ ] Decide first config surface: env var or file-field settings.
- [ ] If field settings: extend file field schema.
- [ ] If field settings: update Admin UI under `ui/`.
- [ ] If field settings: add migration/backward compatibility.

## Build and Deployment

- [ ] Create build docs for HDR-enabled fork.
- [ ] Build on `tasercake-cms` VM with `CGO_ENABLED=1 -tags hdr_thumbs`.
- [ ] Stop current PocketBase service.
- [ ] Backup `/var/lib/pocketbase/pb_data`.
- [ ] Replace binary with fork binary.
- [ ] Start service.
- [ ] Smoke test API/admin.
- [ ] Generate one HDR thumbnail through PocketBase.
- [ ] Generate all gallery HDR thumbnails.

## Verification

- [ ] Confirm generated thumbnails are present in R2.
- [ ] Confirm `?thumb=1200x0` returns HDR-capable derivative.
- [ ] Confirm original files unchanged.
- [ ] Confirm Astro/gallery can use normal PocketBase thumb URLs.
- [ ] Document final working pipeline.
