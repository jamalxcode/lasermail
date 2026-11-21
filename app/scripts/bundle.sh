#!/bin/bash
# Assembles app/build/Laser Email.app: the SwiftUI shell, laserd next to it,
# the icon, the version, and an ad-hoc signature.
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${VERSION:-0.1.0}"
BUILD="${BUILD:-1}"
swift build -c release ${ARCHS:+$ARCHS}
bin=$(swift build -c release ${ARCHS:+$ARCHS} --show-bin-path)
app="build/Laser Email.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$bin/LaserEmail" "$app/Contents/MacOS/LaserEmail"
cp build/go/laserd "$app/Contents/MacOS/laserd"
sed -e "s/__VERSION__/$VERSION/" -e "s/__BUILD__/$BUILD/" Info.plist > "$app/Contents/Info.plist"
# Icon: 1024 px PNG → .icns
iconset=$(mktemp -d)/AppIcon.iconset
mkdir -p "$iconset"
for s in 16 32 128 256 512; do
  sips -z $s $s Resources/AppIcon.png --out "$iconset/icon_${s}x${s}.png" >/dev/null
  sips -z $((s*2)) $((s*2)) Resources/AppIcon.png --out "$iconset/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$app/Contents/Resources/AppIcon.icns"
codesign --force --sign - "$app/Contents/MacOS/laserd"
codesign --force --deep --sign - "$app"
echo "$app"
