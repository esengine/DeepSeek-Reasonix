export const FONT_FAMILIES = ["system", "yahei", "pingfang", "noto", "custom"] as const;
export const MONO_FONT_FAMILIES = ["system", "cascadia", "jetbrains", "sfmono", "custom"] as const;

export type FontFamily = (typeof FONT_FAMILIES)[number];
export type MonoFontFamily = (typeof MONO_FONT_FAMILIES)[number];

export const DEFAULT_FONT_FAMILY: FontFamily = "system";
export const DEFAULT_MONO_FONT_FAMILY: MonoFontFamily = "system";

const FONT_FAMILY_KEY = "reasonix-font-family";
const CUSTOM_FONT_KEY = "reasonix-font-family-custom";
const MONO_FONT_FAMILY_KEY = "reasonix-mono-font-family";
const CUSTOM_MONO_FONT_KEY = "reasonix-mono-font-family-custom";

export function isFontFamily(value: unknown): value is FontFamily {
  return typeof value === "string" && (FONT_FAMILIES as readonly string[]).includes(value);
}

export function isMonoFontFamily(value: unknown): value is MonoFontFamily {
  return typeof value === "string" && (MONO_FONT_FAMILIES as readonly string[]).includes(value);
}

export function getFontFamily(): FontFamily {
  const stored = typeof localStorage !== "undefined" ? localStorage.getItem(FONT_FAMILY_KEY) : null;
  return isFontFamily(stored) ? stored : DEFAULT_FONT_FAMILY;
}

export function getMonoFontFamily(): MonoFontFamily {
  const stored = typeof localStorage !== "undefined" ? localStorage.getItem(MONO_FONT_FAMILY_KEY) : null;
  return isMonoFontFamily(stored) ? stored : DEFAULT_MONO_FONT_FAMILY;
}

export function getCustomFontName(): string {
  if (typeof localStorage === "undefined") return "";
  return localStorage.getItem(CUSTOM_FONT_KEY) ?? "";
}

export function getCustomMonoFontName(): string {
  if (typeof localStorage === "undefined") return "";
  return localStorage.getItem(CUSTOM_MONO_FONT_KEY) ?? "";
}

export function setCustomFontName(name: string): void {
  try {
    localStorage.setItem(CUSTOM_FONT_KEY, name);
  } catch {
    /* private mode / no storage */
  }
}

export function setCustomMonoFontName(name: string): void {
  try {
    localStorage.setItem(CUSTOM_MONO_FONT_KEY, name);
  } catch {
    /* private mode / no storage */
  }
}

export function applyFontFamily(font: FontFamily): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  if (font === DEFAULT_FONT_FAMILY) {
    root.removeAttribute("data-font-family");
    root.style.removeProperty("--font-family-custom");
  } else {
    root.setAttribute("data-font-family", font);
    if (font === "custom") {
      const name = getCustomFontName().trim();
      if (name) root.style.setProperty("--font-family-custom", name);
      else root.style.removeProperty("--font-family-custom");
    } else {
      root.style.removeProperty("--font-family-custom");
    }
  }
  try {
    localStorage.setItem(FONT_FAMILY_KEY, font);
  } catch {
    /* private mode / no storage */
  }
}

export function applyMonoFontFamily(font: MonoFontFamily): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  if (font === DEFAULT_MONO_FONT_FAMILY) {
    root.removeAttribute("data-mono-font-family");
    root.style.removeProperty("--font-family-mono-custom");
  } else {
    root.setAttribute("data-mono-font-family", font);
    if (font === "custom") {
      const name = getCustomMonoFontName().trim();
      if (name) root.style.setProperty("--font-family-mono-custom", name);
      else root.style.removeProperty("--font-family-mono-custom");
    } else {
      root.style.removeProperty("--font-family-mono-custom");
    }
  }
  try {
    localStorage.setItem(MONO_FONT_FAMILY_KEY, font);
  } catch {
    /* private mode / no storage */
  }
}

export function initFontFamily(): void {
  applyFontFamily(getFontFamily());
  applyMonoFontFamily(getMonoFontFamily());
}
