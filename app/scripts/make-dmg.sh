#!/bin/bash
# Packs Laser Email.app into a disk image with an Applications link.
set -euo pipefail
cd "$(dirname "$0")/.."
out="${1:?usage: make-dmg.sh out.dmg}"
stage=$(mktemp -d)
cp -R "build/Laser Email.app" "$stage/"
ln -s /Applications "$stage/Applications"
rm -f "$out"
hdiutil create -volname "Laser Email" -srcfolder "$stage" -ov -format UDZO "$out"
