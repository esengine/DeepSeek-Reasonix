export type TerminalSelectionPoint = { left: number; top: number };

export type TerminalSelectionOperation<T> = {
  generation: number;
  terminal: T;
};

export type TerminalSelectionSnapshot<T> = TerminalSelectionOperation<T> & {
  selectionRevision: number;
};

export type TerminalSelectionLifecycle<T> = {
  activate: (terminal: T) => TerminalSelectionOperation<T>;
  capture: () => TerminalSelectionOperation<T> | null;
  captureSelection: () => TerminalSelectionSnapshot<T> | null;
  deactivate: (terminal: T) => void;
  isCurrent: (operation: TerminalSelectionOperation<T>) => boolean;
  isCurrentSelection: (snapshot: TerminalSelectionSnapshot<T>) => boolean;
  noteSelectionChange: () => void;
};

type RectLike = Pick<DOMRect, "left" | "top" | "right" | "bottom" | "width" | "height">;

function pickBottomMost(paints: readonly RectLike[]): RectLike {
  let anchor = paints[0];
  for (const rect of paints.slice(1)) {
    if (rect.bottom > anchor.bottom) {
      anchor = rect;
      continue;
    }
    if (rect.bottom === anchor.bottom && rect.right > anchor.right) anchor = rect;
  }
  return anchor;
}

// xterm paints the active selection as absolutely positioned divs inside
// `.xterm-selection`. Anchor the floating action to the bottom-most real paint
// row so it sits next to the selected text, not on the panel's far edge.
export function terminalSelectionPointFromHost(
  host: HTMLElement,
  toolbarWidth = 160,
): TerminalSelectionPoint | null {
  const hostRect = host.getBoundingClientRect();
  const paints = Array.from(host.querySelectorAll(".xterm-selection div"))
    .map((node) => node.getBoundingClientRect())
    .filter((rect) => rect.width > 0 || rect.height > 0);
  if (paints.length === 0) return null;
  // xterm occasionally leaves near-full-width spacer rows in the selection
  // layer; those would park the toolbar on the terminal's right edge.
  const usable = paints.filter((rect) => rect.width < hostRect.width * 0.92);
  const anchor = pickBottomMost(usable.length > 0 ? usable : paints);
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

// Clipboard reads and writes can settle after the active terminal changes.
// Keep the terminal identity and generation together so stale completions can
// never clear or paste into the replacement session.
export function createTerminalSelectionLifecycle<T>(): TerminalSelectionLifecycle<T> {
  let generation = 0;
  let selectionRevision = 0;
  let terminal: T | null = null;
  return {
    activate(next) {
      generation += 1;
      selectionRevision += 1;
      terminal = next;
      return { generation, terminal: next };
    },
    capture() {
      return terminal === null ? null : { generation, terminal };
    },
    captureSelection() {
      return terminal === null ? null : { generation, selectionRevision, terminal };
    },
    deactivate(target) {
      if (terminal !== target) return;
      generation += 1;
      selectionRevision += 1;
      terminal = null;
    },
    isCurrent(operation) {
      return operation.generation === generation && operation.terminal === terminal;
    },
    isCurrentSelection(snapshot) {
      return snapshot.generation === generation
        && snapshot.selectionRevision === selectionRevision
        && snapshot.terminal === terminal;
    },
    noteSelectionChange() {
      if (terminal !== null) selectionRevision += 1;
    },
  };
}

// Pure decision for Ctrl+C / Ctrl+Shift+C / Cmd+C: a live selection
// copies and swallows the chord, otherwise the key keeps flowing to the PTY
// (Ctrl+C then stays SIGINT). Preserve xterm's selection byte-for-byte,
// including whitespace, line endings, and escape-looking text. Meta+C mirrors
// the macOS copy convention.
export function handleTerminalCopyKey(input: {
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  altKey: boolean;
  hasSelection: () => boolean;
  getSelection: () => string;
}): { intercepted: boolean; text: string } {
  const lowerKey = input.key.toLowerCase();
  if (lowerKey !== "c") return { intercepted: false, text: "" };
  const isCtrlC = input.ctrlKey && !input.altKey;
  const isMetaC = input.metaKey && !input.altKey;
  if (!isCtrlC && !isMetaC) return { intercepted: false, text: "" };
  if (!input.hasSelection()) return { intercepted: false, text: "" };
  return { intercepted: true, text: input.getSelection() };
}

// Async clipboard reads need the webview's Clipboard API permission; the Wails
// runtime bridge is the fallback, mirroring writeClipboardText's ladder.
export async function readTerminalClipboardText(): Promise<string> {
  try {
    if (typeof navigator !== "undefined" && navigator.clipboard?.readText) {
      return await navigator.clipboard.readText();
    }
  } catch {
    // Permission denied or unavailable — try the bridge.
  }
  try {
    if (typeof window !== "undefined" && window.runtime?.ClipboardGetText) {
      return await window.runtime.ClipboardGetText();
    }
  } catch {
    // Bridge missing or failed.
  }
  return "";
}
