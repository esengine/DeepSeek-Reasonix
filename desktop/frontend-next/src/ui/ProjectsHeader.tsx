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
        <div className="vtabs" role="group" aria-label={t("session.session_view")}>
          <button type="button" className="vtab" data-action="session.view" data-value="projects" aria-pressed="true">{t("common.project")}</button>
          {onRecent && <button type="button" className="vtab" data-action="session.recent" title={t("session.recent_sessions")} aria-label={t("session.recent_sessions")} onClick={onRecent}>{t("session.recent")}</button>}
        </div>
        <span className="projects-spacer" />
        {onToggleAll && (
          <button className="projects-add projects-fold" data-action="workspace.fold-all" disabled={!canFold || searching} aria-expanded={!allFolded}
            title={t(searching ? "session.projects_expand_automatically_while_searching" : allFolded ? "session.expand_all_projects" : "session.collapse_all_projects")} aria-label={t(allFolded ? "session.expand_all_projects" : "session.collapse_all_projects")} onClick={onToggleAll}>
            <StudioIcon name={allFolded ? "expandAll" : "collapseAll"} />
          </button>
        )}
        {filter}
        <button className="projects-add" data-busy={adder.busy ? "" : undefined} disabled={adder.busy}
          title={t("session.open_project")} aria-label={t("session.open_project")} data-action="workspace.add" onClick={() => adder.add()}>
          <StudioIcon name="plus" />
        </button>
      </div>
    </div>
  );
}
