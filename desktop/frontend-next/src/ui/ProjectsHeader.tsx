import { useContext, useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import type { Adder } from "./addws";
import { useDismiss } from "./dismiss";
import { pinToViewport } from "./place";
import { StudioIcon } from "./StudioIcon";
import { workspaceMenuKeys } from "./WorkspaceOrder";
import { RailFilterContext } from "./railfilter";

export function ProjectsHeader({ adder, canSelect, selecting, onSelect, onRecent, onToggleAll, allFolded, canFold, searching }: {
  adder: Adder;
  canSelect: boolean;
  selecting: boolean;
  onSelect: () => void;
  onRecent?: () => void;
  onToggleAll?: () => void;
  allFolded?: boolean;
  canFold?: boolean;
  searching?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const more = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const close = () => { setOpen(false); more.current?.focus(); };
  useDismiss(open, more, close, menu);
  useEffect(() => {
    if (open) menu.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus();
  }, [open]);
  useLayoutEffect(() => {
    const anchor = more.current?.getBoundingClientRect();
    if (open && anchor && menu.current) pinToViewport(menu.current, anchor.right + 8, anchor.top - 7, 12);
  }, [open]);

  const filter = useContext(RailFilterContext);
  return (
    <div className="projects-header">
      <div className="projects-tabs">
        <div className="vtabs" role="group" aria-label={t("会话视图")}>
          <button type="button" className="vtab" data-action="session.view" data-value="projects" aria-pressed="true">{t("项目")}</button>
          {onRecent && <button type="button" className="vtab" data-action="session.recent" title={t("最近的会话")} aria-label={t("最近的会话")} onClick={onRecent}>{t("最近")}</button>}
        </div>
        <span className="projects-spacer" />
        {filter}
        <button className="projects-add" data-busy={adder.busy ? "" : undefined} disabled={adder.busy}
          title={t("打开项目")} aria-label={t("打开项目")} data-action="workspace.add" onClick={() => adder.add()}>
          <StudioIcon name="plus" />
        </button>
        <button ref={more} className="wsmore" title={t("更多操作")}
          aria-label={t("项目列表的更多操作")} aria-haspopup="menu" aria-expanded={open}
          data-action="machine.menu" onClick={() => setOpen((v) => !v)}>
          <StudioIcon name="more" />
        </button>
      </div>
      {open && createPortal(
        <div ref={menu} className="session-pop" role="menu" aria-label={t("项目列表的更多操作")}
          data-action-keydown="machine.menu" onKeyDown={workspaceMenuKeys}>
          <div className="session-pop-head"><b>{t("项目")}</b></div>
          <div className="session-pop-group">
            <button role="menuitem" data-action="session.selection" data-value="start"
              disabled={!canSelect || selecting} onClick={() => { close(); onSelect(); }}>
              <StudioIcon name="check" /><span>{t("选择本机会话")}</span>
            </button>
            {onToggleAll && (
              <button role="menuitem" data-action="workspace.fold-all" disabled={!canFold || searching} aria-label={t(allFolded ? "展开所有项目" : "收起所有项目")}
                onClick={() => { close(); onToggleAll(); }}>
                <StudioIcon name={allFolded ? "expandAll" : "collapseAll"} /><span>{t(allFolded ? "展开所有项目" : "收起所有项目")}</span>
              </button>
            )}
          </div>
        </div>, document.body,
      )}
    </div>
  );
}
