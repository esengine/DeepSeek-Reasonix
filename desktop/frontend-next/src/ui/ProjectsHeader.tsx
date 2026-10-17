import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import type { Adder } from "./addws";
import { useDismiss } from "./dismiss";
import { pinToViewport } from "./place";
import { StudioIcon } from "./StudioIcon";
import { workspaceMenuKeys } from "./WorkspaceOrder";

export function ProjectsHeader({ adder, canSelect, selecting, onSelect, onToggleAll, allFolded, canFold, searching }: {
  adder: Adder;
  canSelect: boolean;
  selecting: boolean;
  onSelect: () => void;
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

  return (
    <div className="projects-header">
      <div className="projects-heading">
        <span className="projects-heading-title">{t("项目")}
          {onToggleAll && <button className="projects-toggle" data-action="workspace.fold-all"
            disabled={!canFold || searching} aria-expanded={!allFolded}
            title={t(searching ? "搜索时自动展开项目" : allFolded ? "展开所有项目" : "收起所有项目")}
            aria-label={t(allFolded ? "展开所有项目" : "收起所有项目")} onClick={onToggleAll}>
            <StudioIcon name={allFolded ? "expandAll" : "collapseAll"} />
          </button>}
        </span>
        <span className="projects-heading-actions">
          <button className="projects-add" data-busy={adder.busy ? "" : undefined} disabled={adder.busy}
            title={t("打开项目")} aria-label={t("打开项目")} data-action="workspace.add" onClick={() => adder.add()}>
            <StudioIcon name="plus" />
          </button>
          <button ref={more} className="wsmore" title={t("更多操作")}
            aria-label={t("项目列表的更多操作")} aria-haspopup="menu" aria-expanded={open}
            data-action="machine.menu" onClick={() => setOpen((v) => !v)}>
            <StudioIcon name="more" />
          </button>
        </span>
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
          </div>
        </div>, document.body,
      )}
    </div>
  );
}
