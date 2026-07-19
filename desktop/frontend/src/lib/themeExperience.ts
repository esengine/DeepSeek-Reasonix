// Unified theme experience controller for the redesigned appearance overview
// and theme gallery. Backend is source of truth for persistence; selected and
// temporary-preview ids stay in memory only.

import { app } from "./bridge";
import {
  applyThemePack,
  beginThemePreview,
  cancelThemePreview,
  clearPreviewSnapshotOnly,
  clearThemePack,
  commitThemePreview,
  setBaseAppearance,
  themePackKind,
  type ThemePackView,
} from "./themePack";
import { applyTheme, isThemeStyle, type Theme, type ThemeStyle } from "./theme";

export type ThemeExperienceView = {
  themeMode: Theme | string;
  baseStyle: ThemeStyle | string;
  effectiveStyle: ThemeStyle | string;
  activeThemeId?: string;
  activePack?: ThemePackView | null;
  safeMode: boolean;
};

export type GalleryTab = "catalog" | "user";

export type ThemeSelection =
  | { kind: "base"; id: ThemeStyle; pack?: ThemePackView }
  | { kind: "official" | "user"; id: string; pack: ThemePackView };

let experienceCache: ThemeExperienceView | null = null;
let previewDepth = 0;

export function getCachedThemeExperience(): ThemeExperienceView | null {
  return experienceCache;
}

/**
 * Return the configured base style that React owners should mirror.
 * An active pack's effectiveStyle is intentionally excluded: it is a live DOM
 * override, not the persisted base appearance restored when the pack is cleared.
 */
export function configuredBaseStyleForSync(view: ThemeExperienceView): ThemeStyle | null {
  if (!view.safeMode && view.activePack) return null;
  return (isThemeStyle(view.baseStyle) ? view.baseStyle : "graphite") as ThemeStyle;
}

export async function loadThemeExperience(): Promise<ThemeExperienceView> {
  // Prefer the unified API; fall back for older shells / partial mocks.
  const api = app as typeof app & {
    GetThemeExperience?: () => Promise<ThemeExperienceView>;
    ActivateBaseStyle?: (style: string) => Promise<void>;
    DisableThemePack?: () => Promise<void>;
    RestoreGraphiteAppearance?: () => Promise<void>;
  };
  if (typeof api.GetThemeExperience === "function") {
    try {
      const view = await api.GetThemeExperience();
      experienceCache = normalizeExperience(view);
      return experienceCache;
    } catch {
      // Fall through to partial Settings / GetActiveThemePack path.
    }
  }

  // Partial test mocks and older shells may only expose Settings + GetActiveThemePack.
  let themeMode: Theme = "auto";
  let baseStyle: ThemeStyle = "graphite";
  let safeMode = false;
  try {
    const settingsApi = app as typeof app & {
      DesktopStartupSettings?: () => Promise<{ desktopTheme?: string; desktopThemeStyle?: string; safeMode?: boolean }>;
      Settings?: () => Promise<{ desktopTheme?: string; desktopThemeStyle?: string; safeMode?: boolean }>;
    };
    const settings =
      typeof settingsApi.DesktopStartupSettings === "function"
        ? await settingsApi.DesktopStartupSettings()
        : typeof settingsApi.Settings === "function"
          ? await settingsApi.Settings()
          : null;
    if (settings) {
      themeMode = (settings.desktopTheme as Theme) || "auto";
      baseStyle = (isThemeStyle(settings.desktopThemeStyle) ? settings.desktopThemeStyle : "graphite") as ThemeStyle;
      safeMode = (settings as { safeMode?: boolean }).safeMode === true;
    }
  } catch {
    // Keep defaults.
  }

  let activePack: ThemePackView | null = null;
  let activeThemeId: string | undefined;
  try {
    if (typeof app.GetActiveThemePack === "function") {
      const active = await app.GetActiveThemePack();
      activePack = active?.pack ?? null;
      activeThemeId = active?.activeThemeId || undefined;
      safeMode = safeMode || active?.safeMode === true;
    }
  } catch {
    // Keep pack unset.
  }

  const view: ThemeExperienceView = {
    themeMode,
    baseStyle,
    effectiveStyle: activePack?.baseStyle || baseStyle,
    activeThemeId,
    activePack,
    safeMode,
  };
  experienceCache = normalizeExperience(view);
  return experienceCache;
}

function normalizeExperience(view: ThemeExperienceView): ThemeExperienceView {
  const themeMode = view.themeMode === "light" || view.themeMode === "dark" || view.themeMode === "auto" ? view.themeMode : "auto";
  const baseStyle = isThemeStyle(view.baseStyle) ? view.baseStyle : "graphite";
  const effectiveStyle = isThemeStyle(view.effectiveStyle) ? view.effectiveStyle : baseStyle;
  return {
    themeMode,
    baseStyle,
    effectiveStyle,
    activeThemeId: view.activeThemeId || undefined,
    activePack: view.activePack ?? null,
    safeMode: view.safeMode === true,
  };
}

/** Apply a loaded experience to the live DOM (no network). */
export function applyExperienceToDOM(view: ThemeExperienceView): void {
  const theme = (view.themeMode === "light" || view.themeMode === "dark" || view.themeMode === "auto" ? view.themeMode : "auto") as Theme;
  const base = (isThemeStyle(view.baseStyle) ? view.baseStyle : "graphite") as ThemeStyle;
  setBaseAppearance(theme, base);
  if (view.safeMode || !view.activePack) {
    clearThemePack();
    applyTheme(theme, base, { persist: false });
    return;
  }
  applyTheme(theme, base, { persist: false });
  applyThemePack(view.activePack);
}

export async function activateBaseStyle(style: ThemeStyle): Promise<ThemeExperienceView> {
  const api = app as typeof app & { ActivateBaseStyle?: (style: string) => Promise<void> };
  if (typeof api.ActivateBaseStyle === "function") {
    await api.ActivateBaseStyle(style);
  } else {
    // Legacy fallback: clear pack then set appearance.
    await app.ResetThemePack();
    const theme = (experienceCache?.themeMode as Theme) || "auto";
    await app.SetDesktopAppearance(theme, style);
  }
  endPreviewIfAny();
  const view = await loadThemeExperience();
  applyExperienceToDOM(view);
  return view;
}

export async function activateThemePack(id: string): Promise<ThemeExperienceView> {
  await app.ActivateThemePack(id);
  // Commit the preview only after persistence succeeds. If activation fails,
  // the snapshot must remain available so Back/Cancel can restore the prior
  // appearance.
  clearPreviewSnapshotOnly();
  endPreviewIfAny();
  const view = await loadThemeExperience();
  applyExperienceToDOM(view);
  return view;
}

export async function disableThemePack(): Promise<ThemeExperienceView> {
  const api = app as typeof app & { DisableThemePack?: () => Promise<void> };
  if (typeof api.DisableThemePack === "function") {
    await api.DisableThemePack();
  } else {
    await app.ResetThemePack();
  }
  endPreviewIfAny();
  const view = await loadThemeExperience();
  applyExperienceToDOM(view);
  return view;
}

export async function restoreGraphiteAppearance(): Promise<ThemeExperienceView> {
  const api = app as typeof app & { RestoreGraphiteAppearance?: () => Promise<void> };
  if (typeof api.RestoreGraphiteAppearance === "function") {
    await api.RestoreGraphiteAppearance();
  } else {
    await app.ResetThemePack();
    const theme = (experienceCache?.themeMode as Theme) || "auto";
    await app.SetDesktopAppearance(theme, "graphite");
  }
  endPreviewIfAny();
  const view = await loadThemeExperience();
  applyExperienceToDOM(view);
  return view;
}

export async function setThemeMode(mode: Theme): Promise<ThemeExperienceView> {
  const base = (isThemeStyle(experienceCache?.baseStyle) ? experienceCache!.baseStyle : "graphite") as ThemeStyle;
  await app.SetDesktopAppearance(mode, base);
  const view = await loadThemeExperience();
  // Theme mode is independent of pack — re-apply pack on top.
  applyExperienceToDOM(view);
  return view;
}

export function startGlobalPreview(pack: ThemePackView): void {
  previewDepth += 1;
  beginThemePreview(pack);
}

export function cancelGlobalPreview(): void {
  if (previewDepth <= 0) return;
  previewDepth = 0;
  cancelThemePreview();
}

export function commitGlobalPreview(pack: ThemePackView | null): void {
  previewDepth = 0;
  commitThemePreview(pack);
}

function endPreviewIfAny(): void {
  if (previewDepth > 0) {
    previewDepth = 0;
    cancelThemePreview();
  }
}

export function isPreviewActive(): boolean {
  return previewDepth > 0;
}

/** Group packs for the gallery tabs. */
export function groupThemePacks(packs: ThemePackView[]): {
  official: ThemePackView[];
  user: ThemePackView[];
  base: ThemePackView[];
} {
  const official: ThemePackView[] = [];
  const user: ThemePackView[] = [];
  const base: ThemePackView[] = [];
  for (const p of packs) {
    const k = themePackKind(p);
    if (k === "official") official.push(p);
    else if (k === "base") base.push(p);
    else user.push(p);
  }
  return { official, user, base };
}

export function selectionFromPack(pack: ThemePackView): ThemeSelection {
  const k = themePackKind(pack);
  if (k === "base") {
    return { kind: "base", id: (isThemeStyle(pack.id) ? pack.id : "graphite") as ThemeStyle, pack };
  }
  return { kind: k, id: pack.id, pack };
}

export function isSelectionActive(sel: ThemeSelection | null, exp: ThemeExperienceView | null): boolean {
  if (!sel || !exp) return false;
  if (sel.kind === "base") {
    return !exp.activeThemeId && exp.baseStyle === sel.id;
  }
  return exp.activeThemeId === sel.id;
}
