# Official Theme Asset Provenance

All eight official Reasonix themes ship with **original** artwork generated procedurally
from scratch with the scripts in `scripts/official-theme-art/` (numpy + Pillow, fixed seeds,
fully reproducible). The visual *direction* was inspired by the MIT-licensed
[Codex-Dream-Skin](https://github.com/Fei-Away/Codex-Dream-Skin) concept gallery, but:

- **No pixels, layouts, UI mockery, text, logos or watermarks were copied** from the reference
  project or any third party. Every background is re-authored code output.
- All depicted people are **original fictional adults** drawn by the generator: an illustrated
  muse (Rose Dawn), a lucky programmer mascot (Fortune Forge), a reader (Sage Breeze), an anime
  adult (Spark Notebook), a silhouette muse (Violet Starlight), a digital performer (Cyan Stage)
  and a gentleman (Noir Gold). Crimson Horizon contains no people.
- Backgrounds contain no windows, sidebars, cards, buttons, inputs or readable text, and are
  stripped of EXIF/author metadata.

Assets are released under the MIT License as part of the Reasonix repository,
© Reasonix Contributors. Human review: Reasonix Contributors (release PR review).

Generation date: 2026-07-17

| Theme | Generator (final prompt equivalent) | background.webp SHA-256 | preview.webp SHA-256 |
| --- | --- | --- | --- |
| Crimson Horizon / 赤曜新城 (`official-crimson-horizon`) | `scripts/official-theme-art/scenes.py::scene_crimson_horizon (seed 20260119, 2560×1440 canvas, low-info left 0–52%, key content x 62–88% / y 16–72%)` | `34e9051e6b47c64a3d3324832325f06811aaf36ca3a7f3e55480cb978343f1f8` | `ea26252f557aefafdad5147e509e73c4b2c284c50d4d38bee33edcf64499c0dd` |
| Cyan Stage / 青岚舞台 (`official-cyan-stage`) | `scripts/official-theme-art/scenes.py::scene_cyan_stage (seed 20260123, 2560×1440 canvas, low-info left 0–52%, key content x 62–88% / y 16–72%)` | `e03e4474e9f0cef3e9f1f48c821962e1adf65cde16ac1659ea7cfe78f524cd5e` | `1fafc6e116f57f68ad8ff040ea6979f0d31769860090779b7a4bd6e04ff2b8ca` |
| Fortune Forge / 鸿运工坊 (`official-fortune-forge`) | `scripts/official-theme-art/scenes.py::scene_fortune_forge (seed 20260118, 2560×1440 canvas, low-info left 0–52%, key content x 62–88% / y 16–72%)` | `f8b6f1d2eaea4a2d74990c0f4fe2958016f273a579e0637ceea794789b35780e` | `04c26ed966ff1fda5acd30a802a51484c140fc9c568acdacf98e5b59310a1255` |
| Noir Gold / 黑金序曲 (`official-noir-gold`) | `scripts/official-theme-art/scenes.py::scene_noir_gold (seed 20260124, 2560×1440 canvas, low-info left 0–52%, key content x 62–88% / y 16–72%)` | `05684900a437a462a6fdc267ff8bc28abe33e311dd5d3d7e99e1e456a3ccf914` | `10df2fe7a3d0f4c6681be4b9421137a9a83894423ae5508c056dd4f996283197` |
| Rose Dawn / 玫瑰晨光 (`official-rose-dawn`) | `scripts/official-theme-art/scenes.py::scene_rose_dawn (seed 20260117, 2560×1440 canvas, low-info left 0–52%, key content x 62–88% / y 16–72%)` | `80e92ffa4c94e7f976f3678116bf59beb3db6f2d322b7b5e5dfbc1d447cc99bc` | `0b7e2096ea67ee2a37d0b50e04ac459b78200aca35906e5051b12382fba37f1e` |
| Sage Breeze / 鼠尾草清风 (`official-sage-breeze`) | `scripts/official-theme-art/scenes.py::scene_sage_breeze (seed 20260120, 2560×1440 canvas, low-info left 0–52%, key content x 62–88% / y 16–72%)` | `a27c4bf63edcb5b5aed2790b2f24d3d72aceedd21c78943c314ad1b11fc05344` | `55fed2b26a7f16b4ec0e144e2f89cf07d17ce4bd53b558c84a69b712044f8aa6` |
| Spark Notebook / 灵感手账 (`official-spark-notebook`) | `scripts/official-theme-art/scenes.py::scene_spark_notebook (seed 20260121, 2560×1440 canvas, low-info left 0–52%, key content x 62–88% / y 16–72%)` | `adc70d90e9fdf6670a2fa6b7e46a878694ed9097995bae2a6d3e59bd21810092` | `f114dc160f3be0d8ff943b0dd9bbbcb93e6fc8c306fdbd5094daecc3f7e32a26` |
| Violet Starlight / 紫曜星夜 (`official-violet-starlight`) | `scripts/official-theme-art/scenes.py::scene_violet_starlight (seed 20260122, 2560×1440 canvas, low-info left 0–52%, key content x 62–88% / y 16–72%)` | `22999e9796f77ad10bd43e21503bf07efa1274eddd4cc0449485fde3e8a830bc` | `9dfbe62b922d140dc89b4991154148f3986f00ab463df9ebb05a997e93c4cf3f` |
