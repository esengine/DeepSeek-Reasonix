"""Build official theme backgrounds + thumbnails and report budgets/hashes."""
from __future__ import annotations

import argparse
import json
import os
import sys

from artkit import H, W, make_thumb, save_webp, sha256_file
from scenes import SCENES

OUT = os.path.join(os.path.dirname(__file__), "out")
BG_BUDGET = int(2.25 * 1024 * 1024)
THUMB_BUDGET = 120 * 1024
TOTAL_BUDGET = 18 * 1024 * 1024


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("themes", nargs="*", default=None, help="theme ids to build (default: all)")
    ap.add_argument("--out", default=OUT)
    args = ap.parse_args()

    ids = args.themes or sorted(SCENES)
    report = {}
    total = 0
    for tid in ids:
        if tid not in SCENES:
            print(f"unknown theme {tid}", file=sys.stderr)
            sys.exit(2)
        img = SCENES[tid]()
        assert img.size == (W, H), img.size
        tdir = os.path.join(args.out, tid)
        bg_path = os.path.join(tdir, "background.webp")
        th_path = os.path.join(tdir, "preview.webp")
        bg_size = save_webp(img, bg_path, quality=82, target_bytes=BG_BUDGET)
        th_size = make_thumb(img, th_path, target_bytes=THUMB_BUDGET)
        total += bg_size
        report[tid] = {
            "background_bytes": bg_size,
            "background_ok": bg_size <= BG_BUDGET,
            "background_sha256": sha256_file(bg_path),
            "preview_bytes": th_size,
            "preview_ok": th_size <= THUMB_BUDGET,
            "preview_sha256": sha256_file(th_path),
        }
        print(f"{tid}: bg={bg_size/1024:.0f} KiB ok={report[tid]['background_ok']}  thumb={th_size/1024:.0f} KiB")
    print(f"total backgrounds: {total/1024/1024:.2f} MiB (budget 18 MiB)")
    with open(os.path.join(args.out, "report.json"), "w") as f:
        json.dump(report, f, indent=2)


if __name__ == "__main__":
    main()
