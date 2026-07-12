import type { BrowserElementBox, BrowserKeyEvent } from "./bridge";

export interface BrowserViewportSize {
  width: number;
  height: number;
}

export interface BrowserPoint {
  x: number;
  y: number;
}

export interface BrowserCanvasBox extends BrowserPoint {
  width: number;
  height: number;
}

export interface BrowserFloatingPosition extends BrowserPoint {
  width: number;
  maxHeight: number;
}

export function browserViewportSize(width: number, height: number): BrowserViewportSize {
  return {
    width: Math.max(320, Math.min(4096, Math.round(width))),
    height: Math.max(240, Math.min(4096, Math.round(height))),
  };
}

export function browserPointFromClient(
  clientX: number,
  clientY: number,
  rect: Pick<DOMRect, "left" | "top" | "width" | "height">,
  viewport: BrowserViewportSize,
): BrowserPoint {
  if (rect.width <= 0 || rect.height <= 0) return { x: 0, y: 0 };
  return {
    x: Math.max(0, Math.min(viewport.width, ((clientX - rect.left) / rect.width) * viewport.width)),
    y: Math.max(0, Math.min(viewport.height, ((clientY - rect.top) / rect.height) * viewport.height)),
  };
}

export function browserElementBoxToCanvas(
  box: BrowserElementBox,
  viewport: BrowserViewportSize,
  canvas: BrowserViewportSize,
): BrowserCanvasBox {
  const scaleX = viewport.width > 0 ? canvas.width / viewport.width : 1;
  const scaleY = viewport.height > 0 ? canvas.height / viewport.height : 1;
  return {
    x: box.x * scaleX,
    y: box.y * scaleY,
    width: box.width * scaleX,
    height: box.height * scaleY,
  };
}

export function browserFloatingPosition(
  anchor: BrowserCanvasBox,
  canvas: BrowserViewportSize,
  preferredWidth = 340,
  preferredHeight = 520,
): BrowserFloatingPosition {
  const edge = 8;
  const gap = 12;
  const availableWidth = Math.max(1, canvas.width - edge * 2);
  const availableHeight = Math.max(1, canvas.height - edge * 2);
  const width = Math.min(Math.max(220, Math.min(preferredWidth, availableWidth)), availableWidth);
  const maxHeight = Math.min(Math.max(180, Math.min(preferredHeight, availableHeight)), availableHeight);
  const clampX = (value: number) => Math.max(edge, Math.min(value, canvas.width - width - edge));
  const clampY = (value: number) => Math.max(edge, Math.min(value, canvas.height - maxHeight - edge));
  const right = anchor.x + anchor.width + gap;
  const left = anchor.x - width - gap;
  if (right + width <= canvas.width - edge) return { x: right, y: clampY(anchor.y), width, maxHeight };
  if (left >= edge) return { x: left, y: clampY(anchor.y), width, maxHeight };
  const below = anchor.y + anchor.height + gap;
  if (below + maxHeight <= canvas.height - edge) return { x: clampX(anchor.x), y: below, width, maxHeight };
  const above = anchor.y - maxHeight - gap;
  if (above >= edge) return { x: clampX(anchor.x), y: above, width, maxHeight };
  return { x: clampX(right), y: clampY(anchor.y), width, maxHeight };
}

export function stepBrowserCSSNumericValue(
  property: string,
  rawValue: string,
  direction: 1 | -1,
  accelerated = false,
): string | null {
  const match = rawValue.trim().match(/^(-?(?:\d+\.?\d*|\.\d+))([a-z%]*)$/i);
  if (!match) return null;
  const current = Number(match[1]);
  if (!Number.isFinite(current)) return null;
  const baseStep = property === "opacity" ? 0.1 : property === "font-weight" ? 100 : 1;
  const nextStep = baseStep * (accelerated ? 10 : 1) * direction;
  let next = current + nextStep;
  if (property === "opacity") next = Math.max(0, Math.min(1, next));
  if (property === "font-weight") next = Math.max(1, Math.min(1000, next));
  const precision = property === "opacity" ? 2 : Math.min(4, (match[1].split(".")[1] || "").length);
  const formatted = Number(next.toFixed(precision)).toString();
  return `${formatted}${match[2]}`;
}

export function browserModifiers(event: Pick<KeyboardEvent | MouseEvent, "altKey" | "ctrlKey" | "metaKey" | "shiftKey">): number {
  return (event.altKey ? 1 : 0)
    | (event.ctrlKey ? 2 : 0)
    | (event.metaKey ? 4 : 0)
    | (event.shiftKey ? 8 : 0);
}

export function browserMouseButton(button: number): "none" | "left" | "middle" | "right" {
  if (button === 0) return "left";
  if (button === 1) return "middle";
  if (button === 2) return "right";
  return "none";
}

export function browserKeyEvent(event: KeyboardEvent, type: BrowserKeyEvent["type"]): BrowserKeyEvent {
  const text = type === "keyDown" && event.key.length === 1 && !event.ctrlKey && !event.metaKey ? event.key : undefined;
  return {
    type,
    key: event.key,
    code: event.code,
    text,
    unmodifiedText: text,
    windowsVirtualKeyCode: event.keyCode || undefined,
    nativeVirtualKeyCode: event.keyCode || undefined,
    modifiers: browserModifiers(event),
    autoRepeat: event.repeat,
    isKeypad: event.location === KeyboardEvent.DOM_KEY_LOCATION_NUMPAD,
  };
}