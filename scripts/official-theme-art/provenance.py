"""Emit docs/THEME_ASSETS.md + .zh-CN.md — provenance & licence ledger for the
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

    zh = []
    zh.append("# 官方主题素材来源与许可记录\n")
    zh.append("八款 Reasonix 官方主题的全部图片均为**原创**，由 `scripts/official-theme-art/` 中的脚本")
    zh.append("从零程序化生成（numpy + Pillow，固定随机种子，可完全复现）。视觉*方向*参考了 MIT 许可的")
    zh.append("[Codex-Dream-Skin](https://github.com/Fei-Away/Codex-Dream-Skin) 概念图库，但是：\n")
    zh.append("- **未复制参考项目或任何第三方素材的任何像素**、布局、界面元素、文字、标志或水印；")
    zh.append("  所有背景均为代码重新生成的独立素材。")
    zh.append("- 图中人物均为生成器绘制的**原创虚构成年人**：插画女性（玫瑰晨光）、吉祥程序员（鸿运工坊）、")
    zh.append("  读者（鼠尾草清风）、动漫人物（灵感手账）、剪影女性（紫曜星夜）、数字表演者（青岚舞台）、")
    zh.append("  绅士（黑金序曲）。赤曜新城不含人物。")
    zh.append("- 背景中不含窗口、侧栏、卡片、按钮、输入框或可读文字，并已去除 EXIF 等元数据。\n")
    zh.append("素材随 Reasonix 仓库以 MIT 许可发布，© Reasonix Contributors。人工审核：Reasonix Contributors（发布 PR 审核）。\n")
    zh.append(f"生成日期：{today}\n")
    zh.append("| 主题 | 生成器（最终提示词等价物） | background.webp SHA-256 | preview.webp SHA-256 |")
    zh.append("| --- | --- | --- | --- |")
    for tid, name, prompt, bg, pv in rows:
        zh.append(f"| {name}（`{tid}`） | `{prompt}` | `{bg}` | `{pv}` |")
    zh.append("")

    with open(os.path.join(DOCS, "THEME_ASSETS.md"), "w") as f:
        f.write("\n".join(en))
    with open(os.path.join(DOCS, "THEME_ASSETS.zh-CN.md"), "w") as f:
        f.write("\n".join(zh))
    print("wrote docs/THEME_ASSETS.md + .zh-CN.md")


if __name__ == "__main__":
    main()
