import { useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { pinToViewport } from "./place";

/** Asked before a tab with unsaved text closes: save it, drop it, or keep the tab. */
export function WorkbenchCloseAsk({ name, anchor, onSave, onDrop, onCancel }: {
  name: string; anchor: HTMLElement; onSave: () => Promise<boolean>; onDrop: () => void; onCancel: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const r = anchor.getBoundingClientRect();
  return createPortal(
    <div className="workbench-close-ask" role="alertdialog" aria-label={t("workbench.name_is_not_saved", { name })}
      data-action-keydown="layer.dismiss" ref={(el) => { if (el) pinToViewport(el, r.left, r.bottom + 4); }}
      onKeyDown={(e) => { if (e.key === "Escape") { e.stopPropagation(); onCancel(); } }}>
      <p>{t("workbench.name_is_not_saved", { name })}</p>
      <div>
        <button type="button" autoFocus data-action="workbench.close-save" disabled={busy}
          onClick={async () => { setBusy(true); if (!(await onSave())) setBusy(false); }}>{t("common.save")}</button>
        <button type="button" data-action="workbench.close-drop" disabled={busy} onClick={onDrop}>{t("workbench.dont_save")}</button>
        <button type="button" data-action="workbench.close-cancel" disabled={busy} onClick={onCancel}>{t("common.cancel")}</button>
      </div>
    </div>,
    document.body,
  );
}
