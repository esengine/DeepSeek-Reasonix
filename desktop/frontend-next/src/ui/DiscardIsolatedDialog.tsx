import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import type { IsolatedDiscardPreview } from "../port/isolation";
import { useModalFocus } from "./useModalFocus";

export function DiscardIsolatedDialog({ preview, busy, error, onConfirm, onClose }: {
  preview: IsolatedDiscardPreview; busy: boolean; error: string; onConfirm: () => void; onClose: () => void;
}) {
  const sheet = useRef<HTMLDivElement>(null);
  const cancel = useRef<HTMLButtonElement>(null);
  useModalFocus(sheet);
  useEffect(() => { if (!busy) cancel.current?.focus(); }, [busy]);
  return createPortal(
    <div ref={sheet} className="pickveil" role="dialog" aria-modal="true" aria-labelledby="discard-isolated-title"
      data-action-keydown="isolation.discard-cancel" onKeyDown={(e) => { if (e.key === "Escape") { e.stopPropagation(); if (!busy) onClose(); } }}>
      <form className="pickcard" data-action="isolation.discard-confirm" onSubmit={(e) => { e.preventDefault(); if (!busy) onConfirm(); }}>
        <h2 id="discard-isolated-title">{t("丢弃隔离成果 {id}？", { id: preview.id })}</h2>
        <p className="picknote">{t("将删除整个候选目录中的 {entries} 个条目，包括忽略文件及任务输出。原工作区不变，删除无法撤销。", { entries: preview.entries })}</p>
        <p className="picknote isolation-discard-path">{preview.candidateRoot}</p>
        <p className="picknote">{t("确认仅对应此预览；候选发生变化时会拒绝删除，需要重新预览。")}</p>
        {error && <p className="why" role="alert">{error}</p>}
        <div className="pickact">
          <button ref={cancel} type="button" data-action="isolation.discard-cancel" disabled={busy} onClick={onClose}>{t("取消")}</button>
          <button type="submit" className="pickgo" data-danger="" data-action="isolation.discard-confirm" disabled={busy}>{t("确认丢弃")}</button>
        </div>
      </form>
    </div>, document.body,
  );
}
