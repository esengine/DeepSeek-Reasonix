import { useRef } from "react";
import { t } from "../i18n";
import { arrowTabs } from "./tablist";

export type PaneView = "flow" | "analysis" | "browser";

/** The pane's own navigation. It projects the five views onto three standing
 *  purposes; it does not own them, and it never holds a selection of its own —
 *  a second piece of state saying which detail is open is a second answer to a
 *  question the pane has already answered. */
export function PaneNav({ view, onPick, rows, surfaces }: {
  view: PaneView;
  onPick: (to: PaneView) => void;
  rows: number;
  // Browsers and files open on the workbench's own tab strip. The workbench is
  // always reachable — the workspace tree lives there — so this is the count
  // beside the tab, never whether to draw it. It counts what that strip holds:
  // a number naming one thing and standing for another is what put a 2 over a
  // strip with one tab on it.
  surfaces: number;
}) {
  const bar = useRef<HTMLDivElement>(null);
  // Read at render: t() answers out of a dictionary boot() installs, and a
  // table built in the module body freezes the labels in the source language.
  const name: Record<PaneView, string> = {
    flow: t("对话"), analysis: t("运行分析"), browser: t("工作台"),
  };

  return (
    <div className="tabs">
      <div className="vtabs" role="tablist" ref={bar} onKeyDown={arrowTabs}>
        {/* No item count. How many rows a transcript holds does not change what
            anyone does next, and it was the largest number on the bar. */}
        <button
          className="tab" role="tab" data-action="pane.view" data-value="flow"
          aria-selected={view === "flow"} onClick={() => onPick("flow")}
        >
          {name.flow}
        </button>
        {rows > 0 && (
          <button
            className="tab" role="tab" data-action="pane.view" data-value="analysis"
            aria-selected={view === "analysis"} onClick={() => onPick("analysis")}
          >
            {name.analysis}
          </button>
        )}
        <button
          className="tab" role="tab" data-action="pane.view" data-value="browser"
          aria-selected={view === "browser"} onClick={() => onPick("browser")}
        >
          {name.browser}
          {surfaces > 0 && <span className="n">{surfaces}</span>}
        </button>
      </div>
    </div>
  );
}
