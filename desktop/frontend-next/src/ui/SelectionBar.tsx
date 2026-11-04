import type { ReactNode } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { pushNote } from "../state/toasts";
import { copyText } from "./CopyButton";

// floor is the top edge of whatever scrolls the selection, which the bar must not cross.
export interface SelectionAt { x: number; top: number; bottom: number; floor: number; text: string }

// Room the bar needs above a selection; short of it the bar drops below.
const ROOM = 40;

/** The small bar that floats under a selection, so a passage reaches the
 *  conversation or the clipboard without the context menu. Its buttons must
 *  prevent mousedown: focus moving away would clear the selection they act on. */
export function SelectionBar({ at, note, children }: { at: SelectionAt; note?: string; children: ReactNode }) {
  const below = at.top - at.floor < ROOM;
  return createPortal(
    <div className="sel-tools" role="toolbar" aria-label={t("chat.selected_text")} data-below={below ? "" : undefined}
      style={below ? { left: at.x, top: at.bottom + 6 } : { left: at.x, top: at.top - 6 }}>
      {children}
      {note && <small>{note}</small>}
    </div>,
    document.body,
  );
}

export function copySelection(text: string) {
  void copyText(text).then(() => pushNote(t("common.copied")), () => pushNote(t("common.copy_failed_try_again")));
}
