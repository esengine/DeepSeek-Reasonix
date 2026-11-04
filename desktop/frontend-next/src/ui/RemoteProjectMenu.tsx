import { useEffect, useLayoutEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { useDismiss } from "./dismiss";
import { pinToViewport } from "./place";
import { StudioIcon } from "./StudioIcon";
import { workspaceMenuKeys } from "./WorkspaceOrder";

export interface RemoteProjectMenuAnchor {
  key: string;
  trigger: HTMLElement;
  point?: { x: number; y: number };
}

export function RemoteProjectMenu({ name, root, host, defaultRoot, busy, anchor, onOpen, onClose, onRefresh, onRemove, onMove }: {
  name: string;
  root: string;
  host: string;
  defaultRoot?: string;
  busy: boolean;
  anchor?: RemoteProjectMenuAnchor;
  onOpen: (trigger: HTMLElement) => void;
  onClose: () => void;
  onRefresh: () => void;
  onRemove: () => void;
  // Steps this row's machine up or down the book; a direction with no neighbour is absent.
  onMove?: Partial<Record<-1 | 1, () => void>>;
}) {
  const open = !!anchor;
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const close = () => { onClose(); anchor?.trigger.focus(); };
  useDismiss(open, trigger, close, menu);
  useLayoutEffect(() => {
    const rect = anchor?.trigger.getBoundingClientRect();
    if (anchor && rect && menu.current) pinToViewport(menu.current, anchor.point?.x ?? rect.right + 8, anchor.point?.y ?? rect.top - 7, 12);
  }, [anchor]);
  useEffect(() => { if (anchor) menu.current?.querySelector<HTMLElement>("button:not(:disabled)")?.focus(); }, [anchor]);
  return <>
    <button ref={trigger} className="wsmore" data-action="workspace.menu" data-target={host + ":" + root}
      title={t("更多操作")} aria-label={t("项目操作：{name}", { name: name + " · " + host })}
      aria-haspopup="menu" aria-expanded={open} onClick={(event) => { event.stopPropagation(); if (open) close(); else onOpen(event.currentTarget); }}>
      <StudioIcon name="more" />
    </button>
    {open && createPortal(<div ref={menu} className="session-pop" role="menu" aria-label={t("项目操作")}
      data-action-keydown="workspace.menu" onKeyDown={workspaceMenuKeys} onClick={(event) => event.stopPropagation()}>
      <div className="session-pop-head"><b>{name}</b><small className="session-pop-path">{host} · {root}</small>
        <small className="session-pop-path">{defaultRoot ? t("默认连接目录：{path}", { path: defaultRoot }) : t("未设置默认工作区")}</small></div>
      <div className="session-pop-group">
        <button role="menuitem" data-action="remote.read" data-target={host} disabled={busy} onClick={() => { close(); onRefresh(); }}>
          <StudioIcon name="refresh" /><span>{t("刷新 {name} 上的会话", { name: host })}</span>
        </button>
        {onMove && ([-1, 1] as const).map((direction) => (
          <button key={direction} role="menuitem" data-action="remote-host.move" data-target={host} data-value={direction}
            disabled={busy || !onMove[direction]} onClick={() => { close(); onMove[direction]?.(); }}>
            <StudioIcon name="arrow" data-flip={direction === 1 ? "" : undefined} />
            <span>{t(direction === -1 ? "上移 {name}" : "下移 {name}", { name: host })}</span>
          </button>
        ))}
        <button className="danger" role="menuitem" data-action="remote-host.remove" disabled={busy}
          onClick={() => { close(); onRemove(); }}><StudioIcon name="close" /><span>{t("从列表移除")}</span><small>{t("不删除文件")}</small></button>
      </div>
    </div>, document.body)}
  </>;
}
