// Run: tsx src/__tests__/theme-pack.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  applyConfiguredBaseAppearance,
  applyThemePack,
  applyThemeScene,
  beginThemePreview,
  cancelThemePreview,
  clearThemePack,
  draftPackView,
  getActiveThemePack,
  getBaseAppearance,
  isSafeBackgroundURL,
  isSafeHex,
  isThemeTokenKey,
  registerTrustedThemeBackgroundURLs,
  setBaseAppearance,
  themePackKind,
} from "../lib/themePack";
import { applyTheme, getThemeStyle } from "../lib/theme";
import { BASE_STYLE_PREVIEW_PALETTES, themePreviewPalette } from "../lib/themePreviewPalette";
import { themePreviewPaneAlpha } from "../components/ThemePreviewSurface";
import {
  activateThemePack,
  applyExperienceToDOM,
  cancelGlobalPreview,
  configuredBaseStyleForSync,
  isPreviewActive,
  startGlobalPreview,
} from "../lib/themeExperience";

const testDir = dirname(fileURLToPath(import.meta.url));
const packSource = readFileSync(resolve(testDir, "../lib/themePack.ts"), "utf8");
const stylesSource = readFileSync(resolve(testDir, "../styles.css"), "utf8");
const appSource = readFileSync(resolve(testDir, "../App.tsx"), "utf8");
const librarySource = readFileSync(resolve(testDir, "../components/ThemeLibrary.tsx"), "utf8");
const gallerySource = readFileSync(resolve(testDir, "../components/ThemeGallery.tsx"), "utf8");
const confirmDialogSource = readFileSync(resolve(testDir, "../components/ConfirmDialog.tsx"), "utf8");
const overviewSource = readFileSync(resolve(testDir, "../components/AppearanceOverview.tsx"), "utf8");
const settingsSource = readFileSync(resolve(testDir, "../components/SettingsPanel.tsx"), "utf8");
const experienceSource = readFileSync(resolve(testDir, "../lib/themeExperience.ts"), "utf8");
const bridgeSource = readFileSync(resolve(testDir, "../lib/bridge.ts"), "utf8");
const viteSource = readFileSync(resolve(testDir, "../../vite.config.ts"), "utf8");
const localeEn = readFileSync(resolve(testDir, "../locales/en.ts"), "utf8");
const localeZh = readFileSync(resolve(testDir, "../locales/zh.ts"), "utf8");
const localeZhTW = readFileSync(resolve(testDir, "../locales/zh-TW.ts"), "utf8");

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

// Minimal DOM for applyThemePack
const attrs = new Map<string, string>();
const styleProps = new Map<string, string>();
const styleEl: {
  id: string;
  textContent: string;
  parentElement: { removeChild: (el: unknown) => void } | null;
  remove: () => void;
} = {
  id: "",
  textContent: "",
  parentElement: null,
  remove() {
    const idx = headChildren.indexOf(styleEl);
    if (idx >= 0) headChildren.splice(idx, 1);
    styleEl.textContent = "";
  },
};
const headChildren: unknown[] = [];

(globalThis as unknown as { document: unknown }).document = {
  documentElement: {
    setAttribute(k: string, v: string) {
      attrs.set(k, v);
    },
    removeAttribute(k: string) {
      attrs.delete(k);
    },
    style: {
      setProperty(k: string, v: string) {
        styleProps.set(k, v);
      },
      removeProperty(k: string) {
        styleProps.delete(k);
      },
    },
  },
  head: {
    appendChild(el: unknown) {
      headChildren.push(el);
      styleEl.parentElement = {
        removeChild(child: unknown) {
          const idx = headChildren.indexOf(child);
          if (idx >= 0) headChildren.splice(idx, 1);
        },
      };
      return el;
    },
  },
  getElementById(id: string) {
    if (id === "reasonix-theme-pack-overlay" && headChildren.includes(styleEl)) return styleEl;
    return null;
  },
  createElement(tag: string) {
    if (tag === "style") return styleEl;
    return {};
  },
  querySelector() {
    return null;
  },
};

(globalThis as unknown as { window: unknown }).window = {
  matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} }),
  location: { href: "http://127.0.0.1:5197/", origin: "http://127.0.0.1:5197" },
  runtime: undefined,
};

console.log("\ntheme pack contract");

ok(isSafeHex("#aabbcc"), "accepts #RRGGBB");
ok(isSafeHex("#aabbccdd"), "accepts #RRGGBBAA");
ok(!isSafeHex("url(x)"), "rejects url()");
ok(!isSafeHex("linear-gradient(red,blue)"), "rejects gradient");
ok(isThemeTokenKey("accent") && !isThemeTokenKey("hack"), "token whitelist");

ok(isSafeBackgroundURL("/__reasonix_theme_asset/my-theme/abc/background.png"), "asset URL allowed");
ok(isSafeBackgroundURL("data:image/png;base64,aaa"), "data URL allowed");
ok(!isSafeBackgroundURL("https://evil.example/bg.png"), "remote URL rejected");
const bundledOfficialBackground = "http://127.0.0.1:5197/@fs/workspace/desktop/themes/official/official-rose-dawn/background.webp";
registerTrustedThemeBackgroundURLs([bundledOfficialBackground, "https://evil.example/assets/background-fake.webp"]);
ok(isSafeBackgroundURL(bundledOfficialBackground), "registered same-origin official dev background allowed");
ok(!isSafeBackgroundURL("https://evil.example/assets/background-fake.webp"), "cross-origin bundled background rejected");

const draft = draftPackView({
  id: "preview-pack",
  name: "Preview",
  baseStyle: "graphite",
  tokens: { dark: { accent: "#ff0000", fg: "#ffffff" }, light: { accent: "#0000ff" } },
  recipes: { density: "compact", corners: "round" },
  background: {
    focusX: 0.2,
    focusY: 0.8,
    safeArea: "left",
    homeOpacity: 1,
    taskOpacity: 0.2,
    overlayStrength: 0.5,
    paneOpacity: 0.50,
  },
  backgroundUrl: "/__reasonix_theme_asset/preview-pack/deadbeef/background.png",
});

const tokenOnlyPreview = draftPackView({
  id: "token-only-preview",
  name: "Token Only Preview",
  baseStyle: "graphite",
  tokens: { dark: { accent: "#ff0000" } },
  recipes: { density: "comfortable", corners: "soft" },
});
ok(themePreviewPaneAlpha(tokenOnlyPreview, "home") === 1, "token-only preview keeps opaque panes");
ok(themePreviewPaneAlpha(draft, "home") === 0.5, "background preview applies configured pane opacity");

applyThemePack(draft);
ok(attrs.get("data-theme-pack") === "preview-pack", "sets data-theme-pack");
ok(attrs.get("data-theme-has-bg") === "true", "marks background present");
ok(styleProps.has("--theme-bg-image"), "sets background image var");
ok((styleEl as { textContent: string }).textContent.includes("--accent:#ff0000"), "injects dark accent override");
ok((styleEl as { textContent: string }).textContent.includes("--r:14px"), "applies round corners recipe");

const twoSceneDraft = draftPackView({
  ...draft,
  taskBackground: { focusX: 0.8, focusY: 0.3, safeArea: "right", opacity: 0.35, overlayStrength: 0.7, paneOpacity: 0.68 },
  taskBackgroundUrl: "/__reasonix_theme_asset/preview-pack/deadbeef/background-task.png",
});
applyThemePack(twoSceneDraft);
ok(styleProps.get("--theme-bg-task-image")?.includes("background-task.png") === true, "sets independent task image var");
ok(styleProps.get("--theme-bg-task-opacity") === "0.35", "sets independent task opacity");
ok(styleProps.get("--theme-pane-card-pct") === "76%", "computes home card pane opacity");
ok(styleProps.get("--theme-pane-task-card-pct") === "82%", "computes task card pane opacity");
ok(attrs.get("data-theme-safe-area") === "right", "task background controls safe area");

// Older shells and partial mocks can expose the independent task scene without
// the newly added paneOpacity field. It must inherit the home pane value rather
// than falling through clamp01(undefined)'s generic midpoint.
const legacyTaskPaneDraft = draftPackView({
  ...twoSceneDraft,
  taskBackground: { ...twoSceneDraft.taskBackground! },
});
delete (legacyTaskPaneDraft.taskBackground as { paneOpacity?: number }).paneOpacity;
applyThemePack(legacyTaskPaneDraft);
ok(styleProps.get("--theme-pane-task-alpha") === "0.5", "legacy task scene inherits home pane opacity");

applyThemeScene("task");
ok(attrs.get("data-theme-scene") === "task", "scene task on root");

applyThemeScene("home");
ok(attrs.get("data-theme-scene") === "home", "scene home on root");

// Preview cancel restores previous (null) pack
clearThemePack();
beginThemePreview(draft);
ok(attrs.get("data-theme-pack") === "preview-pack", "preview applies pack");
cancelThemePreview();
ok(!attrs.has("data-theme-pack"), "cancel restores cleared pack");

// A failed persistent activation must keep the preview snapshot reversible.
clearThemePack();
applyTheme("dark", "graphite", { persist: false });
startGlobalPreview(draft);
const testWindow = window as unknown as {
  go?: { main?: { App?: { ActivateThemePack: (id: string) => Promise<void> } } };
};
testWindow.go = {
  main: {
    App: {
      async ActivateThemePack() {
        throw new Error("activation failed");
      },
    },
  },
};
let activationRejected = false;
try {
  await activateThemePack(draft.id);
} catch {
  activationRejected = true;
}
ok(activationRejected, "activation failure surfaces to caller");
ok(isPreviewActive(), "activation failure keeps preview reversible");
cancelGlobalPreview();
ok(!attrs.has("data-theme-pack") && getThemeStyle() === "graphite", "cancel restores appearance after activation failure");
delete testWindow.go;

// Save-and-apply must commit the preview before editor unmount cleanup can
// restore the old snapshot while the gallery reload is in flight.
const saveEditorStart = gallerySource.indexOf("const saveEditor = async");
const saveEditorEnd = gallerySource.indexOf("if (immersive && selectedPack)", saveEditorStart);
const saveEditorSource = gallerySource.slice(saveEditorStart, saveEditorEnd);
ok(saveEditorSource.includes("activate: false"), "save-and-apply defers persistent activation to the experience controller");
ok(
  (saveEditorSource.match(/activateThemePack\(saved\.id\)/g) || []).length === 1,
  "save-and-apply persists activation exactly once",
);
ok(
  saveEditorSource.indexOf("await activateThemePack(saved.id)") < saveEditorSource.indexOf("setEditor(null)"),
  "save-and-apply activates before editor unmount",
);

// Restore-default must restore config baseStyle, not leave pack baseStyle.
setBaseAppearance("dark", "graphite");
applyTheme("dark", "graphite", { persist: false });
const aurora = draftPackView({
  id: "aurora",
  name: "Aurora",
  baseStyle: "aurora",
  tokens: {},
  recipes: { density: "comfortable", corners: "soft" },
});
applyThemePack(aurora);
ok(attrs.get("data-theme-pack") === "aurora", "aurora pack active");
ok(getThemeStyle() === "aurora", "pack switches live style to aurora");
clearThemePack();
ok(!attrs.has("data-theme-pack"), "clear removes data-theme-pack");
ok(getThemeStyle() === "graphite", "clear restores config graphite style");

// Generic settings refreshes must update the configured restore target without
// replacing an active pack's effective style in the live DOM.
applyThemePack(aurora);
applyConfiguredBaseAppearance("light", "slate");
ok(getActiveThemePack()?.id === "aurora", "settings refresh preserves the active pack");
ok(attrs.get("data-theme-pack") === "aurora", "settings refresh preserves the pack DOM marker");
ok(getThemeStyle() === "aurora", "settings refresh preserves the pack effective style");
ok(
  getBaseAppearance()?.theme === "light" && getBaseAppearance()?.style === "slate",
  "settings refresh updates the configured restore appearance",
);
clearThemePack();
ok(getThemeStyle() === "slate", "clear restores the configured appearance after settings refresh");

// React owners must not replace the configured base style with a pack's
// effective style. Direct reset entry points depend on this restore snapshot.
const activeAuroraExperience = {
  themeMode: "dark" as const,
  baseStyle: "graphite" as const,
  effectiveStyle: "aurora" as const,
  activeThemeId: aurora.id,
  activePack: aurora,
  safeMode: false,
};
applyExperienceToDOM(activeAuroraExperience);
ok(configuredBaseStyleForSync(activeAuroraExperience) === null, "active pack effective style is not mirrored as configured base");
clearThemePack();
ok(getThemeStyle() === "graphite", "direct reset still restores configured base after experience sync");
ok(
  configuredBaseStyleForSync({ ...activeAuroraExperience, activeThemeId: undefined, activePack: null, baseStyle: "slate", effectiveStyle: "slate" }) === "slate",
  "inactive experience still synchronizes a newly selected base style",
);

// Density recipe must land in overlay CSS and have stylesheet consumers.
ok((styleEl as { textContent: string }).textContent.includes("--theme-density-pad") || packSource.includes("--theme-density-pad:6px"), "compact density vars defined in pack builder");
const compactDraft = draftPackView({
  id: "dense",
  name: "Dense",
  baseStyle: "graphite",
  tokens: {},
  recipes: { density: "compact", corners: "soft" },
});
applyThemePack(compactDraft);
ok((styleEl as { textContent: string }).textContent.includes("--theme-density-pad:6px"), "compact density injected");
ok((styleEl as { textContent: string }).textContent.includes("--theme-row-h:28px"), "compact row height injected");
ok(stylesSource.includes("padding: var(--theme-density-pad"), "density pad consumed by cards");
ok(stylesSource.includes("gap: var(--theme-density-gap"), "density gap consumed");
ok(stylesSource.includes("--list-row-height: var(--theme-row-h)"), "density maps to list row height");

// Layout must go transparent when a background is active so theme-bg is visible.
ok(
  /data-theme-has-bg="true"\][^}]*\.layout\s*\{[^}]*background:\s*transparent/s.test(stylesSource),
  "layout background transparent when theme has background",
);
const transparencyStart = stylesSource.indexOf("Extended pane transparency");
const transparencyEnd = stylesSource.indexOf("Density recipe consumers", transparencyStart);
const transparencySlice = stylesSource.slice(transparencyStart, transparencyEnd);
const unguardedTransparencySelectors = transparencySlice
  .split("\n")
  .filter((line) => line.includes(":root[data-theme-pack]") && !line.includes('[data-theme-has-bg="true"]'));
ok(unguardedTransparencySelectors.length === 0, "token-only packs keep opaque layout surfaces");
ok(stylesSource.includes("var(--theme-pane-card-pct, 88%)"), "home cards consume pane opacity tier");
ok(stylesSource.includes("var(--theme-pane-task-card-pct, 88%)"), "task cards consume pane opacity tier");
ok(stylesSource.includes("var(--tp-pane-card-pct, 88%)"), "preview cards consume the same pane opacity tier");
ok(stylesSource.includes(':root[data-theme-has-bg="true"] .theme-bg'), "background layer only displays for packs with backgrounds");

// Unmount must cancel preview.
ok(librarySource.includes("cancelThemePreview()"), "ThemeLibrary cleanup cancels preview");

// Import confirm reuses staged import (replace=true empty path).
ok(
  librarySource.includes("ImportThemePack(\"\", true)") || librarySource.includes("ImportThemePack('', true)"),
  "import confirm reuses staged path without re-picking",
);
ok(librarySource.includes("needsReplace"), "import handles needsReplace result");

// Theme confirmations stay inside the Reasonix UI instead of opening native
// browser/system prompts.
ok(!gallerySource.includes("window.confirm"), "ThemeGallery does not use native confirm dialogs");
ok(!librarySource.includes("window.confirm"), "ThemeLibrary does not use native confirm dialogs");
ok(gallerySource.includes("useConfirmDialog") && librarySource.includes("useConfirmDialog"), "theme flows share the Reasonix confirm dialog");
ok(confirmDialogSource.includes('role="dialog"') && confirmDialogSource.includes('aria-modal="true"'), "confirm dialog exposes accessible modal semantics");
ok(confirmDialogSource.includes('request.tone === "danger"') && confirmDialogSource.includes("btn--danger"), "destructive confirmations use danger styling");
ok(confirmDialogSource.includes('event.key === "Escape"') && confirmDialogSource.includes("restoreFocusRef"), "confirm dialog supports Escape and focus restoration");
ok(gallerySource.includes("moreActionsRef") && gallerySource.includes("moreActionsRef.current?.focus()"), "gallery cancellation restores focus after closing its overflow menu");

// Source contracts
ok(packSource.includes("reasonix-theme-pack-overlay"), "overlay style id stable");
ok(packSource.includes("appendChild(el)"), "overlay style appended last for priority");
ok(packSource.includes("baseAppearance"), "tracks base appearance for restore");
ok(stylesSource.includes(".theme-bg"), "background layer CSS present");
ok(stylesSource.includes("data-theme-scene=\"task\""), "task scene CSS present");
// Theme pack section must not *apply* backdrop-filter (comments may mention it).
const themeBgIdx = stylesSource.indexOf("Theme Pack V1");
const themeBgSlice = themeBgIdx >= 0 ? stylesSource.slice(themeBgIdx) : "";
ok(
  !/^\s*backdrop-filter\s*:/m.test(themeBgSlice) && !/^\s*-webkit-backdrop-filter\s*:/m.test(themeBgSlice),
  "theme pack CSS does not apply backdrop-filter",
);
ok(themeBgSlice.includes(".theme-bg__overlay"), "overlay wash element styled");
ok(appSource.includes("applyThemeScene"), "App wires scene from session content");
ok(appSource.includes("ThemeBackground"), "App mounts background layer");
ok(appSource.includes("applyConfiguredBaseAppearance"), "App applies configured appearance without replacing an active pack");
ok(appSource.includes("ResetThemePack") || appSource.includes("theme reset") || appSource.includes('arg === "reset"'), "reset entry exists");

console.log("\nofficial themes (kind/grouping/i18n)");

// kind resolution with legacy fallback.
ok(themePackKind({ kind: "official", builtin: true }) === "official", "kind official passthrough");
ok(themePackKind({ kind: "base", builtin: true }) === "base", "kind base passthrough");
ok(themePackKind({ kind: "user", builtin: false }) === "user", "kind user passthrough");
ok(themePackKind({ builtin: true }) === "base", "legacy builtin=true falls back to base");
ok(themePackKind({ builtin: false }) === "user", "legacy builtin=false falls back to user");

// Redesigned experience: overview home + independent gallery (select ≠ apply).
ok(overviewSource.includes("appearance-overview"), "appearance overview present");
ok(overviewSource.includes("settings.themeGallery.browse"), "overview has browse themes");
ok(overviewSource.includes("settings.themeGallery.disable") || overviewSource.includes("handleDisable"), "overview can disable pack");
ok(settingsSource.includes('tab !== "appearance"'), "appearance renders a single page header");
ok(overviewSource.includes("initialCreateBaseStyle"), "base-style copy opens a prefilled theme editor");
ok(overviewSource.includes('role="radiogroup"') && overviewSource.includes("aria-checked"), "overview segmented controls expose selection semantics");
ok(overviewSource.includes("appearance-overview__segmented--theme"), "theme-mode control uses compact settings width");
ok(overviewSource.includes("appearance-overview__segmented--text-size"), "text-size control uses its wider compact settings width");
ok(stylesSource.includes("--appearance-segmented-width: 300px") && stylesSource.includes("--appearance-segmented-width: 420px"), "overview segmented controls use intentional widths");
ok(stylesSource.includes(".appearance-overview__segmented { justify-self: stretch; width: 100%; }"), "overview segmented controls expand on narrow screens");
ok(
  overviewSource.includes('fontFamily === "custom"') && overviewSource.includes("onCustomFontNameChange(e.target.value)"),
  "custom UI font selection exposes an editable font name",
);
ok(
  overviewSource.includes('monoFontFamily === "custom"') && overviewSource.includes("onCustomMonoFontNameChange(e.target.value)"),
  "custom monospace font selection exposes an editable font name",
);
ok(
  overviewSource.includes("fontFamilyLabel(f, t)") && overviewSource.includes("monoFontFamilyLabel(f, t)"),
  "font family selectors render localized names",
);
ok(overviewSource.includes("appearance-base-style-help"), "active pack explains why base style is locked");
ok(gallerySource.includes('role="listbox"') || gallerySource.includes("role=\"listbox\""), "gallery cards are listbox options");
ok(gallerySource.includes("settings.themeGallery.apply"), "apply lives in gallery detail");
ok(gallerySource.includes("setSelected") || gallerySource.includes("onSelectPack"), "card click selects without applying");
ok(gallerySource.includes("changeTab") && gallerySource.includes("nextPacks[0]"), "changing gallery groups synchronizes the selected detail");
ok(gallerySource.includes("ActivateThemePack") || experienceSource.includes("activateThemePack"), "apply path uses activate API");
ok(experienceSource.includes("ActivateBaseStyle") || experienceSource.includes("activateBaseStyle"), "base style API wired");
ok(experienceSource.includes("selectedThemeId") || gallerySource.includes("selected"), "selection is frontend state");
ok(gallerySource.includes("loading=\"lazy\"") || gallerySource.includes('loading="lazy"'), "gallery thumbs lazy-load");
ok(gallerySource.includes("ThemePreviewSurface") || gallerySource.includes("theme-preview-surface"), "isolated detail preview");
ok(gallerySource.includes('themePackKind(pack) === "base"') && gallerySource.includes('variant="thumbnail"'), "base gallery cards render semantic UI thumbnails");
ok(gallerySource.includes('themePackKind(p) === "base"'), "immersive rail renders base-style thumbnails");
for (const style of ["graphite", "aurora", "slate", "carbon", "nocturne", "amber"] as const) {
  const basePack = { id: style, name: style, baseStyle: style, builtin: true, kind: "base" as const, active: false, hasBackground: false, tokens: {}, recipes: {} };
  for (const mode of ["light", "dark"] as const) {
    const palette = themePreviewPalette(basePack, mode);
    ok(palette === BASE_STYLE_PREVIEW_PALETTES[style][mode], `${style} ${mode} uses its canonical preview palette`);
  }
}
ok(new Set(Object.values(BASE_STYLE_PREVIEW_PALETTES).map((modes) => modes.dark.accent)).size === 6, "six base previews have distinct dark accents");
ok(
  !gallerySource.includes('tab === "catalog" && !immersive') &&
    gallerySource.includes("if (!immersive)") &&
    gallerySource.includes("previewPackGlobally(pack)"),
  "all gallery card clicks immediately start a global preview",
);
ok(gallerySource.includes("setPreviewingId(pack.id)"), "gallery preview state is visible in theme details");
ok(gallerySource.includes("nextTab !== tab") && gallerySource.includes("cancelGlobalPreview();"), "leaving all themes restores the prior appearance");
ok(gallerySource.includes("ThemePreviewControls"), "detail and immersive views share preview controls");
ok((gallerySource.match(/role="radiogroup"/g) || []).length >= 2, "appearance and scene previews are separate radio groups");
ok(gallerySource.includes("aria-checked={mode ===") && gallerySource.includes("aria-checked={scene ==="), "preview controls expose selected values");
ok(gallerySource.includes("handlePreviewRadioKey") && gallerySource.includes("tabIndex={mode ==="), "preview radios support arrow keys and roving focus");
ok(gallerySource.includes("if (!immersive || !selectedPack) return") && gallerySource.includes("previewPackGlobally(selectedPack)"), "immersive selection automatically starts a global preview");
ok(gallerySource.includes("closeImmersivePreview") && gallerySource.includes("cancelGlobalPreview();"), "leaving immersive preview restores the prior appearance");
ok(!gallerySource.includes("settings.themeGallery.tempPreview"), "redundant global-trial button is removed");
ok(gallerySource.includes("theme-gallery__rail-section") && gallerySource.includes("packs: groups.official") && gallerySource.includes("packs: groups.user") && gallerySource.includes("packs: groups.base"), "immersive rail includes official, user, and base theme groups");
ok(gallerySource.includes("filter((section) => section.packs.length > 0)"), "immersive rail hides empty groups");
ok(!gallerySource.includes("theme-gallery__tabs--compact"), "immersive rail has no duplicate bottom tab navigation");
ok(gallerySource.includes("theme-gallery__detail-status"), "active theme uses a status badge");
ok(!gallerySource.includes("disabled={busy || isActive}"), "active status is not rendered as a disabled primary action");
ok(gallerySource.includes("theme-gallery__detail-user-actions"), "user theme edit and export actions are visible outside the overflow menu");
ok((gallerySource.match(/role="menuitem"/g) || []).length === 1 && gallerySource.includes("settings.themeLibrary.delete"), "user theme overflow menu keeps only delete");
ok(gallerySource.includes("defaultTaskBackground") && gallerySource.includes("taskBackgroundDataUrl"), "editor supports an independent workspace image");
ok(gallerySource.includes("themeTokenKeys()") && gallerySource.includes('type="color"'), "editor exposes semantic theme colors");
ok(gallerySource.includes('type="range"') && gallerySource.includes("settings.themeEditor.opacity"), "editor exposes scene opacity controls");
ok(gallerySource.includes('aria-checked={safeArea === area}') && gallerySource.includes("settings.themeEditor.safeAreaHint"), "content-area control exposes radio semantics and guidance");
ok(gallerySource.includes("beginThemePreview(draft)"), "editor changes are previewed live");
ok(bridgeSource.includes("GetThemeExperience"), "bridge exposes GetThemeExperience");
ok(bridgeSource.includes("ActivateBaseStyle"), "bridge exposes ActivateBaseStyle");
ok(bridgeSource.includes("DisableThemePack"), "bridge exposes DisableThemePack");

// Gallery navigation merges built-in choices while keeping their semantics.
ok(gallerySource.includes('["catalog", t("settings.themeGallery.tabAll"), catalogPacks.length]'), "gallery combines official and base packs in all themes");
ok(gallerySource.includes('id: "official"') && gallerySource.includes('id: "base"'), "all themes keeps flagship and base sections");
ok(gallerySource.includes('role="group"') && gallerySource.includes("theme-gallery__section-head"), "catalog sections retain accessible grouping");
ok(!gallerySource.includes('["base", t("settings.themeGallery.tabBase"), groups.base.length]'), "base styles are no longer a separate top-level tab");
ok(gallerySource.includes("selectionSeeded.current") && gallerySource.includes("packs.length === 0"), "empty user tab is not overwritten by selection seeding");
ok(!overviewSource.includes("theme-card-grid"), "overview no longer renders long style card grid");

// Localized official names/descriptions in all three locales.
const OFFICIAL_IDS = [
  "official-rose-dawn",
  "official-fortune-forge",
  "official-crimson-horizon",
  "official-sage-breeze",
  "official-spark-notebook",
  "official-violet-starlight",
  "official-cyan-stage",
  "official-noir-gold",
];
for (const id of OFFICIAL_IDS) {
  for (const suffix of ["name", "description"]) {
    const key = `settings.themes.official.${id}.${suffix}`;
    ok(localeEn.includes(`"${key}"`), `en has ${key}`);
    ok(localeZh.includes(`"${key}"`), `zh has ${key}`);
    ok(localeZhTW.includes(`"${key}"`), `zh-TW has ${key}`);
  }
}
for (const key of [
  "settings.themeGallery.title",
  "settings.themeGallery.apply",
  "settings.themeGallery.browse",
  "settings.themeGallery.paletteLabel",
  "settings.themeGallery.appearancePreview",
  "settings.themeGallery.scenePreview",
  "settings.themeGallery.scenePreviewHint",
  "settings.themeGallery.tabAll",
  "settings.themeGallery.sectionFlagship",
  "settings.themeEditor.safeAreaHint",
  "settings.themeLibrary.confirmDeleteTitle",
  "settings.themeLibrary.confirmReplaceImportTitle",
  "settings.themeLibrary.replaceConfirm",
  "settings.themeLibrary.exportRightsTitle",
  "settings.themeLibrary.exportConfirm",
]) {
  ok(localeEn.includes(`"${key}"`) && localeZh.includes(`"${key}"`) && localeZhTW.includes(`"${key}"`), `gallery key ${key} in all locales`);
}

// Mock parity: 6 base + 8 official mock packs so browser dev matches the shell.
ok((bridgeSource.match(/kind: "base"/g) || []).length === 6, "mock has 6 base packs");
ok((bridgeSource.match(/kind: "official"/g) || []).length === 8, "mock has 8 official packs");
ok((bridgeSource.match(/previewUrl: new URL\("\.\.\/\.\.\/\.\.\/themes\/official\//g) || []).length === 8, "browser mock has 8 real official previews");
ok((bridgeSource.match(/backgroundUrl: new URL\("\.\.\/\.\.\/\.\.\/themes\/official\//g) || []).length === 8, "browser mock has 8 real official backgrounds");
ok((bridgeSource.match(/paneOpacity:\s*0\.50/g) || []).length === 8, "browser mock gives every official theme the product pane opacity");
ok(viteSource.includes('resolve(configDir, "../themes/official")'), "Vite dev server permits only the official theme asset directory");
ok(stylesSource.includes("container: theme-gallery / inline-size"), "gallery establishes its own responsive container");
ok(stylesSource.includes("@container theme-gallery (max-width: 760px)"), "gallery collapses from its content width");
ok(gallerySource.includes('import { createPortal } from "react-dom"') && gallerySource.includes("document.body"), "theme editor escapes settings containing blocks through a body portal");
ok(gallerySource.includes('role="dialog"') && gallerySource.includes("aria-labelledby={titleId}"), "theme editor portal retains accessible dialog semantics");
ok(stylesSource.includes("container: theme-editor / inline-size"), "theme editor establishes an independent responsive container");
ok(stylesSource.includes("@container theme-editor (max-width: 920px)"), "theme editor collapses from its own width");
ok(stylesSource.includes(".theme-editor__setting-row .set-seg__btn { flex: 1; min-width: 0; }"), "all editor segmented setting buttons share available width");
ok(stylesSource.includes("grid-template-columns: repeat(3, minmax(0, 1fr))"), "base appearance options wrap at narrow editor widths");
ok(stylesSource.includes(".theme-gallery__preview-control"), "preview dimensions have labeled layout styling");
ok(gallerySource.includes("settings.themeGallery.scenePreviewHint") && gallerySource.includes("theme-gallery__preview-help"), "scene preview explains home and workspace behavior");
ok(localeZh.includes('"settings.themeGallery.sceneHome": "首页展示"') && localeZh.includes('"settings.themeGallery.sceneTask": "工作区展示"'), "scene options use explicit Chinese labels");
ok(localeZh.includes('"settings.themeGallery.subtitle": "点击主题即可全局预览，应用后才会保存"'), "gallery explains click-to-preview and apply-to-save semantics");
ok(
  localeEn.includes('"settings.themeGallery.restoreGraphite": "Restore Graphite appearance"') &&
    localeEn.includes("detailed typography are preserved"),
  "English restore copy names Graphite and preserves detailed typography",
);
ok(
  localeZh.includes('"settings.themeGallery.restoreGraphite": "恢复石墨基础外观"') &&
    localeZh.includes("保留明暗模式、字体、字号及详细排版设置") &&
    localeZhTW.includes('"settings.themeGallery.restoreGraphite": "恢復石墨基礎外觀"') &&
    localeZhTW.includes("保留明暗模式、字型、字號及詳細排版設定"),
  "Chinese restore copy localizes Graphite as 石墨 and preserves detailed typography",
);
ok(stylesSource.includes(".theme-gallery__detail-user-actions") && stylesSource.includes("grid-template-columns: repeat(2, minmax(0, 1fr))"), "user theme edit and export actions share a balanced row");
ok(stylesSource.includes(".theme-gallery__rail-section-head") && stylesSource.includes(".theme-gallery__rail-section-items"), "immersive rail groups have lightweight headings and item stacks");
ok(stylesSource.includes(".theme-gallery__detail-status"), "active status has dedicated non-button styling");
ok(stylesSource.includes(".theme-editor__setting-hint"), "content-area guidance has dedicated responsive styling");
ok(localeZh.includes('"settings.themeEditor.safeArea": "界面内容区域"') && localeZh.includes('"settings.themeEditor.safeAreaHint": "选择文字和卡片主要显示的位置；建议避开图片主体。"'), "Chinese content-area copy explains foreground placement");

// Pack overlay stays at :root — Workbench/Creation element-scoped auto-light
// selectors must keep winning in their subtree (theme never overrides them).
ok(!packSource.includes(".app--"), "pack overlay never targets layout-scoped selectors");
ok(packSource.includes("prefers-color-scheme: light"), "auto mode follows system light/dark");

// Keep ThemeLibrary available for any residual editor helpers.
ok(librarySource.includes("ThemeLibrary") || librarySource.includes("ThemeEditor"), "ThemeLibrary module retained for editor/helpers");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
