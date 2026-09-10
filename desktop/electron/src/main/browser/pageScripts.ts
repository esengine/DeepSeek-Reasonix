/// <reference lib="dom" />

// Page-side helpers for actions. Serialised with Function.prototype.toString
// like the snapshot walker: self-contained, no module-scope references.

export interface RefInput {
  key: string;
  snapshotId: string;
  docId: string;
  ref: string;
}

export interface ResolveInput extends RefInput {
  scroll: boolean;
}

export interface ResolvedElement {
  ok: true;
  x: number;
  y: number;
  width: number;
  height: number;
  tag: string;
  type: string;
  disabled: boolean;
  editable: boolean;
  frameOffsetKnown: boolean;
}

export interface ResolveFailure {
  ok: false;
  reason: string;
}

export type ResolveOutput = ResolvedElement | ResolveFailure;

interface Registry {
  docId: string;
  snapshotId: string;
  refs: Map<string, Element>;
}

// scroll:true brings the element to the viewport centre first; the returned
// rect is in top-document CSS pixels when every enclosing frame is
// same-origin, otherwise frameOffsetKnown is false and the caller refuses.
export function pageResolve(input: ResolveInput): ResolveOutput {
  const registry = (window as unknown as Record<string, unknown>)[input.key] as Registry | undefined;
  if (!registry || registry.docId !== input.docId || registry.snapshotId !== input.snapshotId) return { ok: false, reason: "stale" };
  const el = registry.refs.get(input.ref);
  if (!el) return { ok: false, reason: "stale" };
  if (!el.isConnected) return { ok: false, reason: "element is no longer in the document" };
  const style = window.getComputedStyle(el);
  if (style.display === "none" || style.visibility === "hidden") return { ok: false, reason: "element is hidden" };
  if (input.scroll) {
    try {
      el.scrollIntoView({ block: "center", inline: "center" });
    } catch {
      // Detached or non-scrollable ancestors; the rect check below decides.
    }
  }
  const rect = el.getBoundingClientRect();
  if (rect.width === 0 || rect.height === 0) return { ok: false, reason: "element has no size" };
  const cx = rect.left + rect.width / 2;
  const cy = rect.top + rect.height / 2;
  if (cx < 0 || cy < 0 || cx > window.innerWidth || cy > window.innerHeight) return { ok: false, reason: "element is outside the viewport" };
  if (el.tagName !== "OPTION") {
    const hit = document.elementFromPoint(cx, cy);
    if (hit && hit !== el && !el.contains(hit) && !hit.contains(el)) return { ok: false, reason: "element is covered by another element" };
  }
  let dx = 0;
  let dy = 0;
  let frameOffsetKnown = true;
  try {
    let win: Window = window;
    while (win !== win.top) {
      const owner = win.frameElement;
      if (!owner) {
        frameOffsetKnown = false;
        break;
      }
      const ownerRect = owner.getBoundingClientRect();
      dx += ownerRect.left + owner.clientLeft;
      dy += ownerRect.top + owner.clientTop;
      win = win.parent;
    }
  } catch {
    frameOffsetKnown = false;
  }
  const tag = el.tagName.toLowerCase();
  const type = tag === "input" ? (el as HTMLInputElement).type.toLowerCase() : "";
  const editable = tag === "textarea" || (el as HTMLElement).isContentEditable || (tag === "input" && !["button", "submit", "reset", "image", "checkbox", "radio", "file", "range", "color"].includes(type));
  return {
    ok: true,
    x: rect.left + dx,
    y: rect.top + dy,
    width: rect.width,
    height: rect.height,
    tag,
    type,
    disabled: (el as HTMLButtonElement).disabled === true,
    editable,
    frameOffsetKnown,
  };
}

export interface SelectInput extends RefInput {
  options: string[];
}

export type SelectOutput = { ok: true; selected: string[] } | ResolveFailure;

// The one action that synthesises DOM events: a <select> has no trustworthy
// pointer path (its popup is native), so options are toggled directly and
// input/change are dispatched the way Chromium itself does after a pick.
export function pageSelect(input: SelectInput): SelectOutput {
  const registry = (window as unknown as Record<string, unknown>)[input.key] as Registry | undefined;
  if (!registry || registry.docId !== input.docId || registry.snapshotId !== input.snapshotId) return { ok: false, reason: "stale" };
  const target = registry.refs.get(input.ref);
  if (!target) return { ok: false, reason: "stale" };
  const select = target.tagName === "SELECT" ? (target as HTMLSelectElement) : target.tagName === "OPTION" ? (target as HTMLOptionElement).closest("select") : null;
  if (!select) return { ok: false, reason: "element is not a <select>" };
  if (select.disabled) return { ok: false, reason: "select is disabled" };
  const wanted = input.options.length ? input.options : target.tagName === "OPTION" ? [(target as HTMLOptionElement).value] : [];
  const matches = [...select.options].filter((option) => wanted.includes(option.value) || wanted.includes(option.label) || wanted.includes(option.text.trim()));
  if (matches.length === 0) return { ok: false, reason: "no option matches the requested values" };
  const chosen = select.multiple ? matches : [matches[0]];
  for (const option of select.options) option.selected = chosen.includes(option);
  select.dispatchEvent(new Event("input", { bubbles: true }));
  select.dispatchEvent(new Event("change", { bubbles: true }));
  return { ok: true, selected: chosen.map((option) => option.value) };
}

export function pageFocus(input: RefInput): boolean {
  const registry = (window as unknown as Record<string, unknown>)[input.key] as Registry | undefined;
  if (!registry || registry.docId !== input.docId || registry.snapshotId !== input.snapshotId) return false;
  const target = registry.refs.get(input.ref);
  if (!target || !target.isConnected) return false;
  (target as HTMLElement).focus({ preventScroll: true });
  return document.activeElement === target;
}

export type LocateOutput = { ok: true; tag: string; type: string; path: string } | ResolveFailure;

// Locates a ref without visibility checks: uploads target hidden file inputs
// and the CSS path lets the DevTools protocol find the same node.
export function pageLocate(input: RefInput): LocateOutput {
  const registry = (window as unknown as Record<string, unknown>)[input.key] as Registry | undefined;
  if (!registry || registry.docId !== input.docId || registry.snapshotId !== input.snapshotId) return { ok: false, reason: "stale" };
  const el = registry.refs.get(input.ref);
  if (!el) return { ok: false, reason: "stale" };
  if (!el.isConnected) return { ok: false, reason: "element is no longer in the document" };
  const path: string[] = [];
  let current: Element | null = el;
  while (current && current !== document.documentElement) {
    const parent: Element | null = current.parentElement;
    if (!parent) break;
    const index = [...parent.children].indexOf(current) + 1;
    path.unshift(`${current.tagName.toLowerCase()}:nth-child(${index})`);
    current = parent;
  }
  const tag = el.tagName.toLowerCase();
  return { ok: true, tag, type: tag === "input" ? (el as HTMLInputElement).type.toLowerCase() : "", path: path.length ? `html > ${path.join(" > ")}` : "html" };
}

export interface IdentityInput {
  key: string;
  docId: string;
}

export function pageIdentity(input: IdentityInput): boolean {
  const registry = (window as unknown as Record<string, unknown>)[input.key] as Registry | undefined;
  return Boolean(registry) && registry?.docId === input.docId;
}

export const RESOLVE_SCRIPT_SOURCE = pageResolve.toString();
export const SELECT_SCRIPT_SOURCE = pageSelect.toString();
export const FOCUS_SCRIPT_SOURCE = pageFocus.toString();
export const IDENTITY_SCRIPT_SOURCE = pageIdentity.toString();
export const LOCATE_SCRIPT_SOURCE = pageLocate.toString();

export function scriptCall(source: string, input: unknown): string {
  return `(${source})(${JSON.stringify(input)})`;
}
