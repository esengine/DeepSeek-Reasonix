// theme.ts manages the appearance override. The stylesheet is dark by default
// and follows the OS via prefers-color-scheme; this lets the user force a theme
// by setting data-theme on <html>, or "auto" to remove it and follow the OS.
// The choice persists in localStorage and is applied on load.

export type Theme = "auto" | "light" | "dark";

const KEY = "reasonix-theme";

function normalizeTheme(value: unknown): Theme | null {
  if (typeof value === "object" && value !== null) {
    return normalizeTheme((value as { mode?: unknown }).mode);
  }
  if (typeof value !== "string") return null;
  switch (value) {
    case "auto":
      return "auto";
    case "light":
    case "focus":
    case "forest":
      return "light";
    case "dark":
    case "midnight":
    case "contrast":
      return "dark";
    default:
      return null;
  }
}

export function getTheme(): Theme {
  const v = typeof localStorage !== "undefined" ? localStorage.getItem(KEY) : null;
  if (!v) return "auto";
  try {
    const parsed = JSON.parse(v) as unknown;
    return normalizeTheme(parsed) ?? normalizeTheme(v) ?? "auto";
  } catch {
    return normalizeTheme(v) ?? "auto";
  }
}

export function applyTheme(theme: Theme): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  root.removeAttribute("data-theme-mode");
  root.removeAttribute("data-theme-scheme");
  if (theme === "auto") root.removeAttribute("data-theme");
  else root.setAttribute("data-theme", theme);
  try {
    localStorage.setItem(KEY, theme);
  } catch {
    /* private mode / no storage — the in-DOM attribute still applies */
  }
}

// initTheme applies the saved choice once at startup (before React renders).
export function initTheme(): void {
  applyTheme(getTheme());
}
