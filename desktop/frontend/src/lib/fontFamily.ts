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

/** Fired after the monospace font preference changes, so non-DOM renderers
 *  (e.g. the xterm.js integrated terminal) can pick up the new font. */
export const MONO_FONT_CHANGED_EVENT = "reasonix:mono-font-changed";

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
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent(MONO_FONT_CHANGED_EVENT));
  }
}

export function initFontFamily(): void {
  applyFontFamily(getFontFamily());
  applyMonoFontFamily(getMonoFontFamily());
}

/**
 * Monospace font stacks for the xterm.js integrated terminal, mirroring the
 * `--font-mono` presets in styles.css. The terminal renders into a canvas, so
 * CSS variables never reach it — resolve the stack here instead.
 */
const TERMINAL_MONO_STACKS: Record<Exclude<MonoFontFamily, "custom">, string> = {
  system: 'ui-monospace, "Cascadia Code", "JetBrains Mono", "Noto Sans Mono", "Liberation Mono", Consolas, monospace',
  cascadia: '"Cascadia Code", "Cascadia Mono", Consolas, "Liberation Mono", ui-monospace, monospace',
  jetbrains: '"JetBrains Mono", "Cascadia Code", "SF Mono", Consolas, ui-monospace, monospace',
  sfmono: '"SF Mono", SFMono-Regular, ui-monospace, Menlo, Monaco, "Cascadia Code", Consolas, monospace',
};

const TERMINAL_MONO_CUSTOM_FALLBACK = '"Cascadia Code", "SF Mono", Consolas, ui-monospace, monospace';

function quoteFontNameForStack(name: string): string {
  const trimmed = name.trim();
  if (!trimmed) return "";
  if (trimmed.startsWith('"') || trimmed.startsWith("'")) return trimmed;
  if (/\s/.test(trimmed)) return `"${trimmed}"`;
  return trimmed;
}

/** The CSS font stack the integrated terminal should use, honoring the
 *  user's monospace font preference (including a custom Nerd Font name). */
export function monoFontStackForTerminal(): string {
  const font = getMonoFontFamily();
  if (font === "custom") {
    const name = quoteFontNameForStack(getCustomMonoFontName());
    return name ? `${name}, ${TERMINAL_MONO_CUSTOM_FALLBACK}` : TERMINAL_MONO_STACKS.system;
  }
  return TERMINAL_MONO_STACKS[font];
}
