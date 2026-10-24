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
          aria-pressed={view === "analysis"} aria-label={t("运行分析")} aria-keyshortcuts={ariaChord("2")}
          title={view === "analysis" ? t("关闭运行分析") : t("打开运行分析")} shortcut={chord("2")} onClick={() => press("analysis")}
        >
          <StudioIcon name="activity" />
          <span className="view-label">{t("运行分析")}</span>
        </TooltipButton>
      )}
      <TooltipButton
        type="button" className="thbtn view-action" data-action="pane.view" data-value="browser"
        aria-pressed={view === "browser"} aria-label={t("工作台")} aria-keyshortcuts={ariaChord("3")}
        title={view === "browser" ? t("关闭工作台") : t("打开工作台")} shortcut={chord("3")} onClick={() => press("browser")}
      >
        <StudioIcon name="panel" />
        {surfaces > 0 && <span className="view-count">{surfaces}</span>}
      </TooltipButton>
    </>
  );
}
