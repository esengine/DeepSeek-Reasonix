---
status: active
owner: @esengine
backup: @SivanCola
reviewed: 2026-10-01
---

# Studio theme author guide

Studio reads a directory containing `theme.json` with `schemaVersion: 1`.
The [Paper Dawn package](themes/reasonix-plugin.json) is a local, token-only
starter with light and dark palettes. Its palette is released as CC0-1.0.
It contributes one theme and no skills, hooks, commands, MCP servers or runtime.

## Install and try the starter

From this repository's root, with the Studio `reasonix` CLI on your PATH:

```bash
reasonix plugin install ./docs/themes --dry-run
reasonix plugin install ./docs/themes --yes
reasonix plugin doctor paper-dawn-kit
reasonix plugin show paper-dawn-kit
```

Review the preview before installing: it should contain one plugin action with
`themeCount: 1` and no execution capabilities. Installation copies the package;
moving the source afterwards does not break the installed theme.

Open **Settings → Appearance** in Studio and choose **Paper Dawn** from the theme
choices. Installing a theme does not select it. Try both light and dark modes,
read ordinary and muted text, and check a dialog and a code block.

The package's theme identity is `plugin:paper-dawn-kit:paper-dawn`: the plugin
name and the contributed theme directory name determine it. The `id` inside
`theme.json` does not override that plugin identity.

## Make your own package

Copy `docs/themes` into a local working folder; keep this layout:

```text
my-palette/
  reasonix-plugin.json
  paper-dawn/
    theme.json
```

Change the plugin `name`, `version` and `description` in
`reasonix-plugin.json`, then edit the theme's `name`, `author`, `description`
and palette. If you rename `paper-dawn`, update `contributes.themes` to match.
The manifest uses an explicit relative path; all contributions stay inside
the package root.

For a live local development copy:

```bash
cd /absolute/path/to/my-palette
reasonix plugin install . --link --yes
```

A link source must be inside the current workspace or your home directory.
A linked package reads your working folder. Keep it in place, and keep the
theme directory name stable.

After edits, reopen Studio's Appearance settings
and reselect the theme to reread its tokens. For a copied installation, preview
and apply the replacement instead:

```bash
reasonix plugin install /absolute/path/to/my-palette --replace --dry-run
reasonix plugin install /absolute/path/to/my-palette --replace --yes
```

Use your package's manifest name in management commands. Share an immutable
repository commit using the [community author guide](MARKET_AUTHOR_GUIDE.md)
after checking the package and its licence; local installation does not publish it.

## Current token vocabulary

Each scheme is a map under `tokens.light` or `tokens.dark`. Both schemes must
contain at least one valid token, or the pack will not load. Omitted tokens
retain Studio defaults or derive from another declared surface.

The authoritative vocabulary is the kernel's [`Tokens` map](../internal/ext/theme/tokens.go), with value validation in `validToken`, `isColour`, `isLength` and `isFontStack` in that same file. Use those definitions when choosing token names and values; this guide does not maintain a second token table.

The frontend maps the vocabulary to CSS variables in [`theme.ts`](../desktop/frontend-next/src/ui/theme.ts). The kernel's [`TestThemeTokenVocabularyMatchesTheFrontend`](../internal/ext/theme/tokens_test.go) checks that both sides agree.

Code is coloured by nine optional tokens, in the file editor and in rendered
code blocks alike. A pack that omits one keeps what each surface draws today.

| Token | Colours | When omitted |
| --- | --- | --- |
| `synKeyword`, `synString`, `synNumber`, `synFunction` | keywords, strings, numbers, function names | Studio's own colour |
| `synComment` | comments | the faint ink |
| `synType` | type and class names | the `synFunction` colour |
| `synOperator`, `synPunctuation` | operators, punctuation | the quiet ink in blocks, the text ink in the editor |
| `synVariable` | variables | the surrounding text |

A more specific code token, such as a function name, still takes its own colour.
Hold every code colour you set to AA contrast on `codeBg`; the kernel's tests
check it for the shipped packs.

`accent` paints filled controls; `accentFg` is text on that fill. Optional
`accentInk` paints accent-coloured text on ordinary surfaces and tinted washes.

When absent, Studio mixes 65% `accent` with 35% `fg` (or the current text colour).
Choose these as separate contrast pairs. Older readers ignore `accentInk` and
continue loading the other schema-version-1 tokens.

The built-in **清白蓝 / Clear Blue** (`official-clear-blue`), **茶绿 / Tea Green**
(`official-tea-green`) and **Patina** (`official-patina`) are plain light/dark
palettes. They use no wallpaper, sky, font or radius overrides. They are opt-in;
an empty `desktop.theme_pack` still selects the unchanged default.

Foundation palettes share the default reading
surface and respect the reader's size, weight and contrast preferences.

Status colours such as `ok`, `warn` and `err` belong to Studio and cannot be
recoloured by a pack. `sidebar` and `chat` are not current token names.
An unknown token or invalid value is dropped, while valid tokens still load;
the theme choice shows the resulting warnings.

User contrast settings can adjust a theme's text colours. Check readability
with the contrast settings and both colour schemes instead of assuming the
JSON colour is always the final text colour.

The retired desktop's `baseStyle`, `recipes`, `taskBackground` and
`schemaVersion: 2` are not the Studio authoring contract.
[Theme Pack V2](THEME_PACK.md) remains the legacy reference.

## Optional local images

Keep image files next to `theme.json`. Studio recognises an optional
`background.png`, `.jpg`, `.jpeg` or `.webp`, and likewise a `preview` image.

The reader selects those conventional filenames; a `background.image` field
alone does not locate an arbitrary file.
Keep each image at most 8 MiB so the asset endpoint can serve it.

The current `background` block controls `focusX`, `focusY`, `safeArea`
(`left`, `center`, `right`), `homeOpacity`, `taskOpacity` and
`overlayStrength`. Numeric placement and opacity values are bounded to 0–1.

Use a low task opacity so the image does not compete with a transcript.
Check your image licence and avoid including workspace screenshots or secrets.

### Backdrop surface opacity

`background.surfaceOpacity` optionally sets each shell surface's background
alpha over a backdrop, independently for light and dark:

```json
"background": {
  "surfaceOpacity": {
    "dark": { "nav": 0.45, "rail": 0.45, "chrome": 0.80, "side": 0.80, "compose": 0.88 },
    "light": { "nav": 0.60, "rail": 0.60, "chrome": 0.86, "side": 0.86, "compose": 0.88 }
  }
}
```

| Surface key | Selector | Colour mixed with transparency |
| --- | --- | --- |
| `nav` | `.nav` (icon navigation) | `--page` |
| `rail` | `.rail` (sessions) | `--page` |
| `chrome` | `.chrome` (top bar) | `--page` |
| `side` | `.side` (legacy details) | `--page` |
| `compose` | `.compose` (composer card) | `--raised` |

This takes effect only with `data-bg`, from a sky or background image.
Blur, transcript backing and text shadows do not change.
There is no scalar shorthand or new appearance control.

Omitted values retain the current stylesheet paint; older packs look unchanged.

| Surface | Current Studio default | Base layout fallback |
| --- | --- | --- |
| Navigation | 0.62 | 0.62 |
| Sessions/top bar | 0.80 dark; 0.86 light | 0.62 |
| Composer | 0.88 | 0.80 |
| Legacy details | Opaque light overlay/dark surface | 0.62 |

The current dock does not mount the legacy `.side` selector.

- Numbers are clamped to 0–1; zero means fully transparent.
- Unknown schemes/surfaces are dropped with theme warnings, also in plugin diagnostics.
- Null means omitted. Wrong types fail manifest decoding.
- Switching scheme/pack, clearing the pack, or removing the backdrop clears old values.

Check ordinary and muted text over your image in both schemes and at phone width.


### Transcript text glow

Set `background.textGlow` to `"none"` to disable the transcript's wallpaper
text shadow. Omit it, or use `"default"`, to retain the two existing shadows
(1px offset with 2px blur, plus a 7px halo). For example:

```json
"background": { "textGlow": "none", "taskOpacity": 0.2 }
```

This choice applies in both schemes whenever a backdrop is active, including
a sky or the reader's own wallpaper. It also works without a packaged image.

The composer, navigation shadows and frosted transcript backing retain their
own treatments. Reduced effects still overrides text shadows;
reduced motion changes animation only.

Switching or clearing a pack
removes its glow choice, so older packs retain the default.

Only `"default"` and `"none"` are accepted. An unknown string is dropped with
a theme warning. Null behaves as omitted; booleans, numbers, arrays and objects
fail manifest decoding. CSS values are not accepted.

Check ordinary and muted text over bright and dark images in
both schemes and at phone width before sharing a pack.

## Disable and clean up

```bash
reasonix plugin disable paper-dawn-kit
reasonix plugin enable paper-dawn-kit
reasonix plugin remove paper-dawn-kit --yes
```

Restart Studio after external CLI changes, then reopen Appearance settings.
Disabling hides the contributed
theme and retains the copied files; re-enabling restores the same identity.
Removing a copied package deletes its installed copy. Removing a linked
package retains the source working folder.

Studio retains the selected theme identity when its plugin becomes unavailable
and renders its default appearance until the theme is available again. Choose
the default theme in Appearance settings to clear that selection explicitly.
Themes do not enter model prompts or add tools to a conversation.
