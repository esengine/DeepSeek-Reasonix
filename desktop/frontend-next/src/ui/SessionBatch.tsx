import { useEffect, useRef, useState } from "react";
import { current, t } from "../i18n";
import type { HubPort, TreeSession } from "../port/hub";
import { clearDraftForSession } from "./drafts";
import "../styles/session-batch.css";

export function SessionBatch({ hub, selection, selected, displayed, onSelect, onExit, onBusy, onClose, onArchive, liveIds, reload, onError }: {
  hub: HubPort;
  selection: Set<string> | null;
  selected: TreeSession[];
  displayed: TreeSession[];
  onSelect: (next: Set<string> | null) => void;
  onExit: () => void;
  onBusy: (busy: boolean) => void;
  onClose: (ids: string[]) => Promise<void>;
  onArchive: (path: string, archived: boolean, runtimeId?: string) => Promise<void>;
  liveIds: (ids: string[]) => string[];
  reload: () => Promise<void>;
  onError: (e: unknown) => void;
}) {
  const [phase, setPhase] = useState<"idle" | "confirm" | "archive" | "restore" | "delete">("idle");
  const entryButton = useRef<HTMLButtonElement>(null);
  const wasSelecting = useRef(selection !== null);
  const deleteButton = useRef<HTMLButtonElement>(null);
  const doneButton = useRef<HTMLButtonElement>(null);
  const pending = phase !== "idle" && phase !== "confirm";
  const running = liveIds(selected.flatMap((row) => row.runtimeId ? [row.runtimeId] : [])).length > 0;
  const blocked = pending || running || selected.length === 0;
  const all = displayed.length > 0 && displayed.every((row) => selection?.has(row.path));
  const restore = selected.length > 0 && selected.every((row) => row.archived);
  const cancel = () => { setPhase("idle"); onBusy(false); deleteButton.current?.focus(); };
  useEffect(() => {
    if (wasSelecting.current && selection === null) {
      if (entryButton.current?.disabled) onExit(); else entryButton.current?.focus();
    }
    wasSelecting.current = selection !== null;
  }, [selection !== null]);
  useEffect(() => {
    if (phase === "idle" && selection !== null) (deleteButton.current?.disabled ? doneButton.current : deleteButton.current)?.focus();
  }, [phase, selection !== null]);

  const run = async (action: "archive" | "restore" | "delete") => {
    if (blocked) return;
    setPhase(action);
    onBusy(true);
    const failed = new Set<string>();
    let firstError: unknown;
    let removed = false;
    for (const row of selected) {
      try {
        if (row.runtimeId && liveIds([row.runtimeId]).length > 0) throw new Error(t("所选会话正在运行，停止后才能操作"));
        if (action === "delete") {
          if (row.runtimeId) await onClose([row.runtimeId]);
          await hub.removeSession(row.path);
          clearDraftForSession("", row.path);
          removed = true;
        } else {
          await onArchive(row.path, action === "archive", row.runtimeId);
        }
      } catch (e) {
        failed.add(row.path);
        firstError ??= e;
      }
    }
    if (removed) {
      try { await reload(); } catch (e) { firstError ??= e; }
    }
    onSelect(failed);
    setPhase("idle");
    onBusy(false);
    if (firstError) onError(firstError);
    deleteButton.current?.focus();
  };

  if (selection === null) return (
    <div className="session-batch-entry">
      <button ref={entryButton} data-action="session.selection" data-value="start" disabled={displayed.length === 0} onClick={() => onSelect(new Set())}>{t("选择本机会话")}</button>
    </div>
  );
  return (
    <div className="session-batch" aria-busy={pending}>
      <div className="session-batch-head">
        <span role="status">{t("已选 {n} 个会话", { n: selected.length })}</span>
        <button ref={doneButton} data-action="session.selection" data-value="end" disabled={pending} onClick={() => { cancel(); onSelect(null); }}>{t("完成选择")}</button>
      </div>
      <div className="session-batch-actions">
        <button data-action="session.selection" data-value={all ? "clear" : "all"} disabled={phase !== "idle" || displayed.length === 0}
          aria-label={t(all ? "取消当前显示会话的选择" : "全选当前显示的会话")}
          onClick={() => {
            const next = new Set(selection);
            for (const row of displayed) { if (all) next.delete(row.path); else next.add(row.path); }
            onSelect(next);
          }}>{t(all ? "取消全选" : "全选可见")}</button>
        <button data-action="session.batch-archive" data-value={restore ? "restore" : "archive"} disabled={blocked || phase === "confirm"}
          aria-label={t(restore ? "取消所选会话归档" : "归档所选会话")} onClick={() => void run(restore ? "restore" : "archive")}>{t(restore ? "取消归档" : "归档")}</button>
        <button ref={deleteButton} data-action="session.batch-delete" disabled={blocked || phase === "confirm"} aria-label={t("删除所选会话")}
          onClick={() => { setPhase("confirm"); onBusy(true); }}>{t("删除")}</button>
      </div>
      {running && <p>{t("所选会话正在运行，停止后才能操作")}</p>}
      {pending && <p role="status">{t("正在处理所选会话…")}</p>}
      {phase === "confirm" && (
        <div className="wsconfirm" role="alertdialog" aria-label={t("删除 {n} 个会话？", { n: selected.length })}
          data-action-keydown="layer.dismiss" onKeyDown={(ev) => { if (ev.key === "Escape") { ev.stopPropagation(); cancel(); } }}>
          <div className="wsconfirm-t">
            <span className="q">{t("删除 {n} 个会话？", { n: selected.length })}</span>
            <span className="session-batch-names">{selected.map((row) => row.title || row.name).join(current() === "zh" ? "、" : ", ")}</span>
            <span className="h">{t("连同其记录一并删除")}</span>
          </div>
          <div className="wsconfirm-a">
            <button autoFocus data-action="layer.dismiss" onClick={cancel}>{t("取消")}</button>
            <button data-action="session.batch-delete" data-danger="" disabled={blocked} onClick={() => void run("delete")}>{t("删除")}</button>
          </div>
        </div>
      )}
    </div>
  );
}
