#!/bin/bash
# Builds laserd and the laser CLI into app/build/go (arm64 + amd64 with
# `universal`). Pure Go, no cgo.
set -euo pipefail
cd "$(dirname "$0")/../.."
out=app/build/go
mkdir -p "$out"
ver="${VERSION:-dev}"
ld="-s -w -X github.com/jamalxcode/laser-email/internal/daemon.Version=$ver"
if [ -n "${GOOGLE_CLIENT_ID:-}" ]; then
  ld="$ld -X main.googleClientID=$GOOGLE_CLIENT_ID -X main.googleClientSecret=${GOOGLE_CLIENT_SECRET:-}"
fi
build() { # os arch suffix
  for cmd in laserd laser; do
    CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -trimpath -ldflags "$ld" -o "$out/$cmd$3" ./cmd/$cmd
  done
}
if [ "${1:-}" = universal ]; then
  build darwin arm64 -arm64
  build darwin amd64 -amd64
  for cmd in laserd laser; do
    lipo -create -output "$out/$cmd" "$out/$cmd-arm64" "$out/$cmd-amd64"
    rm "$out/$cmd-arm64" "$out/$cmd-amd64"
  done
else
  build "$(go env GOOS)" "$(go env GOARCH)" ""
fi
ls -l "$out"
