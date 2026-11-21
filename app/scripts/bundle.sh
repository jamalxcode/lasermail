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
chmod 755 "$app/Contents/MacOS/LaserEmail"
cp build/go/laserd "$app/Contents/MacOS/laserd"
sed -e "s/__VERSION__/$VERSION/" -e "s/__BUILD__/$BUILD/" Info.plist > "$app/Contents/Info.plist"
# What's new in this version (shown by Check for Updates…), from CHANGELOG.md.
awk -v h="## $VERSION" '$0 == h {on=1; next} /^## / {on=0} on' ../CHANGELOG.md | sed '/^$/d' > "$app/Contents/Resources/WhatsNew.txt"
# The updater helper: its own small app, the only networked Swift code.
helper="$app/Contents/Helpers/Laser Email Updater.app"
mkdir -p "$helper/Contents/MacOS"
cp "$bin/LaserEmailUpdater" "$helper/Contents/MacOS/LaserEmailUpdater"
chmod 755 "$helper/Contents/MacOS/LaserEmailUpdater"
cat > "$helper/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
	<key>CFBundleName</key><string>Laser Email Updater</string>
	<key>CFBundleIdentifier</key><string>company.sala.laser-email.updater</string>
	<key>CFBundleExecutable</key><string>LaserEmailUpdater</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>$VERSION</string>
	<key>LSUIElement</key><true/>
	<key>LSMinimumSystemVersion</key><string>14.0</string>
</dict></plist>
PLIST
# Icon: 1024 px PNG → .icns
iconset=$(mktemp -d)/AppIcon.iconset
mkdir -p "$iconset"
for s in 16 32 128 256 512; do
  sips -z $s $s Resources/AppIcon.png --out "$iconset/icon_${s}x${s}.png" >/dev/null
  sips -z $((s*2)) $((s*2)) Resources/AppIcon.png --out "$iconset/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$app/Contents/Resources/AppIcon.icns"
codesign --force --sign - "$app/Contents/MacOS/laserd"
codesign --force --sign - "$helper"
codesign --force --deep --sign - "$app"
echo "$app"
