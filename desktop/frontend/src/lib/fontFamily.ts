export const FONT_FAMILIES = ["system", "yahei", "pingfang", "noto"] as const;

export type FontFamily = (typeof FONT_FAMILIES)[number];

export const DEFAULT_FONT_FAMILY: FontFamily = "system";

const FONT_FAMILY_KEY = "reasonix-font-family";

export function isFontFamily(value: unknown): value is FontFamily {
  return typeof value === "string" && (FONT_FAMILIES as readonly string[]).includes(value);
}

export function getFontFamily(): FontFamily {
  const stored = typeof localStorage !== "undefined" ? localStorage.getItem(FONT_FAMILY_KEY) : null;
  return isFontFamily(stored) ? stored : DEFAULT_FONT_FAMILY;
}

export function applyFontFamily(font: FontFamily): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  if (font === DEFAULT_FONT_FAMILY) root.removeAttribute("data-font-family");
  else root.setAttribute("data-font-family", font);
  try {
    localStorage.setItem(FONT_FAMILY_KEY, font);
  } catch {
    /* private mode / no storage */
  }
}

export function initFontFamily(): void {
  applyFontFamily(getFontFamily());
}
