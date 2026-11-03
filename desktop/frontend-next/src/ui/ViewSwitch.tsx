import { t } from "../i18n";
import type { PaneView } from "../state/paneview";
import { ariaChord, chord } from "./keys";
import { StudioIcon } from "./StudioIcon";
import { TooltipButton } from "./TooltipButton";

/** The focused pane's two side views, drawn on the window bar. The conversation
 *  is where a pane rests, so it has no button: pressing the view that is showing
 *  returns to it. The analysis view is drawn only once there is a conversation
 *  to analyse. */
export function ViewSwitch({ view, rows, surfaces, onView }: {
  view: PaneView;
  rows: number;
  surfaces: number;
  onView: (to: PaneView) => void;
}) {
  const press = (to: PaneView) => onView(view === to ? "flow" : to);
  return (
    <>
      {rows > 0 && (
        <TooltipButton
          type="button" className="thbtn view-action top-action" data-action="pane.view" data-value="analysis"
          aria-pressed={view === "analysis"} aria-label={t("common.runAnalysis")} aria-keyshortcuts={ariaChord("2")}
          title={view === "analysis" ? t("viewSwitch.closeRunAnalysis") : t("viewSwitch.openRunAnalysis")} shortcut={chord("2")} onClick={() => press("analysis")}
        >
          <StudioIcon name="activity" />
          <span className="view-label">{t("common.runAnalysis")}</span>
        </TooltipButton>
      )}
      <TooltipButton
        type="button" className="thbtn view-action" data-action="pane.view" data-value="browser"
        aria-pressed={view === "browser"} aria-label={t("common.workbench")} aria-keyshortcuts={ariaChord("3")}
        title={view === "browser" ? t("viewSwitch.closeWorkbench") : t("viewSwitch.openWorkbench")} shortcut={chord("3")} onClick={() => press("browser")}
      >
        <StudioIcon name="panel" />
        {surfaces > 0 && <span className="view-count">{surfaces}</span>}
      </TooltipButton>
    </>
  );
}
