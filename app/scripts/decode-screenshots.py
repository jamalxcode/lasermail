#!/usr/bin/env python3
"""Pulls the self-test screenshots out of a CI job log.

    gh api repos/<owner>/<repo>/actions/jobs/<job-id>/logs > job.txt
    python3 app/scripts/decode-screenshots.py job.txt out-dir
"""
import base64, os, re, sys

log = open(sys.argv[1], encoding="utf-8", errors="replace").read().split("\n")
out = sys.argv[2] if len(sys.argv) > 2 else "."
os.makedirs(out, exist_ok=True)
name, chunks = None, []
for line in log:
    text = re.sub(r"^\S+Z ", "", line).strip()
    m = re.match(r"=== (END )?SCREENSHOT (\S+)", text)
    if m and not m.group(1):
        name, chunks = m.group(2), []
    elif m and name:
        path = os.path.join(out, name + ".jpg")
        open(path, "wb").write(base64.b64decode("".join(chunks)))
        print(path)
        name = None
    elif name and re.fullmatch(r"[A-Za-z0-9+/=]+", text):
        chunks.append(text)
