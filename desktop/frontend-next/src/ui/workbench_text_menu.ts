import { copyText } from "./CopyButton";

export type TextMenuAction = "undo" | "redo" | "cut" | "copy" | "paste" | "selectAll";

export interface TextMenuSession {
  anchor: HTMLElement;
  text: string;
  editable: boolean;
  selected: boolean;
  canUndo: boolean;
  canRedo: boolean;
  valid(): boolean;
  focus(selection?: boolean): void;
  run(action: TextMenuAction, current: () => boolean): void | Promise<void>;
}

export type OfferTextMenu = (event: MouseEvent | KeyboardEvent, session: TextMenuSession) => void;

export function textMenuKey(event: KeyboardEvent): boolean {
  return event.key === "ContextMenu" || (event.key === "F10" && event.shiftKey && !event.altKey && !event.ctrlKey && !event.metaKey);
}

export function readerTextSession(anchor: HTMLElement): TextMenuSession | null {
  const selection = document.getSelection();
  if (!selection || selection.isCollapsed || !anchor.contains(selection.anchorNode) || !anchor.contains(selection.focusNode)) return null;
  const ranges = Array.from({ length: selection.rangeCount }, (_, i) => selection.getRangeAt(i).cloneRange());
  if (ranges.some((range) => !anchor.contains(range.startContainer) || !anchor.contains(range.endContainer))) return null;
  if (!anchor.hasAttribute("tabindex")) anchor.tabIndex = -1;
  const text = selection.toString();
  const valid = () => anchor.isConnected && ranges.every((range) => anchor.contains(range.startContainer) && anchor.contains(range.endContainer));
  const focus = (restore = true) => {
    if (!valid()) return;
    anchor.focus({ preventScroll: true });
    if (!restore) return;
    selection.removeAllRanges();
    for (const range of ranges) selection.addRange(range);
  };
  return {
    anchor, text, editable: false, selected: true, canUndo: false, canRedo: false, valid, focus,
    async run(action, current) {
      if (!current()) return;
      if (action === "copy") {
        await copyText(text);
        if (current()) focus();
      } else if (action === "selectAll") {
        focus(false);
        const range = document.createRange();
        range.selectNodeContents(anchor);
        selection.removeAllRanges();
        selection.addRange(range);
      }
    },
  };
}
