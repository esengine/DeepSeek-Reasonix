import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import type { RuntimeView, TreeSession } from "../port/hub";
import { onSessionQuickActionsChange, showsSessionQuickActions } from "../state/sessionQuick";
import { useDismiss } from "./dismiss";
import { pinToViewport } from "./place";
import { SessionAge } from "./SessionAge";
import { SessionArchiveAction, SessionPinAction } from "./SessionRowActions";
import { StudioIcon } from "./StudioIcon";
import { UnreadDot } from "./UnreadMark";
import { workspaceMenuKeys } from "./WorkspaceOrder";
import { asksDelete } from "./keys";
import type { SessionMutation } from "./useSessionMutation";

export function RemoteSessionRow({ host, target, session, held, active, busy, pending, live, onOpen, onFocus, onWrite, onDelete }: {
  host: string;
  target: string;
  session: TreeSession;
  held?: RuntimeView;
  active: string;
  busy: boolean;
  pending?: SessionMutation;
  live: boolean;
  onOpen: () => void;
  onFocus: (id: string) => void;
  onWrite: (kind: "pin" | "archive", row: HTMLElement | null) => void;
  onDelete: (row: HTMLElement | null) => void;
}) {
  const [at, setAt] = useState<{ x: number; y: number } | null>(null);
  const row = useRef<HTMLDivElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const quick = useSyncExternalStore(onSessionQuickActionsChange, showsSessionQuickActions, showsSessionQuickActions);
  const title = session.title || session.name;
  const close = () => { setAt(null); row.current?.focus(); };
  useDismiss(!!at, row, close, menu);
  useLayoutEffect(() => { if (at && menu.current) pinToViewport(menu.current, at.x, at.y, 12); }, [at]);
  useEffect(() => { if (at) menu.current?.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus(); }, [at]);
  const write = (kind: "pin" | "archive") => {
    if (pending || busy || kind === "archive" && live) return;
    setAt(null);
    onWrite(kind, row.current);
  };
  return <div ref={row} className="sessrow sessrow-remote" role="treeitem" aria-level={2} aria-label={title} tabIndex={0}
    aria-selected={held?.id === active} data-on={held?.id === active ? "" : undefined}
    data-live={held ? "" : undefined} data-unread={session.unread ? "" : undefined}
    data-busy={pending || busy ? "" : undefined} data-target={target}
    data-action-click="session.open" data-action-contextmenu="session.menu"
    data-action-keydown={["session.open", "session.menu", "session.delete"]}
    onClick={() => { if (!pending || pending === "pin") held ? onFocus(held.id) : onOpen(); }}
    onContextMenu={(event) => { event.preventDefault(); event.stopPropagation(); if (!pending && !busy) setAt({ x: event.clientX, y: event.clientY }); }}
    onKeyDown={(event) => {
      if (event.target !== event.currentTarget) return;
      if (pending || busy) return;
      if (event.key === "Enter" || event.key === " ") { event.preventDefault(); event.stopPropagation(); held ? onFocus(held.id) : onOpen(); }
      if (event.key === "ContextMenu" || event.shiftKey && event.key === "F10") {
        event.preventDefault(); const rect = event.currentTarget.getBoundingClientRect(); setAt({ x: rect.right, y: rect.top });
      }
      if (asksDelete(event)) { event.preventDefault(); onDelete(row.current); }
    }}>
    <i className="pip" />
    <SessionPinAction target={target} title={title} pinned={!!session.pinned} enabled={quick} disabled={!!pending || busy} onPin={() => write("pin")} />
    <span className="sesstitle" title={title}><span>{title}</span>{session.isolated && <em className="sessarch" title={session.workspaceRoot}>{t("隔离")}</em>}{session.archived && <em className="sessarch">{t("已归档")}</em>}</span>
    {session.unread && <UnreadDot />}
    <SessionAge at={session.lastActiveAt} />
    {session.legacy && <span className="sesslegacy" title={t("由 Reasonix 1.x 创建：Studio 只读打开，继续对话会另存为新会话")}
      aria-label={t("由 Reasonix 1.x 创建：Studio 只读打开，继续对话会另存为新会话")}>{t("旧")}</span>}
    {quick && <SessionArchiveAction target={target} title={title} archived={!!session.archived} disabled={!!pending || busy || live} onArchive={() => write("archive")} />}
    {at && createPortal(<div ref={menu} className="session-pop" role="menu" aria-label={t("会话操作")}
      data-action-keydown="session.menu" onKeyDown={workspaceMenuKeys} onClick={(event) => event.stopPropagation()}>
      <div className="session-pop-head"><b>{title}</b><small>{host}</small></div>
      <div className="session-pop-group">
        <button role="menuitem" data-action="session.pin" data-target={target} disabled={!!pending || busy} onClick={() => { close(); write("pin"); }}>
          <StudioIcon name="pin" /><span>{t(session.pinned ? "取消置顶" : "置顶会话")}</span>
        </button>
        <button role="menuitem" data-action="session.archive" data-target={target} data-value={session.archived ? "restore" : "archive"}
          disabled={!!pending || busy || live} onClick={() => { close(); write("archive"); }}>
          <StudioIcon name="archive" /><span>{t(session.archived ? "取消归档" : "归档会话")}</span>
        </button>
        <button role="menuitem" className="danger" data-action="session.delete" data-target={target} disabled={!!pending || busy} onClick={() => { close(); onDelete(row.current); }}>
          <StudioIcon name="trash" /><span>{t("删除会话")}</span>
        </button>
      </div>
    </div>, document.body)}
  </div>;
}
