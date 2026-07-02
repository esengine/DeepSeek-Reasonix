/**
 * DPI zoom scale service.
 *
 * Uses the WebView2 ZoomFactor (Go side) for reliable, layout-safe zooming.
 * The frontend only saves the user's preference and lets the Go binding persist
 * it to desktop-zoom.json; on next startup main.go reads the file and applies
 * ZoomFactor to the WebView2 window options.
 *
 * Range: 0.50 – 2.00 (50% – 200%), step 0.05.
 */

export const MIN_ZOOM = 0.5;
export const MAX_ZOOM = 2.0;
export const ZOOM_STEP = 0.05;

export type ZoomLevel = number; // 0.5 – 2.0

export const DEFAULT_ZOOM: ZoomLevel = 1.0;

const ZOOM_KEY = "reasonix-zoom-restart";

// ─── helpers ────────────────────────────────────────────────────────

/** Snap a number to the nearest valid step within [MIN, MAX]. */
export function snapZoom(value: number): ZoomLevel {
  const clamped = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, value));
  const steps = Math.round((clamped - MIN_ZOOM) / ZOOM_STEP);
  return parseFloat((MIN_ZOOM + steps * ZOOM_STEP).toFixed(2));
}

/** Convert a zoom value (0.5-2.0) to a percentage integer (50-200). */
export function zoomToPercent(zoom: ZoomLevel): number {
  return Math.round(zoom * 100);
}

/** Convert a percentage integer (50-200) back to a zoom value (0.5-2.0). */
export function percentToZoom(pct: number): ZoomLevel {
  return snapZoom(pct / 100);
}

function readZoom(fallback: ZoomLevel): ZoomLevel {
  const stored = typeof localStorage !== "undefined" ? localStorage.getItem(ZOOM_KEY) : null;
  if (stored === null) return fallback;
  const parsed = parseFloat(stored);
  if (isNaN(parsed)) return fallback;
  return snapZoom(parsed);
}

function writeZoom(value: ZoomLevel): void {
  try {
    localStorage.setItem(ZOOM_KEY, String(value));
  } catch {
    /* private mode / no storage */
  }
}

// ─── public API ─────────────────────────────────────────────────────

/** Read the saved zoom level that will be applied on next restart. */
export function getRestartZoom(): ZoomLevel {
  return readZoom(DEFAULT_ZOOM);
}

/**
 * Save a zoom factor that will be picked up by Go on next startup and
 * applied as the WebView2 `ZoomFactor`. Takes effect after the app is
 * restarted (no layout issues — ZoomFactor is engine-level).
 */
export function saveRestartZoom(userZoom: ZoomLevel): void {
  writeZoom(snapZoom(userZoom));
}

/**
 * Init: no-op for zoom (the Go-side ZoomFactor is applied at WebView2
 * creation time).
 */
export function initDpiScale(): void {
  /* zoom is handled entirely by the Go side (ZoomFactor) */
}
