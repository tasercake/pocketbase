#!/usr/bin/env bash
set -euo pipefail

# Pinned source used by the HDR thumbnail milestone. Override only for deliberate upgrades.
LIBULTRAHDR_REPO="${LIBULTRAHDR_REPO:-https://github.com/google/libultrahdr.git}"
LIBULTRAHDR_REF="${LIBULTRAHDR_REF:-d52a0d13814ca399fc8a07e23de1d2c63f0e8404}"
PREFIX="${HDRTHUMB_PREFIX:-$PWD/.deps/hdrthumb}"
SRC_DIR="${HDRTHUMB_SRC_DIR:-$PWD/.deps/src/libultrahdr}"
BUILD_DIR="${HDRTHUMB_BUILD_DIR:-$PWD/.deps/build/libultrahdr}"
HELPER_BUILD_DIR="${HDRTHUMB_HELPER_BUILD_DIR:-$PWD/.deps/build/hdrthumb-helper}"

mkdir -p "$(dirname "$SRC_DIR")" "$(dirname "$BUILD_DIR")" "$PREFIX"
if [[ ! -d "$SRC_DIR/.git" ]]; then
  git clone "$LIBULTRAHDR_REPO" "$SRC_DIR"
fi
git -C "$SRC_DIR" fetch --tags --force origin "$LIBULTRAHDR_REF"
git -C "$SRC_DIR" checkout --detach "$LIBULTRAHDR_REF"

git -C "$SRC_DIR" submodule update --init --recursive

cmake -S "$SRC_DIR" -B "$BUILD_DIR" -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_INSTALL_PREFIX="$PREFIX" \
  -DBUILD_SHARED_LIBS=OFF
cmake --build "$BUILD_DIR"
cmake --install "$BUILD_DIR"

cmake -S "$PWD/tools/hdrthumb/cmd/hdrthumb-helper" -B "$HELPER_BUILD_DIR" -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_INSTALL_PREFIX="$PREFIX" \
  -DCMAKE_PREFIX_PATH="$PREFIX" \
  -DPKG_CONFIG_USE_CMAKE_PREFIX_PATH=ON
PKG_CONFIG_PATH="$PREFIX/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}" cmake --build "$HELPER_BUILD_DIR"
cmake --install "$HELPER_BUILD_DIR"

echo "libultrahdr and hdrthumb-helper installed to $PREFIX"
