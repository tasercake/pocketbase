#!/usr/bin/env bash
set -euo pipefail

export CGO_ENABLED="${CGO_ENABLED:-1}"
export CGO_CFLAGS="${CGO_CFLAGS:-}"
export CGO_LDFLAGS="${CGO_LDFLAGS:-}"

# Build the HDR-enabled Go target. The Go backend shells out to the deterministic
# hdrthumb-helper binary installed by build-libultrahdr.sh; fail early if the
# helper/libultrahdr bundle has not been built.
PREFIX="${HDRTHUMB_PREFIX:-$PWD/.deps/hdrthumb}"
if [[ ! -x "$PREFIX/bin/hdrthumb-helper" ]]; then
  echo "missing $PREFIX/bin/hdrthumb-helper; run scripts/hdrthumb/build-libultrahdr.sh first" >&2
  exit 1
fi
if [[ -d "$PREFIX/lib/pkgconfig" ]]; then
  export PKG_CONFIG_PATH="$PREFIX/lib/pkgconfig:${PKG_CONFIG_PATH:-}"
fi
export HDRTHUMB_HELPER="${HDRTHUMB_HELPER:-$PREFIX/bin/hdrthumb-helper}"

go build -tags hdr_thumbs "$@" ./examples/base
