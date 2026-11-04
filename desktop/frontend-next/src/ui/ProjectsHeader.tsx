import { useContext } from "react";
import { t } from "../i18n";
import type { Adder } from "./addws";
import { StudioIcon } from "./StudioIcon";
import { RailFilterContext } from "./railfilter";

export function ProjectsHeader({ adder, onRecent, onToggleAll, allFolded, canFold, searching }: { adder: Adder; onRecent?: () => void; onToggleAll?: () => void; allFolded?: boolean; canFold?: boolean; searching?: boolean }) {
  const filter = useContext(RailFilterContext);
  return (
    <div className="projects-header">
      <div className="projects-tabs">
        <div className="vtabs" role="group" aria-label={t("会话视图")}>
          <button type="button" className="vtab" data-action="session.view" data-value="projects" aria-pressed="true">{t("项目")}</button>
          {onRecent && <button type="button" className="vtab" data-action="session.recent" title={t("最近的会话")} aria-label={t("最近的会话")} onClick={onRecent}>{t("最近")}</button>}
        </div>
        <span className="projects-spacer" />
        {onToggleAll && (
          <button className="projects-add projects-fold" data-action="workspace.fold-all" disabled={!canFold || searching} aria-expanded={!allFolded}
            title={t(searching ? "搜索时自动展开项目" : allFolded ? "展开所有项目" : "收起所有项目")} aria-label={t(allFolded ? "展开所有项目" : "收起所有项目")} onClick={onToggleAll}>
            <StudioIcon name={allFolded ? "expandAll" : "collapseAll"} />
          </button>
        )}
        {filter}
        <button className="projects-add" data-busy={adder.busy ? "" : undefined} disabled={adder.busy}
          title={t("打开项目")} aria-label={t("打开项目")} data-action="workspace.add" onClick={() => adder.add()}>
          <StudioIcon name="plus" />
        </button>
      </div>
    </div>
  );
}
