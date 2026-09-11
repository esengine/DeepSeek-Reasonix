const EXPLICIT_SCHEME = /^(?:[a-z][a-z0-9+.-]*:\/\/|(?:about|blob|data|file|view-source):)/i;
const LOOPBACK_HOST = /^(?:localhost|127(?:\.\d{1,3}){3}|\[::1\]|0\.0\.0\.0)(?::\d+)?(?:[/?#]|$)/i;

/** Turns address-bar input into a loadable URL; bare hosts become https, loopback hosts http. */
export function normalizeAddress(input: string): string | null {
  const value = input.trim();
  if (!value) return null;
  if (EXPLICIT_SCHEME.test(value)) return value;
  return `${LOOPBACK_HOST.test(value) ? "http" : "https"}://${value}`;
}

const ZOOM_PRESETS = [0.25, 0.33, 0.5, 0.67, 0.75, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2, 2.5, 3, 4, 5];
const ZOOM_EPSILON = 0.005;

/** Next Chromium zoom preset in `direction`; 0 resets to 100%. */
export function zoomStep(current: number, direction: -1 | 0 | 1): number {
  if (direction === 0) return 1;
  if (direction > 0) return ZOOM_PRESETS.find((preset) => preset > current + ZOOM_EPSILON) ?? ZOOM_PRESETS[ZOOM_PRESETS.length - 1];
  return [...ZOOM_PRESETS].reverse().find((preset) => preset < current - ZOOM_EPSILON) ?? ZOOM_PRESETS[0];
}

export function zoomPercent(factor: number): number {
  return Math.round(factor * 100);
}
