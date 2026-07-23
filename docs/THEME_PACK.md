# Reasonix Theme Pack V2

Native theme packs for the Reasonix desktop app. Packs are controlled skins:
semantic color tokens, density/corner recipes, and optional local images for
the home and task/workspace scenes. They **cannot** run CSS, JavaScript, fonts,
remote URLs, or SVG scripts. V1 packs remain valid and use the home image in
both scenes.

> Chinese: [THEME_PACK.zh-CN.md](./THEME_PACK.zh-CN.md)

## Goals (first release)

- Built-in styles, user themes, backgrounds, live preview, import/export, local library
- Full background on the home (empty) scene; reduced opacity + directional overlay on task scenes
- Works with Classic / Workbench / Creation and `auto` / `light` / `dark`
- **No** online marketplace, cloud sync, or script plugins

## Theme experience (settings IA)

Appearance is split into two surfaces (no third entry):

1. **Appearance overview** — current theme summary, light/dark mode, **one** base-style
   control, fonts and zoom. Primary action: **Browse themes**.
2. **Theme gallery** — official / my themes / base styles tabs, select-to-inspect cards,
   detail panel with isolated preview, temporary full-app preview, and a single
   **Apply theme** action. Immersive preview is part of the gallery detail flow.

State model (schema v2 of `desktop-theme-state.json`):

| State | Meaning | Persistence |
| --- | --- | --- |
| `themeMode` | auto / light / dark | desktop config |
| `baseStyle` | Graphite…Amber | desktop config (`theme_style`) |
| `activeThemeId` | official or user pack only | `desktop-theme-state.json` |
| `selectedThemeId` / `previewThemeId` | gallery selection / temp preview | frontend memory only |

- `activeThemeId` **must not** store base style ids. Choosing a base style clears the pack.
- Applying a pack keeps `baseStyle` as the disable/fallback value.
- Light/dark mode is independent of the pack.

## Theme kinds

The gallery has three groups:

| Kind | Source | Editable | Deletable | Exportable |
| --- | --- | --- | --- | --- |
| **Base styles** | Six visual directions (Graphite, Aurora, Slate, Carbon, Nocturne, Amber), token-less | no (duplicate first) | no | no |
| **Official themes** | Eight read-only packs embedded in the installer (manifest + original background + thumbnail, MIT) | no (duplicate first) | no | no |
| **User themes** | Created in the editor, duplicated, or imported as `.reasonix-theme` | yes | yes | yes |

- All 14 built-in ids (6 base + 8 official) are **reserved**: save, import, copy-over
  and delete all refuse collisions.
- Activating an official theme stores only its id in `desktop-theme-state.json` —
  assets are read from the embedded copy at runtime.
- "Duplicate" on a base/official theme creates an ordinary editable user theme
  (the official background is copied into the user library); the duplicate can
  then be edited or exported.
- v1 states that stored a base id as `activeThemeId` are migrated to `desktop.theme_style`
  and cleared on load.

### The eight official themes

| ID | Name | Base style | Artwork |
| --- | --- | --- | --- |
| `official-rose-dawn` | Rose Dawn / 玫瑰晨光 | graphite | Ivory dawn, soft roses, original illustrated muse |
| `official-fortune-forge` | Fortune Forge / 鸿运工坊 | amber | Vermilion/gold/jade workshop, original lucky programmer |
| `official-crimson-horizon` | Crimson Horizon / 赤曜新城 | graphite | Coral-red future city skyline, no people |
| `official-sage-breeze` | Sage Breeze / 鼠尾草清风 | slate | Cream paper, sage sprigs, original reader |
| `official-spark-notebook` | Spark Notebook / 灵感手账 | aurora | Notebook grid with stationery, original anime adult |
| `official-violet-starlight` | Violet Starlight / 紫曜星夜 | nocturne | Blue-violet starfield, butterflies, silhouette muse |
| `official-cyan-stage` | Cyan Stage / 青岚舞台 | carbon | Cyan stage, light rings, original digital performer |
| `official-noir-gold` | Noir Gold / 黑金序曲 | carbon | Black velvet, gold spotlights, original gentleman |

Previews are shown inside the app's theme library (Settings → Appearance) from
real Reasonix builds. **Screenshots of the app must not be imported as theme
backgrounds.** Asset provenance, hashes and licence ledger:
[THEME_ASSETS.md](./THEME_ASSETS.md) · generator scripts in
`scripts/official-theme-art/` (procedural, fixed seeds, reproducible).

## Package format

Distribute as a `.reasonix-theme` ZIP. The archive root may contain **only**:

| File | Required | Notes |
| --- | --- | --- |
| `theme.json` | yes | Manifest (≤ 1 MiB) |
| `background.png` / `.jpg` / `.jpeg` / `.webp` | no | Home image ≤ 16 MiB, ≤ 8192×8192 |
| `background-task.png` / `.jpg` / `.jpeg` / `.webp` | no | Independent task/workspace image ≤ 16 MiB, ≤ 8192×8192 (V2) |

ZIP limits: package ≤ 36 MiB; no nested directories, no symlinks, no duplicate entries, no path traversal.

### `theme.json` example

```json
{
  "schemaVersion": 2,
  "id": "my-theme",
  "name": "My Theme",
  "author": "",
  "description": "",
  "license": "",
  "baseStyle": "graphite",
  "tokens": {
    "light": {
      "bg": "#f4f3ef",
      "fg": "#111827",
      "accent": "#2f5fa8"
    },
    "dark": {
      "bg": "#0c0d10",
      "fg": "#f1f1ef",
      "accent": "#ff6a3d"
    }
  },
  "recipes": {
    "density": "comfortable",
    "corners": "soft"
  },
  "background": {
    "image": "background.webp",
    "focusX": 0.72,
    "focusY": 0.45,
    "safeArea": "left",
    "homeOpacity": 1,
    "taskOpacity": 0.28,
    "overlayStrength": 0.62
  },
  "taskBackground": {
    "image": "background-task.webp",
    "focusX": 0.5,
    "focusY": 0.5,
    "safeArea": "right",
    "opacity": 0.28,
    "overlayStrength": 0.62
  }
}
```

JSON Schema: [theme-pack.schema.json](./theme-pack.schema.json)

### Fields

| Field | Rules |
| --- | --- |
| `schemaVersion` | `1` or `2`; `taskBackground` requires `2` |
| `id` | Lowercase `[a-z][a-z0-9-]*`, reserved: `graphite`, `aurora`, `slate`, `carbon`, `nocturne`, `amber` |
| `baseStyle` | One of the six built-in directions; uncovered tokens inherit it |
| `tokens.light` / `tokens.dark` | Optional maps of semantic keys → `#RRGGBB` or `#RRGGBBAA` only |
| `recipes.density` | `compact` \| `comfortable` |
| `recipes.corners` | `square` \| `soft` \| `round` |
| `background.image` | Bare file name only (png/jpeg/webp) |
| `background.focusX/Y` | 0–1 focal point |
| `background.safeArea` | `left` \| `right` \| `center` (task overlay direction) |
| `background.homeOpacity` | 0–1 |
| `background.taskOpacity` | 0–1 |
| `background.overlayStrength` | 0–1 |
| `background.paneOpacity` | 0–1 (home scene panel opacity) |
| `taskBackground.image` | Optional independent task/workspace image; bare local file name only |
| `taskBackground.focusX/Y` | 0–1 focal point |
| `taskBackground.safeArea` | `left` \| `right` \| `center` |
| `taskBackground.opacity` | 0–1 |
| `taskBackground.overlayStrength` | 0–1 |
| `taskBackground.paneOpacity` | 0–1 (task scene panel opacity) |

### Allowed token keys

`bg`, `bgSoft`, `bgElev`, `panel`, `sidebar`, `chat`, `workspace`, `workspaceFiles`,
`border`, `borderSoft`, `fg`, `fgDim`, `fgFaint`, `accent`, `accentFg`, `ok`, `warn`, `err`

Colors must **not** include `url()`, gradients, or arbitrary CSS.

## Engine behavior

1. Apply global `auto` / `light` / `dark` and the base visual style.
2. Apply the pack overlay (CSS custom properties) **after** stylesheets so it wins over trailing `:root` and Creation locals.
3. Root gets `data-theme-pack="<id>"`; the app container gets `data-theme-scene="home|task"`.
4. Scene is derived only from whether the current session has content — it does not change chat lifecycle.
5. Background is a fixed, non-interactive layer. Task scene dims the image and paints a directional wash (**no** `backdrop-filter`).

## Storage

| Path under Reasonix home | Purpose |
| --- | --- |
| `desktop-theme-state.json` | Versioned active theme pointer (not `config.toml`) |
| `themes/<id>/` | User theme library (`theme.json` + up to two optional scene images) |

Legacy installs without theme state keep the previous appearance. Old app versions ignore the new directory. CLI theme, prompts, provider requests, and cache keys are unchanged.

## Desktop bridge (frontend)

List / activate / reset / save / delete / copy / import / export / pick background.
The UI only receives temporary asset URLs (`/__reasonix_theme_asset/...`) or data URLs — never absolute host paths.

Import: same id is rejected until the user confirms atomic replace. Built-ins cannot be overwritten or deleted. Corrupt / missing packs fall back to the Graphite path. Safe mode does not load external themes. `/theme reset` and the command palette restore entry clear the pack.

## Authoring tips

1. Start from a built-in direction and override only the tokens you need.
2. Prefer WCAG AA contrast (≈ 4.5:1 body text). The editor warns but does not block save.
3. Before sharing a pack with a photo or portrait, confirm redistribution rights.
4. Do not ship third-party or copyrighted reference assets from other products.

## Template

A minimal, royalty-free starter (no portrait photos):

```json
{
  "schemaVersion": 1,
  "id": "paper-dawn",
  "name": "Paper Dawn",
  "author": "Reasonix",
  "description": "Template theme — solid tokens only, no background image.",
  "license": "CC0-1.0",
  "baseStyle": "graphite",
  "tokens": {
    "light": {
      "bg": "#f7f4ef",
      "panel": "#ffffff",
      "sidebar": "#f3efe8",
      "chat": "#fbfaf7",
      "fg": "#1c1917",
      "fgDim": "#57534e",
      "fgFaint": "#a8a29e",
      "border": "#e7e5e4",
      "accent": "#c2410c",
      "accentFg": "#fff7ed",
      "ok": "#15803d",
      "warn": "#b45309",
      "err": "#b91c1c"
    },
    "dark": {
      "bg": "#0c0b0a",
      "panel": "#171412",
      "sidebar": "#141210",
      "chat": "#0c0b0a",
      "fg": "#f5f5f4",
      "fgDim": "#a8a29e",
      "fgFaint": "#78716c",
      "border": "#292524",
      "accent": "#fb923c",
      "accentFg": "#0c0b0a",
      "ok": "#4ade80",
      "warn": "#fbbf24",
      "err": "#f87171"
    }
  },
  "recipes": {
    "density": "comfortable",
    "corners": "soft"
  }
}
```

Zip as `paper-dawn.reasonix-theme` with only `theme.json` at the root.
