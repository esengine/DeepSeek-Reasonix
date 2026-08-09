import { stripTerminalControlSequences } from "./terminalOutput";

export type TerminalSelectionPoint = {
  left: number;
  top: number;
};

type RectLike = Pick<DOMRect, "left" | "top" | "right" | "bottom" | "width" | "height">;

function isUsablePaint(rect: RectLike, hostWidth: number): boolean {
  if (rect.width <= 0 && rect.height <= 0) return false;
  // xterm occasionally leaves near-full-width spacer rows in `.xterm-selection`.
  // Anchoring to those parks the toolbar on the terminal's right edge.
  if (hostWidth > 0 && rect.width >= hostWidth * 0.92) return false;
  return true;
}

function pickAnchorPaint(paints: readonly RectLike[]): RectLike {
  let anchor = paints[0];
  for (const rect of paints.slice(1)) {
    if (rect.bottom > anchor.bottom) {
      anchor = rect;
      continue;
    }
    if (rect.bottom === anchor.bottom && rect.right > anchor.right) {
      anchor = rect;
    }
  }
  return anchor;
}

// xterm paints the active selection as absolutely positioned divs inside
// `.xterm-selection`. Prefer the bottom-most real paint row and place the
// toolbar just past its end so "Add to Chat" sits next to the selected text
// inside the terminal, not on the panel's far right edge.
export function terminalSelectionPointFromHost(
  host: HTMLElement,
  toolbarWidth = 160,
): TerminalSelectionPoint | null {
  const hostRect = host.getBoundingClientRect();
  const paints = Array.from(host.querySelectorAll(".xterm-selection div"))
    .map((node) => node.getBoundingClientRect())
    .filter((rect) => rect.width > 0 || rect.height > 0);
  if (paints.length === 0) return null;

  const usable = paints.filter((rect) => isUsablePaint(rect, hostRect.width));
  const anchor = pickAnchorPaint(usable.length > 0 ? usable : paints);
  const maxLeft = Math.max(hostRect.left + 8, hostRect.right - toolbarWidth - 8);
  const left = Math.min(Math.max(hostRect.left + 8, anchor.right + 4), maxLeft);
  const top = Math.min(
    Math.max(hostRect.top + 8, anchor.bottom + 6),
    Math.max(hostRect.top + 8, hostRect.bottom - 40),
  );
  return { left, top };
}

export function clampTerminalSelectionPointToHost(
  point: TerminalSelectionPoint,
  host: HTMLElement,
  toolbarWidth = 160,
  toolbarHeight = 40,
): TerminalSelectionPoint {
  const hostRect = host.getBoundingClientRect();
  const maxLeft = Math.max(hostRect.left + 8, hostRect.right - toolbarWidth - 8);
  const maxTop = Math.max(hostRect.top + 8, hostRect.bottom - toolbarHeight - 8);
  return {
    left: Math.min(Math.max(hostRect.left + 8, point.left), maxLeft),
    top: Math.min(Math.max(hostRect.top + 8, point.top), maxTop),
  };
}

export function normalizeTerminalSelectionText(value: string): string {
  return stripTerminalControlSequences(value).trim();
}
