import type { BrowserKeyEvent } from "./bridge";

export interface BrowserViewportSize {
  width: number;
  height: number;
}

export interface BrowserPoint {
  x: number;
  y: number;
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