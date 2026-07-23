// themePack.ts applies controlled Theme Pack V1/V2 overlays on top of the existing
// auto/light/dark + baseStyle system. Packs cannot execute CSS/JS or load remote
// resources — only semantic tokens, recipe enums, and local background images.

import { applyTheme, getTheme, getThemeStyle, isThemeStyle, type Theme, type ThemeStyle } from "./theme";

export type ThemePackTokens = {
  light?: Record<string, string>;
  dark?: Record<string, string>;
};

export type ThemePackRecipes = {
  density?: "compact" | "comfortable" | string;
  corners?: "square" | "soft" | "round" | string;
};

export type ThemePackBackground = {
  image?: string;
  focusX: number;
  focusY: number;
  safeArea?: "left" | "right" | "center" | string;
  homeOpacity: number;
  taskOpacity: number;
  overlayStrength: number;
  paneOpacity: number;
};

export type ThemePackSceneBackground = {
  image?: string;
  focusX: number;
  focusY: number;
  safeArea?: "left" | "right" | "center" | string;
  opacity: number;
  overlayStrength: number;
  paneOpacity: number;
};

export type ThemeContrastWarning = {
  mode: string;
  pair: string;
  ratio: number;
  minimum: number;
  suggest?: string;
};

export type ThemePackKind = "base" | "official" | "user";

export type ThemePackView = {
  id: string;
  name: string;
  author?: string;
  description?: string;
  license?: string;
  baseStyle: string;
  builtin: boolean;
  /** New in the official-themes release; old backends/mocks may omit it. */
  kind?: ThemePackKind;
  active: boolean;
  hasBackground: boolean;
  backgroundUrl?: string;
  taskBackgroundUrl?: string;
  previewUrl?: string;
  nameKey?: string;
  descriptionKey?: string;
  tokens: ThemePackTokens;
  recipes: ThemePackRecipes;
  background?: ThemePackBackground | null;
  taskBackground?: ThemePackSceneBackground | null;
  contrastWarnings?: ThemeContrastWarning[];
};

/**
 * Resolve the pack group. Older mocks/responses without `kind` fall back to
 * the historical builtin flag: builtin ? "base" : "user".
 */
export function themePackKind(pack: Pick<ThemePackView, "kind" | "builtin">): ThemePackKind {
  if (pack.kind === "base" || pack.kind === "official" || pack.kind === "user") return pack.kind;
  return pack.builtin ? "base" : "user";
}

export type ThemeActiveView = {
  activeThemeId?: string;
  pack?: ThemePackView | null;
  safeMode: boolean;
};

export type ThemeSaveInput = {
  id: string;
  name: string;
  author?: string;
  description?: string;
  license?: string;
  baseStyle: string;
  tokens: ThemePackTokens;
  recipes: ThemePackRecipes;
  background?: ThemePackBackground | null;
  taskBackground?: ThemePackSceneBackground | null;
  backgroundDataUrl?: string;
  taskBackgroundDataUrl?: string;
  clearBackground?: boolean;
  clearTaskBackground?: boolean;
  replace?: boolean;
  activate?: boolean;
};

export type ThemeImportResult = {
  pack: ThemePackView;
  replaced: boolean;
  needsReplace?: boolean;
  pendingId?: string;
};

export type ThemeScene = "home" | "task";

const PACK_STYLE_ID = "reasonix-theme-pack-overlay";
const TOKEN_KEYS = [
  "bg",
  "bgSoft",
  "bgElev",
  "panel",
  "sidebar",
  "chat",
  "workspace",
  "workspaceFiles",
  "border",
  "borderSoft",
  "fg",
  "fgDim",
  "fgFaint",
  "accent",
  "accentFg",
  "ok",
  "warn",
  "err",
] as const;

const TOKEN_TO_CSS: Record<string, string[]> = {
  bg: ["--bg", "--stage"],
  bgSoft: ["--bg-soft", "--surface-3"],
  bgElev: ["--bg-elev"],
  panel: ["--panel", "--bg-elev", "--surface"],
  sidebar: ["--sidebar-bg"],
  chat: ["--chat-bg"],
  workspace: ["--workspace-preview-bg"],
  workspaceFiles: ["--workspace-files-bg"],
  border: ["--border"],
  borderSoft: ["--border-soft"],
  fg: ["--fg", "--text"],
  fgDim: ["--fg-dim", "--text-2"],
  fgFaint: ["--fg-faint", "--text-3"],
  accent: ["--accent", "--accent-strong", "--control-primary-bg"],
  accentFg: ["--accent-fg", "--control-primary-fg"],
  ok: ["--ok"],
  warn: ["--warn"],
  err: ["--err"],
};

let activePack: ThemePackView | null = null;
let activeScene: ThemeScene = "home";
/** User config appearance under the pack (restored on clear / restore-default). */
let baseAppearance: { theme: Theme; style: ThemeStyle } | null = null;
let previewSnapshot: {
  pack: ThemePackView | null;
  theme: Theme;
  style: ThemeStyle;
  baseAppearance: { theme: Theme; style: ThemeStyle } | null;
} | null = null;

// Browser development uses Vite-bundled copies of the same official images
// that Wails serves through /__reasonix_theme_asset/. Only exact, internally
// registered URLs may cross the background URL safety boundary.
const trustedBundledThemeBackgroundURLs = new Set<string>();

export function registerTrustedThemeBackgroundURLs(urls: readonly string[]): void {
  if (typeof window === "undefined" || !window.location) return;
  for (const raw of urls) {
    try {
      const parsed = new URL(raw, window.location.href);
      if (parsed.origin !== window.location.origin) continue;
      const path = decodeURIComponent(parsed.pathname);
      const viteDevOfficial = /\/desktop\/themes\/official\/official-[a-z0-9-]+\/background\.webp$/.test(path);
      const viteBuiltOfficial = /^\/assets\/background-[a-zA-Z0-9_-]+\.webp$/.test(path);
      if (viteDevOfficial || viteBuiltOfficial) trustedBundledThemeBackgroundURLs.add(parsed.href);
    } catch {
      // Ignore malformed candidates; they remain outside the allow-list.
    }
  }
}

export function getActiveThemePack(): ThemePackView | null {
  return activePack;
}

export function getThemeScene(): ThemeScene {
  return activeScene;
}

export function getBaseAppearance(): { theme: Theme; style: ThemeStyle } | null {
  return baseAppearance ? { ...baseAppearance } : null;
}

export function isThemeTokenKey(key: string): boolean {
  return (TOKEN_KEYS as readonly string[]).includes(key);
}

export function themeTokenKeys(): readonly string[] {
  return TOKEN_KEYS;
}

/**
 * Remember the user's config appearance before a pack overrides baseStyle.
 * Call from settings load when preferences are known, or let applyThemePack
 * snapshot automatically on first non-preview apply.
 */
export function setBaseAppearance(theme: Theme, style: ThemeStyle): void {
  baseAppearance = { theme, style };
}

/**
 * Apply a configured appearance without replacing an active pack's live base
 * style. The configured values remain the restore target when the pack is
 * cleared, while the pack continues to own the effective visual direction.
 */
export function applyConfiguredBaseAppearance(theme: Theme, style: ThemeStyle): void {
  setBaseAppearance(theme, style);
  applyTheme(theme, style, { persist: false });
  if (activePack) applyThemePack(activePack);
}

/** Apply or clear the active theme pack overlay. Pass null only clears overlay attrs — prefer clearThemePack(). */
export function applyThemePack(pack: ThemePackView | null | undefined, options?: { preview?: boolean }): void {
  if (typeof document === "undefined") return;
  const next = pack ?? null;
  if (!options?.preview) {
    activePack = next;
  }

  const root = document.documentElement;
  if (!next) {
    root.removeAttribute("data-theme-pack");
    removePackStyleElement();
    clearBackgroundCSSVars(root);
    return;
  }

  // Snapshot config appearance once before the first pack overrides baseStyle.
  if (!options?.preview && !baseAppearance) {
    baseAppearance = { theme: getTheme(), style: getThemeStyle() };
  }

  root.setAttribute("data-theme-pack", next.id);

  // Base style from the pack (inherits remaining tokens from the direction sheets).
  const style = isThemeStyle(next.baseStyle) ? next.baseStyle : getThemeStyle();
  applyTheme(getTheme(), style, { persist: false });

  const css = buildPackOverlayCSS(next);
  ensurePackStyleElement().textContent = css;
  applyBackgroundCSSVars(root, next);
  applyThemeScene(activeScene);
}

/**
 * Clear the active pack and restore the user's base appearance (theme mode + style).
 * Fixes: enabling Aurora then "restore default" must return data-theme-style to Graphite
 * (or whatever was configured), not leave the pack's baseStyle behind.
 */
export function clearThemePack(): void {
  previewSnapshot = null;
  activePack = null;
  if (typeof document !== "undefined") {
    const root = document.documentElement;
    root.removeAttribute("data-theme-pack");
    removePackStyleElement();
    clearBackgroundCSSVars(root);
  }
  if (baseAppearance) {
    applyTheme(baseAppearance.theme, baseAppearance.style, { persist: false });
  }
  // Keep baseAppearance so subsequent applyThemePack can re-snapshot if needed;
  // after full clear the restored style IS the base.
}

/** Scene is home (full background) vs task (dimmed + overlay). Does not touch chat state. */
export function applyThemeScene(scene: ThemeScene): void {
  activeScene = scene === "task" ? "task" : "home";
  if (typeof document === "undefined") return;
  const app = document.querySelector(".app") ?? document.documentElement;
  app.setAttribute("data-theme-scene", activeScene);
  // Also mirror on root for CSS that targets :root.
  document.documentElement.setAttribute("data-theme-scene", activeScene);
}

export function beginThemePreview(pack: ThemePackView): void {
  if (!previewSnapshot) {
    previewSnapshot = {
      pack: activePack,
      theme: getTheme(),
      style: getThemeStyle(),
      baseAppearance: baseAppearance ? { ...baseAppearance } : null,
    };
  }
  applyThemePack(pack, { preview: true });
}

export function cancelThemePreview(): void {
  if (!previewSnapshot) return;
  const snap = previewSnapshot;
  previewSnapshot = null;
  baseAppearance = snap.baseAppearance;
  applyTheme(snap.theme, snap.style, { persist: false });
  if (snap.pack) {
    applyThemePack(snap.pack);
  } else {
    // No active pack under the preview — strip overlay without changing restored style again.
    activePack = null;
    if (typeof document !== "undefined") {
      const root = document.documentElement;
      root.removeAttribute("data-theme-pack");
      removePackStyleElement();
      clearBackgroundCSSVars(root);
    }
  }
}

export function commitThemePreview(pack: ThemePackView | null): void {
  previewSnapshot = null;
  if (pack) {
    applyThemePack(pack);
  } else {
    clearThemePack();
  }
}

/**
 * Clear the preview snapshot without restoring the original theme.
 * Use this after persistent activation succeeds and before editor cleanup so
 * cancelThemePreview() cannot overwrite the newly applied theme.
 */
export function clearPreviewSnapshotOnly(): void {
  previewSnapshot = null;
}

function ensurePackStyleElement(): HTMLStyleElement {
  let el = document.getElementById(PACK_STYLE_ID) as HTMLStyleElement | null;
  if (!el) {
    el = document.createElement("style");
    el.id = PACK_STYLE_ID;
    // Append last so pack overrides win over stylesheets and Creation locals.
    document.head.appendChild(el);
  } else if (el.parentElement === document.head) {
    document.head.appendChild(el);
  }
  return el;
}

function removePackStyleElement(): void {
  const el = document.getElementById(PACK_STYLE_ID);
  if (!el) return;
  if (typeof el.remove === "function") el.remove();
  else el.parentElement?.removeChild(el);
}

function buildPackOverlayCSS(pack: ThemePackView): string {
  const light = tokensToDecls(pack.tokens?.light);
  const dark = tokensToDecls(pack.tokens?.dark);
  const recipes = recipeDecls(pack.recipes);
  const chunks: string[] = [];

  // Recipe vars apply in both modes.
  if (recipes) {
    chunks.push(`:root[data-theme-pack="${cssEscape(pack.id)}"]{${recipes}}`);
  }

  // Dark tokens (default / forced dark / auto-dark).
  if (dark) {
    chunks.push(`:root[data-theme-pack="${cssEscape(pack.id)}"]{${dark}}`);
    chunks.push(`:root[data-theme="dark"][data-theme-pack="${cssEscape(pack.id)}"]{${dark}}`);
  }
  // Light tokens.
  if (light) {
    chunks.push(`:root[data-theme="light"][data-theme-pack="${cssEscape(pack.id)}"]{${light}}`);
    chunks.push(`@media (prefers-color-scheme: light){:root:not([data-theme])[data-theme-pack="${cssEscape(pack.id)}"]{${light}}}`);
  }

  // Soft accent derived when accent is set.
  const accentDark = pack.tokens?.dark?.accent;
  const accentLight = pack.tokens?.light?.accent;
  if (accentDark && isSafeHex(accentDark)) {
    chunks.push(
      `:root[data-theme-pack="${cssEscape(pack.id)}"]{--accent-soft: color-mix(in srgb, ${accentDark} 16%, transparent);}`,
    );
  }
  if (accentLight && isSafeHex(accentLight)) {
    chunks.push(
      `:root[data-theme="light"][data-theme-pack="${cssEscape(pack.id)}"]{--accent-soft: color-mix(in srgb, ${accentLight} 12%, transparent);}`,
    );
  }

  return chunks.join("\n");
}

function tokensToDecls(tokens?: Record<string, string>): string {
  if (!tokens) return "";
  const parts: string[] = [];
  for (const [key, value] of Object.entries(tokens)) {
    if (!isThemeTokenKey(key) || !isSafeHex(value)) continue;
    const cssVars = TOKEN_TO_CSS[key] ?? [];
    for (const css of cssVars) {
      parts.push(`${css}:${value}`);
    }
  }
  return parts.join(";");
}

function recipeDecls(recipes?: ThemePackRecipes): string {
  if (!recipes) return "";
  const parts: string[] = [];
  const density = recipes.density === "compact" ? "compact" : "comfortable";
  const corners = recipes.corners === "square" || recipes.corners === "round" ? recipes.corners : "soft";
  if (density === "compact") {
    parts.push("--theme-density-pad:6px", "--theme-density-gap:6px", "--theme-row-h:28px");
  } else {
    parts.push("--theme-density-pad:10px", "--theme-density-gap:10px", "--theme-row-h:34px");
  }
  if (corners === "square") {
    parts.push("--r-s:0px", "--r:2px", "--r-l:4px", "--radius:2px");
  } else if (corners === "round") {
    parts.push("--r-s:8px", "--r:14px", "--r-l:18px", "--radius:14px");
  } else {
    parts.push("--r-s:5px", "--r:8px", "--r-l:11px", "--radius:8px");
  }
  return parts.join(";");
}

function applyBackgroundCSSVars(root: HTMLElement, pack: ThemePackView): void {
  const home = pack.background;
  const homeUrl = pack.backgroundUrl || "";
  const task = pack.taskBackground;
  const taskUrl = pack.taskBackgroundUrl || "";
  const safeHomeUrl = Boolean(home && homeUrl && isSafeBackgroundURL(homeUrl));
  const safeTaskUrl = Boolean(task && taskUrl && isSafeBackgroundURL(taskUrl));
  if ((!safeHomeUrl && !safeTaskUrl) || !pack.hasBackground) {
    clearBackgroundCSSVars(root);
    return;
  }

  if (safeHomeUrl && home) {
    root.style.setProperty("--theme-bg-home-image", `url("${cssUrlEscape(homeUrl)}")`);
    root.style.setProperty("--theme-bg-home-focus-x", `${clamp01(home.focusX) * 100}%`);
    root.style.setProperty("--theme-bg-home-focus-y", `${clamp01(home.focusY) * 100}%`);
    root.style.setProperty("--theme-bg-home-opacity", String(clamp01(home.homeOpacity ?? 1)));
    // Pane transparency: how much the background shows through the UI panes.
    const homePane = clamp01(home.paneOpacity ?? 0.50);
    root.style.setProperty("--theme-pane-alpha", String(homePane));
    // Pre-computed percentages for CSS (avoids calc() compat issues).
    // Clamp to 100% to prevent color-mix from receiving values > 100%.
    root.style.setProperty("--theme-pane-shell-pct", `${Math.min((homePane + 0.08) * 100, 100)}%`);
    root.style.setProperty("--theme-pane-card-pct", `${Math.min((homePane + 0.26) * 100, 100)}%`);
    // Legacy aliases keep V1 tests and third-party diagnostics stable.
    root.style.setProperty("--theme-bg-image", `url("${cssUrlEscape(homeUrl)}")`);
    root.style.setProperty("--theme-bg-focus-x", `${clamp01(home.focusX) * 100}%`);
    root.style.setProperty("--theme-bg-focus-y", `${clamp01(home.focusY) * 100}%`);
  } else {
    root.style.setProperty("--theme-bg-home-image", "none");
  }

  const taskSource = safeTaskUrl && task ? task : home;
  const effectiveTaskUrl = safeTaskUrl ? taskUrl : safeHomeUrl ? homeUrl : "";
  if (taskSource && effectiveTaskUrl) {
    root.style.setProperty("--theme-bg-task-image", `url("${cssUrlEscape(effectiveTaskUrl)}")`);
    root.style.setProperty("--theme-bg-task-focus-x", `${clamp01(taskSource.focusX) * 100}%`);
    root.style.setProperty("--theme-bg-task-focus-y", `${clamp01(taskSource.focusY) * 100}%`);
  } else {
    root.style.setProperty("--theme-bg-task-image", "none");
  }
  const taskOpacity = task ? task.opacity : home?.taskOpacity;
  const taskOverlay = task ? task.overlayStrength : home?.overlayStrength;
  root.style.setProperty("--theme-bg-task-opacity", String(clamp01(taskOpacity ?? 0.28)));
  root.style.setProperty("--theme-bg-task-overlay", String(clamp01(taskOverlay ?? 0.62)));
  root.style.setProperty("--theme-bg-overlay", String(clamp01(taskOverlay ?? 0.62)));
  // Task scene pane transparency (defaults to home paneOpacity if not set on task scene).
  const taskPane = clamp01(task?.paneOpacity ?? home?.paneOpacity ?? 0.68);
  root.style.setProperty("--theme-pane-task-alpha", String(taskPane));
  root.style.setProperty("--theme-pane-task-shell-pct", `${Math.min((taskPane + 0.08) * 100, 100)}%`);
  root.style.setProperty("--theme-pane-task-card-pct", `${Math.min((taskPane + 0.14) * 100, 100)}%`);
  const safe = taskSource?.safeArea === "left" || taskSource?.safeArea === "right" ? taskSource.safeArea : "center";
  root.setAttribute("data-theme-safe-area", safe);
  root.setAttribute("data-theme-has-bg", "true");
}

function clearBackgroundCSSVars(root: HTMLElement): void {
  root.style.removeProperty("--theme-bg-home-image");
  root.style.removeProperty("--theme-bg-home-focus-x");
  root.style.removeProperty("--theme-bg-home-focus-y");
  root.style.removeProperty("--theme-bg-task-image");
  root.style.removeProperty("--theme-bg-task-focus-x");
  root.style.removeProperty("--theme-bg-task-focus-y");
  root.style.removeProperty("--theme-bg-task-overlay");
  root.style.removeProperty("--theme-bg-image");
  root.style.removeProperty("--theme-bg-focus-x");
  root.style.removeProperty("--theme-bg-focus-y");
  root.style.removeProperty("--theme-bg-home-opacity");
  root.style.removeProperty("--theme-bg-task-opacity");
  root.style.removeProperty("--theme-bg-overlay");
  root.style.removeProperty("--theme-pane-alpha");
  root.style.removeProperty("--theme-pane-task-alpha");
  root.style.removeProperty("--theme-pane-shell-pct");
  root.style.removeProperty("--theme-pane-task-shell-pct");
  root.style.removeProperty("--theme-pane-card-pct");
  root.style.removeProperty("--theme-pane-task-card-pct");
  root.removeAttribute("data-theme-safe-area");
  root.removeAttribute("data-theme-has-bg");
}

export function isSafeHex(value: string): boolean {
  return /^#([0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/.test(value.trim());
}

export function isSafeBackgroundURL(url: string): boolean {
  const u = url.trim();
  if (!u) return false;
  if (u.startsWith("/__reasonix_theme_asset/")) return true;
  if (u.startsWith("data:image/png;base64,")) return true;
  if (u.startsWith("data:image/jpeg;base64,")) return true;
  if (u.startsWith("data:image/jpg;base64,")) return true;
  if (u.startsWith("data:image/webp;base64,")) return true;
  if (u.startsWith("blob:")) return true;
  if (trustedBundledThemeBackgroundURLs.has(u)) return true;
  return false;
}

function clamp01(v: number): number {
  if (!Number.isFinite(v)) return 0.5;
  return Math.min(1, Math.max(0, v));
}

function cssEscape(value: string): string {
  return value.replace(/[^a-zA-Z0-9_-]/g, "");
}

function cssUrlEscape(url: string): string {
  return url.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

/** Build a draft pack view for live editor preview (may use data-URL background). */
export function draftPackView(input: {
  id: string;
  name: string;
  baseStyle: string;
  tokens: ThemePackTokens;
  recipes: ThemePackRecipes;
  background?: ThemePackBackground | null;
  backgroundUrl?: string;
  taskBackground?: ThemePackSceneBackground | null;
  taskBackgroundUrl?: string;
}): ThemePackView {
  return {
    id: input.id || "preview",
    name: input.name || "Preview",
    baseStyle: input.baseStyle || "graphite",
    builtin: false,
    active: false,
    hasBackground: Boolean((input.backgroundUrl && input.background) || (input.taskBackgroundUrl && input.taskBackground)),
    backgroundUrl: input.backgroundUrl,
    taskBackgroundUrl: input.taskBackgroundUrl,
    tokens: input.tokens || {},
    recipes: input.recipes || { density: "comfortable", corners: "soft" },
    background: input.background ?? undefined,
    taskBackground: input.taskBackground ?? undefined,
  };
}

export function emptyThemeTokens(): ThemePackTokens {
  return { light: {}, dark: {} };
}

export function defaultBackground(): ThemePackBackground {
  return {
    focusX: 0.5,
    focusY: 0.5,
    safeArea: "center",
    homeOpacity: 1,
    taskOpacity: 0.28,
    overlayStrength: 0.62,
    paneOpacity: 0.50,
  };
}

export function defaultTaskBackground(): ThemePackSceneBackground {
  return {
    focusX: 0.5,
    focusY: 0.5,
    safeArea: "center",
    opacity: 0.28,
    overlayStrength: 0.62,
    paneOpacity: 0.68,
  };
}
