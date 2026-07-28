import { normalizeSelectedText } from "./selectedTextContext";
import { browserReferencePath, isBrowserReferencePath } from "./browserUrl";

export interface BrowserAnnotationRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface BrowserAnnotationPayload {
  url: string;
  note: string;
  text?: string;
  selector?: string;
  tagName?: string;
  rect?: BrowserAnnotationRect;
  screenshotDataUrl?: string;
}

export function buildBrowserAnnotationText(payload: BrowserAnnotationPayload): string {
  const lines: string[] = [`URL: ${payload.url.trim()}`];
  if (payload.selector) lines.push(`Selector: ${payload.selector}`);
  if (payload.tagName) lines.push(`Element: ${payload.tagName.toLowerCase()}`);
  if (payload.rect) {
    const { x, y, width, height } = payload.rect;
    lines.push(`Region: ${Math.round(x)},${Math.round(y)} ${Math.round(width)}x${Math.round(height)}`);
  }
  const note = payload.note.trim();
  if (note) {
    lines.push("", "Requested change:", note);
  }
  const selected = normalizeSelectedText(payload.text ?? "").text;
  if (selected) {
    lines.push("", "Selected content:", selected);
  }
  return lines.join("\n").trim();
}

export function toBrowserSelectedTextReference(payload: BrowserAnnotationPayload, id: string): {
  id: string;
  text: string;
  path: string;
} {
  return {
    id,
    text: buildBrowserAnnotationText(payload),
    path: browserReferencePath(payload.url, payload.selector),
  };
}

export function rectFromPoints(a: { x: number; y: number }, b: { x: number; y: number }): BrowserAnnotationRect {
  return {
    x: Math.min(a.x, b.x),
    y: Math.min(a.y, b.y),
    width: Math.abs(a.x - b.x),
    height: Math.abs(a.y - b.y),
  };
}

export function cssPathForElement(el: Element): string {
  if (el.id) return `#${CSS.escape(el.id)}`;
  const parts: string[] = [];
  let current: Element | null = el;
  while (current && current.nodeType === 1 && parts.length < 6) {
    const tag = current.tagName.toLowerCase();
    if (tag === "html" || tag === "body") {
      parts.unshift(tag);
      break;
    }
    const parent: Element | null = current.parentElement;
    if (!parent) {
      parts.unshift(tag);
      break;
    }
    const siblings = Array.from(parent.children).filter((child) => child.tagName === current!.tagName);
    if (siblings.length === 1) {
      parts.unshift(tag);
    } else {
      const index = siblings.indexOf(current) + 1;
      parts.unshift(`${tag}:nth-of-type(${index})`);
    }
    current = parent;
  }
  return parts.join(" > ");
}

export { isBrowserReferencePath };
