export type DisplayMode = "standard" | "compact";

const DISPLAY_MODE_KEY = "reasonix-display-mode";
const DISPLAY_MODE_EVENT = "reasonix:display-mode";

export function getDisplayMode(): DisplayMode {
  if (typeof localStorage === "undefined") return "standard";
  const stored = localStorage.getItem(DISPLAY_MODE_KEY);
  if (stored === "standard" || stored === "compact") return stored;
  if (stored === "minimal") return "compact";
  return "standard";
}

export function setDisplayMode(mode: DisplayMode): void {
  localStorage.setItem(DISPLAY_MODE_KEY, mode);
  window.dispatchEvent(new CustomEvent(DISPLAY_MODE_EVENT, { detail: mode }));
}

/** Adopts the toml-persisted mode at boot so config is the source of truth across machines. */
export function hydrateDisplayMode(mode: string | undefined): void {
  const next: DisplayMode | undefined = mode === "standard" || mode === "compact" ? mode : mode === "minimal" ? "compact" : undefined;
  if (!next) return;
  if (next === getDisplayMode()) return;
  setDisplayMode(next);
}

export function onDisplayModeChange(cb: (mode: DisplayMode) => void): () => void {
  const handler = (e: Event) => cb((e as CustomEvent).detail as DisplayMode);
  window.addEventListener(DISPLAY_MODE_EVENT, handler);
  return () => window.removeEventListener(DISPLAY_MODE_EVENT, handler);
}
