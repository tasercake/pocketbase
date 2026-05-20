#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "--help" ]]; then
  cat <<'USAGE'
Install deterministic native build prerequisites for HDR thumbnail support on Ubuntu/Debian.
USAGE
  exit 0
fi

if ! command -v apt-get >/dev/null 2>&1; then
  echo "install-deps-ubuntu.sh requires apt-get" >&2
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive
sudo apt-get update
sudo apt-get install -y --no-install-recommends \
  ca-certificates \
  git \
  cmake \
  ninja-build \
  build-essential \
  pkg-config \
  libjpeg-dev \
  libpng-dev \
  zlib1g-dev
