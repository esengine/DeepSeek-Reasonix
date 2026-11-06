import { createPortal } from "react-dom";
import { t } from "../i18n";
import { pinToViewport } from "./place";

/** Asked before a terminal with a command running is closed: ending the shell ends the command. */
export function TerminalCloseAsk({ anchor, onConfirm, onCancel }: { anchor: HTMLElement; onConfirm: () => void; onCancel: () => void }) {
  const r = anchor.getBoundingClientRect();
  return createPortal(
    <div className="workbench-close-ask" role="alertdialog" aria-label={t("terminal.close_a_running_one")}
      data-action-keydown="layer.dismiss" ref={(el) => { if (el) pinToViewport(el, r.left, r.bottom + 4); }}
      onKeyDown={(e) => { if (e.key === "Escape") { e.stopPropagation(); onCancel(); } }}>
      <p>{t("terminal.close_a_running_one")}</p>
      <div>
        <button type="button" autoFocus data-action="terminal.close-confirm" onClick={onConfirm}>{t("terminal.close_and_end")}</button>
        <button type="button" data-action="terminal.close-cancel" onClick={onCancel}>{t("common.cancel")}</button>
      </div>
    </div>,
    document.body,
  );
}
