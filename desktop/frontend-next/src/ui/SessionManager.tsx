import { useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import type { HubPort, TreeSession, TreeWorkspace } from "../port/hub";
import { clearDraftForSession } from "./drafts";
import { pushNote } from "../state/toasts";
import { StudioIcon } from "./StudioIcon";
import "../styles/session-batch.css";
import "../styles/session-manager.css";

interface Row extends TreeSession {
  wsName: string;
  wsRoot: string;
}

type Scope = "all" | "live" | "pinned" | "archived";

export function SessionManager({ hub, tree, liveIds, reload, onOpen, onError, onCloseManager }: {
  hub: HubPort;
  tree: TreeWorkspace[];
  liveIds: (ids: string[]) => string[];
  reload: () => Promise<void>;
  onOpen: (req: { sessionPath: string }) => Promise<void>;
  onError: (e: unknown) => void;
  onCloseManager: () => void;
}) {
  const [needle, setNeedle] = useState("");
  const [scope, setScope] = useState<Scope>("all");
  const [wsRoot, setWsRoot] = useState<string>("all");
  const [selection, setSelection] = useState<Set<string>>(new Set());
  const [confirm, setConfirm] = useState(false);
  const [pending, setPending] = useState(false);

  const rows = useMemo<Row[]>(() => {
    const out: Row[] = [];
    for (const ws of tree) {
      for (const s of ws.sessions) {
        out.push({ ...s, wsName: ws.name, wsRoot: ws.root });
      }
    }
    return out;
  }, [tree]);

  const filtered = useMemo<Row[]>(() => {
    const q = needle.trim().toLowerCase();
    return rows.filter((s) => {
      if (wsRoot !== "all" && s.wsRoot !== wsRoot) return false;
      if (scope === "live" && !s.runtimeId) return false;
      if (scope === "pinned" && !s.pinned) return false;
      if (scope === "archived" && !s.archived) return false;
      if (q && !(s.title || s.name).toLowerCase().includes(q)) return false;
      return true;
    });
  }, [rows, needle, scope, wsRoot]);

  // A session is deletable only when no pane is actively driving it. A stale
  // runtimeId on a finished conversation must not keep it from being selected.
  const isRunning = (s: Row) => !!s.runtimeId && liveIds([s.runtimeId]).length > 0;
  const selectable = useMemo(() => filtered.filter((s) => !isRunning(s)), [filtered, liveIds]);
  const allSelected = selectable.length > 0 && selectable.every((s) => selection.has(s.path));
  const someSelected = selectable.some((s) => selection.has(s.path));
  const runningSelected = filtered.some((s) => selection.has(s.path) && isRunning(s));

  const toggle = (path: string) =>
    setSelection((cur) => {
      const next = new Set(cur);
      if (next.has(path)) next.delete(path); else next.add(path);
      return next;
    });

  const toggleAll = () =>
    setSelection((cur) => {
      const next = new Set(cur);
      if (allSelected) for (const s of selectable) next.delete(s.path);
      else for (const s of selectable) next.add(s.path);
      return next;
    });

  const runDelete = async () => {
    setConfirm(false);
    setPending(true);
    const deletable = filtered.filter((s) => selection.has(s.path) && !isRunning(s)).map((s) => s.path);
    try {
      await hub.removeSessions(deletable);
      for (const p of deletable) clearDraftForSession("", p);
      pushNote(t("已删除 {n} 个会话", { n: deletable.length }));
      setSelection(new Set());
      await reload();
    } catch (e) {
      onError(e);
    } finally {
      setPending(false);
    }
  };

  // The confirm step names every conversation about to be erased and the
  // workspace each one belongs to, so a person can see the full blast radius
  // before the delete is irreversible.
  const deletableRows = filtered.filter((s) => selection.has(s.path) && !isRunning(s));
  const deletableCount = deletableRows.length;

  return createPortal(
    <div className="sm-backdrop" role="presentation" data-action-click="session-manager.close"
      onClick={(ev) => { if (ev.target === ev.currentTarget && !pending) onCloseManager(); }}
      onKeyDown={(ev) => { if (ev.key === "Escape" && !pending) onCloseManager(); }}
      data-action-keydown="session-manager.close">
      <div className="sm-panel" role="dialog" aria-modal="true" aria-label={t("会话管理")}>
        <header className="sm-head">
          <h2>{t("会话管理")}</h2>
          <span className="sm-count">{t("共 {n} 个会话", { n: rows.length })}</span>
          <button className="sm-x" aria-label={t("关闭")} data-action="session-manager.close" onClick={onCloseManager}>
            <StudioIcon name="close" />
          </button>
        </header>

        <div className="sm-filters">
          <label className="sm-search">
            <StudioIcon name="search" />
            <input type="search" value={needle} placeholder={t("按名称搜索…")} data-action="session-manager.search"
              onChange={(e) => setNeedle(e.target.value)} aria-label={t("搜索会话")} />
          </label>
          <div className="sm-seg" role="group" aria-label={t("范围")}>
            {(["all", "live", "pinned", "archived"] as Scope[]).map((v) => (
              <button key={v} data-action="session-manager.scope" data-value={v} aria-pressed={scope === v}
                onClick={() => setScope(v)}>{t(v === "all" ? "全部" : v === "live" ? "进行中" : v === "pinned" ? "置顶" : "归档")}</button>
            ))}
          </div>
          <select className="sm-ws" value={wsRoot} aria-label={t("项目")} data-action="session-manager.project"
            onChange={(e) => setWsRoot(e.target.value)}>
            <option value="all">{t("所有项目")}</option>
            {tree.map((ws) => <option key={ws.root} value={ws.root}>{ws.name}</option>)}
          </select>
        </div>

        <div className="sm-toolbar">
          <label className="sm-selall">
            <input type="checkbox" checked={allSelected} ref={(el) => { if (el) el.indeterminate = !allSelected && someSelected; }}
              disabled={selectable.length === 0 || pending} aria-label={t(allSelected ? "取消全选" : "全选")}
              data-action="session-manager.select-all" onChange={toggleAll} />
            <span>{allSelected ? t("取消全选") : t("全选")}</span>
          </label>
          <span className="sm-status" role="status">{t("已选 {n} 个", { n: selection.size })}</span>
          <span className="sm-spacer" />
          <button className="sm-danger" data-action="session-manager.delete" disabled={deletableCount === 0 || pending || confirm}
            onClick={() => setConfirm(true)}>
            <StudioIcon name="trash" /><span>{t("删除所选")}</span>
          </button>
        </div>

        <div className="sm-list" role="list" aria-busy={pending}>
          {filtered.length === 0 && <p className="sm-empty">{t("没有匹配的会话")}</p>}
          {filtered.map((s) => {
            const checked = selection.has(s.path);
            const running = isRunning(s);
            return (
              <div key={s.path} className={"sm-row" + (checked ? " is-sel" : "") + (running ? " is-running" : "")} role="listitem">
                <input type="checkbox" className="sm-check" checked={checked} disabled={running || pending}
                  aria-label={t("选择会话：{name}", { name: s.title || s.name })} data-action="session-manager.toggle"
                  onChange={() => toggle(s.path)} />
                <button className="sm-open" data-action="session-manager.open" onClick={() => void onOpen({ sessionPath: s.path }).then(onCloseManager).catch(onError)}>
                  <span className="sm-title">{s.title || s.name}</span>
                  <span className="sm-meta">
                    <span className="sm-ws">{s.wsName}</span>
                    {s.turns != null && <span>{t("{n} 轮", { n: s.turns })}</span>}
                    {s.lastActiveAt != null && <span>{new Date(s.lastActiveAt).toLocaleDateString()}</span>}
                  </span>
                </button>
                <span className="sm-badges">
                  {running && <span className="sm-badge running">{t("进行中")}</span>}
                  {s.pinned && <span className="sm-badge">{t("置顶")}</span>}
                  {s.archived && <span className="sm-badge">{t("归档")}</span>}
                  {s.unread && <span className="sm-badge unread">{t("未读")}</span>}
                </span>
              </div>
            );
          })}
        </div>

        {runningSelected && <p className="sm-note">{t("进行中的会话需先停止，不会被删除")}</p>}

        {confirm && createPortal(
          <div className="wsconfirm" role="alertdialog" aria-label={t("删除 {n} 个会话？", { n: deletableCount })}
            data-action-keydown="layer.dismiss" onKeyDown={(ev) => { if (ev.key === "Escape") { ev.stopPropagation(); setConfirm(false); } }}>
            <div className="wsconfirm-t">
              <span className="q">{t("删除 {n} 个会话？", { n: deletableCount })}</span>
              <ul className="sm-confirm-list" aria-label={t("将删除以下会话")}>
                {deletableRows.map((s) => (
                  <li key={s.path}>
                    <span className="sm-confirm-name">{s.title || s.name}</span>
                    <span className="sm-confirm-ws">{s.wsName}</span>
                  </li>
                ))}
              </ul>
              <span className="h">{t("连同其记录一并删除，且无法恢复")}</span>
            </div>
            <div className="wsconfirm-a">
              <button data-action="layer.dismiss" onClick={() => setConfirm(false)}>{t("取消")}</button>
              <button data-action="session-manager.delete-confirm" data-danger="" disabled={pending} onClick={() => void runDelete()}>{t("删除")}</button>
            </div>
          </div>, document.body,
        )}
      </div>
    </div>, document.body,
  );
}
