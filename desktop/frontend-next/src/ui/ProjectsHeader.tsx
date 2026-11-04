import { useContext } from "react";
import { t } from "../i18n";
import type { Adder } from "./addws";
import { StudioIcon } from "./StudioIcon";
import { RailFilterContext } from "./railfilter";

export function ProjectsHeader({ adder, onRecent }: { adder: Adder; onRecent?: () => void }) {
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
      </div>
    </div>
  );
}
