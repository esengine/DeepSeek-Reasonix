import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import { useDismiss } from "./dismiss";
import { pinToViewport } from "./place";
import { StudioIcon } from "./StudioIcon";
import { workspaceMenuKeys } from "./WorkspaceOrder";

export function RemoteProjectMenu({ name, root, host, defaultRoot, busy, onRefresh, onRemove }: {
  name: string;
  root: string;
  host: string;
  defaultRoot?: string;
  busy: boolean;
  onRefresh: () => void;
  onRemove?: () => void;
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
      title={t("更多操作")} aria-label={t("项目操作：{name}", { name: name + " · " + host })}
      aria-haspopup="menu" aria-expanded={open} onClick={(event) => { event.stopPropagation(); setOpen((value) => !value); }}>
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
        {onRemove && <button role="menuitem" data-action="remote-host.remove" disabled={busy}
          onClick={() => { close(); onRemove(); }}><StudioIcon name="close" /><span>{t("从列表移除")}</span><small>{t("不删除文件")}</small></button>}
      </div>
    </div>, document.body)}
  </>;
}
