# Testing HDR thumbnails

## Default test suite

Run the default upstream-compatible build and tests:

```sh
go test ./...
go build ./examples/base
```

Expected result: all packages pass and the base example binary builds without HDR native dependencies.

## HDR-enabled test suite

Run the scripted dependency setup and HDR build/tests:

```sh
scripts/hdrthumb/install-deps-ubuntu.sh
scripts/hdrthumb/build-libultrahdr.sh
scripts/hdrthumb/build-pocketbase-hdr.sh
CGO_ENABLED=1 go test -tags hdr_thumbs ./...
```

Expected result: all packages pass, including HDR backend and filesystem tests.

## Fixture verification

The current fixtures are in `tests/data/hdr/` and are documented in `docs/hdr-thumbnails/fixture-analysis.md`.

Useful commands:

```sh
go test ./tools/hdrthumb
CGO_ENABLED=1 go test -tags hdr_thumbs ./tools/hdrthumb ./tools/filesystem ./apis
exiftool -a -G1 -s -json tests/data/hdr/current-photo-1.jpg
identify -verbose tests/data/hdr/current-photo-1.jpg
```

For a generated thumbnail, verify:

- HTTP response content type is `image/jpeg`.
- Requested dimensions match the `thumb` geometry.
- Pure-Go marker scan still finds MPF and ISO 21496/JPEG_R metadata.
- The libultrahdr helper can probe/decode the generated file.

Example helper probe:

```sh
.deps/hdrthumb/bin/hdrthumb-helper probe /path/to/thumb.jpg
```

## Live API verification

After enabling `photos.image` with policy `require`, request a known HDR fixture record:

```sh
curl -fL -o /tmp/hdr-thumb.jpg \
  'https://<host>/api/files/photos/<recordId>/<filename>?thumb=1200x0'
file /tmp/hdr-thumb.jpg
.deps/hdrthumb/bin/hdrthumb-helper probe /tmp/hdr-thumb.jpg
```

A detected HDR source must not fall back to a flattened SDR thumbnail when HDR generation fails.
