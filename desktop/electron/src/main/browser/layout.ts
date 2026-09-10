import type { BrowserLayoutRect } from "../../shared/ipc.js";

// DOM geometry is in CSS pixels; native View bounds are in device-independent
// pixels. Display scale (devicePixelRatio) must not be applied a second time.
export function browserLayoutInDIP(rect: BrowserLayoutRect | null, zoom: number): BrowserLayoutRect | null {
  if (rect === null) return null;
  if (!Number.isFinite(zoom) || zoom <= 0) throw new Error("invalid renderer zoom factor");
  return { x: rect.x * zoom, y: rect.y * zoom, width: rect.width * zoom, height: rect.height * zoom };
}
