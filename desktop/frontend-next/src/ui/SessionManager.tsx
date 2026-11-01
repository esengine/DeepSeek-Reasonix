import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { HubPort, TreeSession, TreeWorkspace } from "../port/hub";
import { hidesLegacySessions, onHidesLegacySessionsChange } from "../state/prefs";
import { clearDraftForSession } from "./drafts";
import { listenAction } from "./listen";
import { SessionAge } from "./SessionAge";
import { TREE_PAGE } from "./treewindow";
import { useModalFocus } from "./useModalFocus";
import "../styles/session-manager.css";

// A conversation list you can act on without hunting for each row's menu. The
// sidebar keeps its own in-place selection for the rows already on screen; this
// sheet is the cross-project answer to "delete a lot of them at once", and it
// reads one page at a time for the same reason the recent list does — a history
// is unbounded and a dialog should not try to draw all of it.
//
// Local sessions only: a remote pane's transcript lives on another machine and
// its removal is that host's business, which is why the entry says 本机.

type Phase = "idle" | "confirm" | "archive" | "restore" | "delete";

const label = (session: TreeSession) => session.title || session.name;

export function SessionManager({ hub, liveIds, onClosePanes, onArchive, onChanged, onClose, onError }: {
  hub: HubPort;
  liveIds: (ids: string[]) => string[];
  onClosePanes: (ids: string[]) => Promise<void>;
  onArchive: (path: string, archived: boolean, runtimeId?: string) => Promise<void>;
  onChanged: () => Promise<void>;
  onClose: () => void;
  onError: (e: unknown) => void;
}) {
  const sheet = useRef<HTMLDivElement>(null);
  useModalFocus(sheet);
  const hideLegacy = useSyncExternalStore(onHidesLegacySessionsChange, hidesLegacySessions, hidesLegacySessions);
  const [draft, setDraft] = useState("");
  const [view, setView] = useState({ q: "", offset: 0 });
  const [tree, setTree] = useState<TreeWorkspace[] | null>(null);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [phase, setPhase] = useState<Phase>("idle");
  const deleteButton = useRef<HTMLButtonElement>(null);
  const cancelButton = useRef<HTMLButtonElement>(null);
  // The delete button is disabled while the question is open, so it cannot take
  // focus until the phase change has been committed — asking in the handler
  // moves focus to nothing when the question unmounts.
  const refocus = useRef(false);
  // A second click can arrive before the phase change is painted, and this one
  // must not become two kernel mutations for one gesture.
  const working = useRef(false);
  const pending = phase === "archive" || phase === "restore" || phase === "delete";

  useEffect(() => {
    const timer = setTimeout(() => setView((current) => current.q === draft.trim() ? current : { q: draft.trim(), offset: 0 }), 250);
    return () => clearTimeout(timer);
  }, [draft]);

  useEffect(() => { sheet.current?.querySelector<HTMLInputElement>("input")?.focus(); }, []);

  // A page or a word is a different list; keeping checkboxes on rows the reader
  // can no longer see is how a batch deletes something nobody chose.
  useEffect(() => { setSelected(new Set()); setPhase("idle"); }, [view.q, view.offset]);

  useEffect(() => {
    let stale = false;
    setBusy(true);
    setError("");
    const window = { order: "recent" as const, scope: "" as const, limit: TREE_PAGE, offset: view.offset, q: view.q, hideLegacy };
    void hub.tree(window).then((next) => {
      if (stale) return;
      if (next?.some((ws) => ws.recentOffset !== view.offset)) throw new Error(t("此运行环境不支持会话管理，请更新 Reasonix"));
      if (!next?.some((ws) => ws.sessions.length > 0) && view.offset > 0) {
        // The last page can empty out under a delete; step back instead of
        // showing a blank sheet that still says it is at page four.
        setView((current) => ({ ...current, offset: Math.max(0, current.offset - TREE_PAGE) }));
        return;
      }
      setTree(next ?? []);
    }).catch((e: unknown) => { if (!stale) setError(reason(e)); })
      .finally(() => { if (!stale) setBusy(false); });
    return () => { stale = true; };
  }, [hub, view, hideLegacy, retry]);

  const rows = (tree ?? []).flatMap((ws) => ws.sessions.map((session) => ({ ws, session })))
    .sort((a, b) => (b.session.lastActiveAt ?? 0) - (a.session.lastActiveAt ?? 0) || (a.session.path < b.session.path ? -1 : a.session.path > b.session.path ? 1 : 0));
  const remaining = (tree ?? []).reduce((n, ws) => n + (ws.remaining ?? 0), 0);
  const picked = rows.filter(({ session }) => selected.has(session.path));
  const running = picked.some(({ session }) => !!session.runtimeId && liveIds([session.runtimeId]).length > 0);
  // While a page is being read the rows on screen belong to the page before it,
  // so acting on them would act on a list the reader has already left.
  const blocked = busy || pending || running || picked.length === 0;
  const all = rows.length > 0 && rows.every(({ session }) => selected.has(session.path));
  const restore = picked.length > 0 && picked.every(({ session }) => session.archived);

  const toggle = (path: string) => {
    setPhase("idle");
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(path)) next.delete(path); else next.add(path);
      return next;
    });
  };

  const cancel = () => {
    refocus.current = true;
    setPhase("idle");
  };

  useEffect(() => {
    if (phase === "confirm") { cancelButton.current?.focus(); return; }
    if (phase !== "idle" || !refocus.current) return;
    refocus.current = false;
    deleteButton.current?.focus();
  }, [phase]);

  useEffect(() => {
    const escape = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || pending) return;
      onClose();
    };
    return listenAction(window, "keydown", { action: "layer.dismiss", listener: escape as EventListener });
  }, [onClose, pending]);

  const run = async (action: "archive" | "restore" | "delete") => {
    if (blocked || working.current) return;
    working.current = true;
    setPhase(action);
    const failed = new Set<string>();
    let firstError: unknown;
    for (const { session } of picked) {
      try {
        // Rechecked per row, not once at confirmation: a turn can start in the
        // gap, and closing its pane would stop work a delete never announced.
        if (session.runtimeId && liveIds([session.runtimeId]).length > 0) throw new Error(t("所选会话正在运行，停止后才能操作"));
        if (action === "delete") {
          // Same order as the row's own delete: an idle pane's runtime holds the
          // transcript's lease until it is down, and the kernel refuses otherwise.
          if (session.runtimeId) await onClosePanes([session.runtimeId]);
          await hub.removeSession(session.path);
          clearDraftForSession("", session.path);
        } else {
          await onArchive(session.path, action === "archive", session.runtimeId);
        }
      } catch (e) {
        failed.add(session.path);
        firstError ??= e;
      }
    }
    setSelected(failed);
    refocus.current = true;
    setPhase("idle");
    if (action === "delete") {
      try { await onChanged(); } catch (e) { firstError ??= e; }
    }
    setRetry((n) => n + 1);
    working.current = false;
    if (firstError) onError(firstError);
  };

  return createPortal(
    <div className="pickveil" role="dialog" aria-modal="true" aria-label={t("管理会话")} ref={sheet}>
      <div className="pickcard session-manager">
        <h2>{t("管理会话")}</h2>
        <p className="session-manager-description">{t("多选本机会话，一次归档或删除；删除前会再确认。")}</p>
        <div className="pickpath">
          <input data-action="workspace.search" type="search" aria-label={t("搜索会话 / 项目")} placeholder={t("搜索会话 / 项目")}
            value={draft} disabled={pending} onChange={(event) => setDraft(event.target.value)} />
        </div>
        <div className="session-manager-tools">
          <span role="status">{t("已选 {n} 个会话", { n: picked.length })}</span>
          <button data-action="session.selection" data-value={all ? "clear" : "all"} disabled={busy || pending || rows.length === 0}
            aria-label={t(all ? "取消本页选择" : "全选本页")}
            onClick={() => {
              if (all) { setSelected(new Set()); return; }
              setSelected(new Set(rows.map(({ session }) => session.path)));
            }}>{t(all ? "取消本页选择" : "全选本页")}</button>
          <button data-action="session.batch-archive" data-value={restore ? "restore" : "archive"} disabled={blocked || phase === "confirm"}
            aria-label={t(restore ? "取消所选会话归档" : "归档所选会话")}
            onClick={() => void run(restore ? "restore" : "archive")}>{t(restore ? "取消归档" : "归档")}</button>
          <button ref={deleteButton} data-action="session.batch-delete" disabled={blocked || phase === "confirm"}
            aria-label={t("删除所选会话")}
            onClick={() => setPhase("confirm")}>{t("删除")}</button>
        </div>
        {running && <p className="session-manager-note">{t("所选会话正在运行，停止后才能操作")}</p>}
        <div className="picklist session-manager-list" aria-busy={busy} data-busy={busy ? "" : undefined}>
          {!busy && !error && !rows.length && <p className="pickempty">{t("本页没有匹配的会话")}</p>}
          {busy && <p className="picknote" role="status">{t("正在读取会话…")}</p>}
          {!busy && !error && rows.map(({ ws, session }) => (
            <label key={session.path} className="session-manager-row" data-archived={session.archived ? "" : undefined}
              data-live={session.runtimeId && liveIds([session.runtimeId]).length > 0 ? "" : undefined}>
              <input type="checkbox" checked={selected.has(session.path)} disabled={busy || pending}
                data-action-click="session.selection" data-action-change="session.selection" data-target={session.path}
                aria-label={t("选择会话：{name}", { name: label(session) })}
                onClick={(event) => event.stopPropagation()} onChange={() => toggle(session.path)} />
              <span className="session-manager-text">
                <b>{label(session)}</b>
                <small>{ws.name}{session.archived ? ` · ${t("已归档")}` : ""}{session.runtimeId && liveIds([session.runtimeId]).length > 0 ? ` · ${t("运行中")}` : ""}</small>
              </span>
              <SessionAge at={session.lastActiveAt} />
            </label>
          ))}
          {error && <><p className="picknote" role="alert">{error}</p>
            <button className="pickgo" data-action="session.manage-retry" onClick={() => setRetry((n) => n + 1)}>{t("重试")}</button></>}
        </div>
        {phase === "confirm" && (
          <div className="wsconfirm session-manager-confirm" role="alertdialog" aria-label={t("删除 {n} 个会话？", { n: picked.length })}
            data-action-keydown="layer.dismiss" onKeyDown={(event) => { if (event.key === "Escape") { event.stopPropagation(); cancel(); } }}>
            <div className="wsconfirm-t">
              <span className="q">{t("删除 {n} 个会话？", { n: picked.length })}</span>
              <span className="session-manager-names">{picked.map(({ session }) => label(session)).join("、")}</span>
              <span className="h">{t("连同其记录一并删除")}</span>
            </div>
            <div className="wsconfirm-a">
              <button ref={cancelButton} data-action="layer.dismiss" onClick={cancel}>{t("取消")}</button>
              <button data-action="session.batch-delete" data-danger="" onClick={() => void run("delete")}>{t("删除")}</button>
            </div>
          </div>
        )}
        {pending && <p className="session-manager-note" role="status">{t("正在处理所选会话…")}</p>}
        <div className="pickact">
          <button data-action="session.manage-page" data-value="previous" disabled={busy || pending || view.offset === 0}
            onClick={() => setView({ ...view, offset: Math.max(0, view.offset - TREE_PAGE) })}>{t("上一页")}</button>
          <span className="recent-page">{t("第 {n} 页", { n: view.offset / TREE_PAGE + 1 })}</span>
          <button data-action="session.manage-page" data-value="next" disabled={busy || pending || !!error || remaining === 0}
            onClick={() => setView({ ...view, offset: view.offset + TREE_PAGE })}>{t("下一页")}</button>
          <button data-action="layer.dismiss" disabled={pending} onClick={onClose}>{t("关闭")}</button>
        </div>
      </div>
    </div>, document.body,
  );
}
