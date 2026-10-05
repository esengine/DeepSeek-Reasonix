import type { ViewRect } from "../port/host";

// A native view is drawn above the whole page, so the page has to say where it
// may go and when something of its own must be seen over it. Both answers are
// read from layout and computed style: what is on screen, not what an element
// is called.

type Stack = (x: number, y: number) => Element[];

// The probe grid: one probe per STEP screen pixels, at most MAX_PROBES a side.
// A layer narrower than the spacing in both directions can fall between them.
const STEP = 64;
const MAX_PROBES = 9;
// How far a layer may reach into the slot's edge and still be drawn under the
// page: a column gutter's grip straddles that edge by design.
const EDGE = 8;
const INVISIBLE = 0.02;

/** visibleBox is the part of el the window actually shows: its box cut to the
 *  viewport and to every ancestor that clips, scrollbars excluded. */
export function visibleBox(el: Element): ViewRect | null {
  const box = el.getBoundingClientRect();
  let left = Math.max(box.left, 0);
  let top = Math.max(box.top, 0);
  let right = Math.min(box.right, window.innerWidth);
  let bottom = Math.min(box.bottom, window.innerHeight);
  for (let up = el.parentElement; up && up !== document.documentElement; up = up.parentElement) {
    const style = getComputedStyle(up);
    if (style.overflowX === "visible" && style.overflowY === "visible") continue;
    const outer = up.getBoundingClientRect();
    const scale = up.offsetWidth > 0 ? outer.width / up.offsetWidth : 1;
    const x = outer.left + up.clientLeft * scale;
    const y = outer.top + up.clientTop * scale;
    left = Math.max(left, x);
    top = Math.max(top, y);
    right = Math.min(right, x + up.clientWidth * scale);
    bottom = Math.min(bottom, y + up.clientHeight * scale);
  }
  const width = right - left;
  const height = bottom - top;
  return width >= 1 && height >= 1 ? { x: left, y: top, width, height } : null;
}

function probes(start: number, length: number): number[] {
  const count = Math.min(MAX_PROBES, Math.max(3, Math.ceil(length / STEP) + 1));
  const inset = Math.min(EDGE, length / 4);
  const span = length - inset * 2;
  return Array.from({ length: count }, (_, i) => start + inset + (span * i) / (count - 1));
}

function alpha(color: string): number {
  if (!color || color === "transparent") return 0;
  const slash = /\/\s*([\d.]+)(%?)\s*\)$/.exec(color);
  if (slash) return slash[2] ? Number(slash[1]) / 100 : Number(slash[1]);
  const rgba = /^rgba\((?:[^,]+,){3}\s*([\d.]+)\s*\)$/.exec(color);
  return rgba ? Number(rgba[1]) : 1;
}

const REPLACED = new Set(["IMG", "VIDEO", "CANVAS", "IFRAME", "EMBED", "OBJECT", "INPUT", "TEXTAREA", "SELECT", "PICTURE"]);

function drawn(value: string | undefined): boolean {
  return !!value && value !== "none";
}

function fills(style: CSSStyleDeclaration): boolean {
  return alpha(style.backgroundColor) > INVISIBLE || drawn(style.backgroundImage) || drawn(style.backdropFilter);
}

function pseudoFills(el: Element): boolean {
  for (const which of ["::before", "::after"]) {
    const style = getComputedStyle(el, which);
    if (style.content && style.content !== "none" && style.content !== "normal" && fills(style)) return true;
  }
  return false;
}

function onBorder(el: Element, style: CSSStyleDeclaration, x: number, y: number): boolean {
  const box = el.getBoundingClientRect();
  const scale = el instanceof HTMLElement && el.offsetWidth > 0 ? box.width / el.offsetWidth : 1;
  const side = (width: string, color: string) => (alpha(color) > INVISIBLE ? parseFloat(width) * scale || 0 : 0);
  return (
    x < box.left + side(style.borderLeftWidth, style.borderLeftColor) ||
    x > box.right - side(style.borderRightWidth, style.borderRightColor) ||
    y < box.top + side(style.borderTopWidth, style.borderTopColor) ||
    y > box.bottom - side(style.borderBottomWidth, style.borderBottomColor)
  );
}

function textAt(el: Element, x: number, y: number): boolean {
  for (const node of el.childNodes) {
    if (node.nodeType !== Node.TEXT_NODE || !node.textContent?.trim()) continue;
    const range = document.createRange();
    range.selectNodeContents(node);
    for (const r of range.getClientRects()) if (x >= r.left && x <= r.right && y >= r.top && y <= r.bottom) return true;
  }
  return false;
}

/** paints says whether el puts anything the eye can see at (x, y): a layer at
 *  opacity 0, or a transparent box that only catches clicks, does not. */
export function paints(el: Element, x: number, y: number): boolean {
  let opacity = 1;
  for (let up: Element | null = el; up; up = up.parentElement) opacity *= Number(getComputedStyle(up).opacity || 1);
  if (opacity <= INVISIBLE) return false;
  const style = getComputedStyle(el);
  if (style.visibility !== "visible") return false;
  if (REPLACED.has(el.tagName) || el instanceof SVGElement) return true;
  return fills(style) || pseudoFills(el) || onBorder(el, style, x, y) || textAt(el, x, y);
}

/** occluded says whether anything the page draws is seen over box, the part of
 *  slot the native view would cover. The slot's own content, its ancestors and
 *  layers that paint nothing there do not count. */
export function occluded(slot: Element, box: ViewRect, stack: Stack = (x, y) => document.elementsFromPoint(x, y)): boolean {
  for (const x of probes(box.x, box.width)) {
    for (const y of probes(box.y, box.height)) {
      for (const el of stack(x, y)) {
        if (el === slot || slot.contains(el)) break;
        if (el.contains(slot)) continue;
        if (paints(el, x, y)) return true;
      }
    }
  }
  return false;
}
