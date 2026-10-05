#!/usr/bin/env python3
"""Find the shell's primary button (Allow, Send, Confirm, Insert) on a
screenshot: the largest blob of the theme's accent color in the given
region, printed as "X Y" (its center). Lab demos only (clicks with
ydotool where a person would click).

    findbtn.py SHOT.png [--accent b5502f] [--region x0,y0,x1,y1] [--skip-top 60]
"""
import argparse
import sys

from PyQt6.QtGui import QImage

ap = argparse.ArgumentParser()
ap.add_argument("shot")
ap.add_argument("--accent", default="b5502f")
ap.add_argument("--region", default="")
ap.add_argument("--skip-top", type=int, default=60, help="ignore the panel")
ap.add_argument("--tol", type=int, default=28)
a = ap.parse_args()
img = QImage(a.shot)
if img.isNull():
    sys.exit("cannot read " + a.shot)
w, h = img.width(), img.height()
x0, y0, x1, y1 = 0, a.skip_top, w, h
if a.region:
    x0, y0, x1, y1 = map(int, a.region.split(","))
ar, ag, ab = int(a.accent[0:2], 16), int(a.accent[2:4], 16), int(a.accent[4:6], 16)
step = 3
hits = set()
for y in range(y0, y1, step):
    for x in range(x0, x1, step):
        c = img.pixelColor(x, y)
        if abs(c.red() - ar) < a.tol and abs(c.green() - ag) < a.tol and abs(c.blue() - ab) < a.tol:
            hits.add((x, y))
# Connected blobs on the sampling grid.
best = None
seen = set()
for p in hits:
    if p in seen:
        continue
    stack, blob = [p], []
    seen.add(p)
    while stack:
        q = stack.pop()
        blob.append(q)
        for dx, dy in ((step, 0), (-step, 0), (0, step), (0, -step)):
            n = (q[0] + dx, q[1] + dy)
            if n in hits and n not in seen:
                seen.add(n)
                stack.append(n)
    xs, ys = [b[0] for b in blob], [b[1] for b in blob]
    bw, bh = max(xs) - min(xs), max(ys) - min(ys)
    # A button: wider than tall, not the logo (round) nor a thin line.
    if len(blob) < 40 or bh < 12 or bw < bh * 1.5 or bh > 80:
        continue
    if best is None or len(blob) > len(best):
        best = blob
if best is None:
    sys.exit("no button found")
xs, ys = [b[0] for b in best], [b[1] for b in best]
print((min(xs) + max(xs)) // 2, (min(ys) + max(ys)) // 2)
