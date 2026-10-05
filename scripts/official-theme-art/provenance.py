"""Emit docs/THEME_ASSETS.md — provenance & licence ledger for the
official theme assets. Reads hashes from out/report.json (produced by build.py).
"""
from __future__ import annotations

import datetime
import json
import os

HERE = os.path.dirname(__file__)
DOCS = os.path.join(HERE, "..", "..", "docs")

# scene function name + seed per theme (see scenes.py).
SCENES = {
    "official-rose-dawn": ("scene_rose_dawn", 20260117),
    "official-fortune-forge": ("scene_fortune_forge", 20260118),
    "official-crimson-horizon": ("scene_crimson_horizon", 20260119),
    "official-sage-breeze": ("scene_sage_breeze", 20260120),
    "official-spark-notebook": ("scene_spark_notebook", 20260121),
    "official-violet-starlight": ("scene_violet_starlight", 20260122),
    "official-cyan-stage": ("scene_cyan_stage", 20260123),
    "official-noir-gold": ("scene_noir_gold", 20260124),
}

NAMES = {
    "official-rose-dawn": "Rose Dawn / 玫瑰晨光",
    "official-fortune-forge": "Fortune Forge / 鸿运工坊",
    "official-crimson-horizon": "Crimson Horizon / 赤曜新城",
    "official-sage-breeze": "Sage Breeze / 鼠尾草清风",
    "official-spark-notebook": "Spark Notebook / 灵感手账",
    "official-violet-starlight": "Violet Starlight / 紫曜星夜",
    "official-cyan-stage": "Cyan Stage / 青岚舞台",
    "official-noir-gold": "Noir Gold / 黑金序曲",
}


def main():
    report = json.load(open(os.path.join(HERE, "out", "report.json")))
    today = datetime.date.today().isoformat()

    rows = []
    for tid in sorted(SCENES):
        fn, seed = SCENES[tid]
        r = report[tid]
        prompt = f"scripts/official-theme-art/scenes.py::{fn} (seed {seed}, 2560×1440 canvas, low-info left 0–52%, key content x 62–88% / y 16–72%)"
        rows.append((tid, NAMES[tid], prompt, r["background_sha256"], r["preview_sha256"]))

    en = []
    en.append("# Official Theme Asset Provenance\n")
    en.append("All eight official Reasonix themes ship with **original** artwork generated procedurally")
    en.append("from scratch with the scripts in `scripts/official-theme-art/` (numpy + Pillow, fixed seeds,")
    en.append("fully reproducible). The visual *direction* was inspired by the MIT-licensed")
    en.append("[Codex-Dream-Skin](https://github.com/Fei-Away/Codex-Dream-Skin) concept gallery, but:\n")
    en.append("- **No pixels, layouts, UI mockery, text, logos or watermarks were copied** from the reference")
    en.append("  project or any third party. Every background is re-authored code output.")
    en.append("- All depicted people are **original fictional adults** drawn by the generator: an illustrated")
    en.append("  muse (Rose Dawn), a lucky programmer mascot (Fortune Forge), a reader (Sage Breeze), an anime")
    en.append("  adult (Spark Notebook), a silhouette muse (Violet Starlight), a digital performer (Cyan Stage)")
    en.append("  and a gentleman (Noir Gold). Crimson Horizon contains no people.")
    en.append("- Backgrounds contain no windows, sidebars, cards, buttons, inputs or readable text, and are")
    en.append("  stripped of EXIF/author metadata.\n")
    en.append("Assets are released under the MIT License as part of the Reasonix repository,")
    en.append("© Reasonix Contributors. Human review: Reasonix Contributors (release PR review).\n")
    en.append(f"Generation date: {today}\n")
    en.append("| Theme | Generator (final prompt equivalent) | background.webp SHA-256 | preview.webp SHA-256 |")
    en.append("| --- | --- | --- | --- |")
    for tid, name, prompt, bg, pv in rows:
        en.append(f"| {name} (`{tid}`) | `{prompt}` | `{bg}` | `{pv}` |")
    en.append("")

    with open(os.path.join(DOCS, "THEME_ASSETS.md"), "w") as f:
        f.write("\n".join(en))
    print("wrote docs/THEME_ASSETS.md")


if __name__ == "__main__":
    main()
