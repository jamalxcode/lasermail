#!/bin/bash
# Runs the bundled app against `laserd --demo`, takes screenshots of the real
# window and prints them (JPEG, base64) into the log as
# "=== SCREENSHOT <name>" blocks, then checks the self-test report.
set -uo pipefail
cd "$(dirname "$0")/.."
app="build/Laser Email.app"
d=$(mktemp -d /tmp/lsr.XXXX)
export LASER_DATA="$d/data" LASER_SOCKET="$d/s.sock" LASER_SELFTEST=1 LASER_SELFTEST_REPORT="$d/report.txt"
"$app/Contents/MacOS/laserd" --demo --data "$LASER_DATA" --socket "$LASER_SOCKET" > "$d/laserd.log" 2>&1 &
dpid=$!
for i in $(seq 1 50); do [ -S "$LASER_SOCKET" ] && break; sleep 0.1; done
"$app/Contents/MacOS/LaserEmail" > "$d/app.log" 2>&1 &
apid=$!

shot() { # name
  local f="$d/$1.png"
  screencapture -x "$f" || { echo "screencapture failed"; return; }
  sips -s format jpeg -s formatOptions 70 -Z 1400 "$f" --out "$d/$1.jpg" >/dev/null
  echo "=== SCREENSHOT $1 ($(wc -c < "$d/$1.jpg") bytes)"
  base64 -i "$d/$1.jpg" | fold -w 16000
  echo "=== END SCREENSHOT $1"
}

wait_for() { # text, seconds
  for i in $(seq 1 $(($2 * 10))); do grep -q "$1" "$d/app.log" && return 0; sleep 0.1; done
  echo "timed out waiting for: $1"; cat "$d/app.log"; return 1
}

ok=0
wait_for "SELFTEST READY main" 60 && shot main || ok=1
wait_for "SELFTEST READY attachment" 20 && shot attachment || ok=1
wait_for "SELFTEST DONE" 20 || ok=1
kill $apid $dpid 2>/dev/null
echo "--- self-test report"
cat "$LASER_SELFTEST_REPORT" 2>/dev/null || { echo "no report"; ok=1; }
grep -q '^FAIL' "$LASER_SELFTEST_REPORT" 2>/dev/null && ok=1
echo "--- laserd log"; tail -20 "$d/laserd.log"
exit $ok
