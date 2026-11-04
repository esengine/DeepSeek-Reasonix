import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { useDismiss } from "./dismiss";
import { pinToViewport } from "./place";
import { StudioIcon } from "./StudioIcon";
import { workspaceMenuKeys } from "./WorkspaceOrder";

export function RemoteProjectMenu({ name, root, host, defaultRoot, busy, onRefresh, onRemove, onMove }: {
  name: string;
  root: string;
  host: string;
  defaultRoot?: string;
  busy: boolean;
  onRefresh: () => void;
  onRemove?: () => void;
  // Steps this row's machine up or down the book; a direction with no neighbour is absent.
  onMove?: Partial<Record<-1 | 1, () => void>>;
}) {
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const close = () => { setOpen(false); trigger.current?.focus(); };
  useDismiss(open, trigger, close, menu);
  useLayoutEffect(() => {
    const rect = trigger.current?.getBoundingClientRect();
    if (open && rect && menu.current) pinToViewport(menu.current, rect.right + 8, rect.top, 12);
  }, [open]);
  useEffect(() => { if (open) menu.current?.querySelector<HTMLElement>("button:not(:disabled)")?.focus(); }, [open]);
  return <>
    <button ref={trigger} className="wsmore" data-action="workspace.menu" data-target={host + ":" + root}
      title={t("common.more_actions")} aria-label={t("common.project_actions_name", { name: name + " · " + host })}
      aria-haspopup="menu" aria-expanded={open} onClick={(event) => { event.stopPropagation(); setOpen((value) => !value); }}>
      <StudioIcon name="more" />
    </button>
    {open && createPortal(<div ref={menu} className="session-pop" role="menu" aria-label={t("common.project_actions")}
      data-action-keydown="workspace.menu" onKeyDown={workspaceMenuKeys} onClick={(event) => event.stopPropagation()}>
      <div className="session-pop-head"><b>{name}</b><small className="session-pop-path">{host} · {root}</small>
        <small className="session-pop-path">{defaultRoot ? t("remote.default_connection_directory_path", { path: defaultRoot }) : t("remote.no_default_workspace")}</small></div>
      <div className="session-pop-group">
        <button role="menuitem" data-action="remote.read" data-target={host} disabled={busy} onClick={() => { close(); onRefresh(); }}>
          <StudioIcon name="refresh" /><span>{t("remote.refresh_the_sessions_on_name", { name: host })}</span>
        </button>
        {onMove && ([-1, 1] as const).map((direction) => (
          <button key={direction} role="menuitem" data-action="remote-host.move" data-target={host} data-value={direction}
            disabled={busy || !onMove[direction]} onClick={() => { close(); onMove[direction]?.(); }}>
            <StudioIcon name="arrow" data-flip={direction === 1 ? "" : undefined} />
            <span>{t(direction === -1 ? "common.move_name_up" : "common.move_name_down", { name: host })}</span>
          </button>
        ))}
        {onRemove && <button role="menuitem" data-action="remote-host.remove" disabled={busy}
          onClick={() => { close(); onRemove(); }}><StudioIcon name="close" /><span>{t("common.remove_from_the_list")}</span><small>{t("common.keeps_files")}</small></button>}
      </div>
    </div>, document.body)}
  </>;
}
