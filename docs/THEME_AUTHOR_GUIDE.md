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
