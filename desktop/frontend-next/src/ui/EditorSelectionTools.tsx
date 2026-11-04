import { createPortal } from "react-dom";
import { t } from "../i18n";
import { pushNote } from "../state/toasts";
import { copyText } from "./CopyButton";
import { StudioIcon } from "./StudioIcon";

export interface SelectionTools { x: number; y: number; from: number; to: number; text: string }

/** The small bar that floats under a selection in the editor, so a passage reaches
 *  the conversation without the context menu. */
export function EditorSelectionTools({ tools, onQuote, onDone }: { tools: SelectionTools; onQuote: (text: string) => void; onDone: () => void }) {
  const lines = tools.from === tools.to ? t("workbench.line_n", { n: tools.from }) : t("workbench.lines_a_b", { a: tools.from, b: tools.to });
  return createPortal(
    <div className="editor-tools" role="toolbar" aria-label={t("workbench.text_actions")} style={{ left: tools.x, top: tools.y }}>
      <button type="button" data-action-click="workbench.quote-selection" data-action-mousedown="workbench.quote-selection" onMouseDown={(e) => e.preventDefault()} onClick={() => { onQuote(tools.text); onDone(); }}>
        <StudioIcon name="chat" /><span>{t("workbench.add_to_conversation")}</span><small>{lines}</small>
      </button>
      <button type="button" data-action-click="workbench.text-copy" data-action-mousedown="workbench.text-copy" onMouseDown={(e) => e.preventDefault()} onClick={() => {
        void copyText(tools.text).then(() => pushNote(t("common.copied")), () => pushNote(t("common.copy_failed_try_again")));
        onDone();
      }}>
        <StudioIcon name="copy" /><span>{t("common.copy")}</span>
      </button>
    </div>,
    document.body,
  );
}
