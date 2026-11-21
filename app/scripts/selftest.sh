#!/bin/bash
# Runs the bundled app against `laserd --demo`, takes screenshots of the real
# screen when the app asks, and prints them (JPEG, base64) into the log
# between "=== SCREENSHOT <name>" and "=== END SCREENSHOT <name>" lines
# (decode with app/scripts/decode-screenshots.py), then checks the report.
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
  sips -s format jpeg -s formatOptions 72 -Z 1600 "$f" --out "$d/$1.jpg" >/dev/null
  echo "=== SCREENSHOT $1 ($(wc -c < "$d/$1.jpg") bytes)"
  base64 -i "$d/$1.jpg" | fold -w 16000
  echo "=== END SCREENSHOT $1"
  touch "$LASER_SELFTEST_REPORT.$1.done"
}

ok=0
deadline=$((SECONDS + 120))
done_shots=""
while [ $SECONDS -lt $deadline ]; do
  for name in $(grep -o 'SELFTEST READY [a-z]*' "$d/app.log" 2>/dev/null | awk '{print $3}'); do
    case " $done_shots " in *" $name "*) ;; *) shot "$name"; done_shots="$done_shots $name";; esac
  done
  grep -q "SELFTEST DONE" "$d/app.log" && break
  kill -0 $apid 2>/dev/null || { echo "the app exited"; break; }
  sleep 0.2
done
grep -q "SELFTEST DONE" "$d/app.log" || { echo "self-test did not finish"; cat "$d/app.log"; ok=1; }
{ kill $apid $dpid; wait; } 2>/dev/null
echo "--- self-test report"
cat "$LASER_SELFTEST_REPORT" 2>/dev/null || { echo "no report"; ok=1; }
grep -q '^FAIL' "$LASER_SELFTEST_REPORT" 2>/dev/null && ok=1
echo "--- laserd log"; tail -20 "$d/laserd.log"
exit $ok
