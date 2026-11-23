#!/usr/bin/env bash
# Cross-compile OpenGit for every supported platform into ./dist (pure Go, no C toolchain needed).
set -euo pipefail
cd "$(dirname "$0")/.."
rm -rf dist && mkdir dist
for t in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
  os=${t%/*}; arch=${t#*/}; ext=""; [ "$os" = windows ] && ext=".exe"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w" -o "dist/opengit-$os-$arch$ext" ./cmd/opengit
  echo "built dist/opengit-$os-$arch$ext"
done
