export const TEXT_SIZES = ["small", "default", "large", "xlarge", "xxlarge"] as const;

export type TextSize = (typeof TEXT_SIZES)[number];

export const DEFAULT_TEXT_SIZE: TextSize = "default";

const TEXT_SIZE_KEY = "reasonix-text-size";

export function isTextSize(value: unknown): value is TextSize {
  return typeof value === "string" && (TEXT_SIZES as readonly string[]).includes(value);
}

export function nextTextSize(current: TextSize, delta: -1 | 1): TextSize {
  const index = TEXT_SIZES.indexOf(current);
  const nextIndex = Math.min(TEXT_SIZES.length - 1, Math.max(0, index + delta));
  return TEXT_SIZES[nextIndex];
}

export function getTextSize(): TextSize {
  const stored = typeof localStorage !== "undefined" ? localStorage.getItem(TEXT_SIZE_KEY) : null;
  return isTextSize(stored) ? stored : DEFAULT_TEXT_SIZE;
}

export function applyTextSize(size: TextSize): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  if (size === DEFAULT_TEXT_SIZE) root.removeAttribute("data-text-size");
  else root.setAttribute("data-text-size", size);
  try {
    localStorage.setItem(TEXT_SIZE_KEY, size);
  } catch {
    /* private mode / no storage - the in-DOM attribute still applies */
  }
}

export function initTextSize(): void {
  applyTextSize(getTextSize());
}
