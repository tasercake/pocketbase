# Building HDR thumbnail support

HDR thumbnail generation requires the normal Go toolchain plus native Ultra HDR helper dependencies.

## Default build

The default build does not require native HDR dependencies:

```sh
go test ./...
go build ./examples/base
```

In this build, HDR detection and policy handling are available, but the HDR generation backend is disabled. If an HDR source requires HDR output, the request fails clearly instead of flattening.

## Ubuntu native dependencies

Install deterministic build prerequisites:

```sh
scripts/hdrthumb/install-deps-ubuntu.sh
```

This installs `cmake`, `ninja`, C/C++ build tools, `pkg-config`, and image codec development headers through `apt-get`.

## Build libultrahdr helper

Build the pinned Google libultrahdr helper bundle:

```sh
scripts/hdrthumb/build-libultrahdr.sh
```

By default the bundle is installed under:

```text
.deps/hdrthumb/
```

Override with `HDRTHUMB_PREFIX=/path/to/prefix` when needed.

## Build HDR-enabled PocketBase

```sh
scripts/hdrthumb/build-pocketbase-hdr.sh
```

The script verifies that `hdrthumb-helper` exists, exports the helper path, and runs:

```sh
go build -tags hdr_thumbs ./examples/base
```

For tests:

```sh
CGO_ENABLED=1 go test -tags hdr_thumbs ./...
```

## Runtime environment

The HDR build locates `hdrthumb-helper` in this order:

1. `HDRTHUMB_HELPER` environment variable.
2. `hdrthumb-helper` in `PATH`.
3. `.deps/hdrthumb/bin/hdrthumb-helper` in the current or parent working directory.

For systemd deployments, set `HDRTHUMB_HELPER` to the installed helper path if the working directory may not contain `.deps/hdrthumb`.
