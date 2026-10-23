import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { useModalFocus } from "./useModalFocus";

/** Closing a file tab that holds edits the disk does not have. The question is
 *  asked here rather than in the tab strip because the cost — a project built
 *  from a half-written file — is not something a hover can carry. Saving is the
 *  emphasised answer; dropping the draft and staying are both one click away. */
export function UnsavedCloseDialog({ busy, onDiscard, onSave, onCancel }: {
  busy: boolean;
  onDiscard: () => void;
  onSave: () => void;
  onCancel: () => void;
}) {
  const sheet = useRef<HTMLDivElement>(null);
  useModalFocus(sheet);
  const cancel = useRef<HTMLButtonElement>(null);
  useEffect(() => { cancel.current?.focus(); }, []);
  return createPortal(
    <div
      ref={sheet}
      className="pickveil"
      role="dialog"
      aria-modal="true"
      aria-labelledby="unsaved-t"
      data-action-keydown="layer.dismiss"
      onKeyDown={(e) => {
        if (e.key !== "Escape") return;
        e.stopPropagation();
        onCancel();
      }}
    >
      <div className="pickcard unsaved-close">
        <h2 id="unsaved-t">{t("确认未保存就关闭它吗？")}</h2>
        <p className="picknote">{t("这可能引起你的项目出错。")}</p>
        <div className="pickact">
          <button data-action="workbench.close-discard" disabled={busy} onClick={onDiscard}>{t("不保存")}</button>
          <button className="pickgo" data-action="workbench.close-save" disabled={busy} onClick={onSave}>{t("保存")}</button>
          <button ref={cancel} data-action="workbench.close-cancel" disabled={busy} onClick={onCancel}>{t("取消")}</button>
        </div>
      </div>
    </div>,
    document.body,
  );
}
