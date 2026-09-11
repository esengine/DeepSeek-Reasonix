import { useRef } from "react";
import { t } from "../i18n";
import { Picker, type MenuItem } from "./Menu";
import { arrowTabs } from "./tablist";

/** The five things a pane can be showing. These identities are the pane's and
 *  are not this file's to change: a graph node hands focus to a call by landing
 *  on one of them, the task board reaches two more, and a run that loses its
 *  graph sends the reader back to the first. Only which of them the bar spends
 *  a permanent slot on is decided here. */
export type PaneView = "flow" | "line" | "traj" | "graph" | "task";

// Which of the five answer "how do I read this run" rather than "what am I
// doing". Two navigations sat one above the other speaking the same grammar —
// a row of tabs under a row of tabs — so which row changed the session and
// which changed the view had to be worked out on every glance. These three are
// diagnosis and reach the reader through one entry instead of three slots.
const DETAIL = ["line", "traj", "graph"] as const;

type Detail = (typeof DETAIL)[number];

const isDetail = (view: PaneView): view is Detail => (DETAIL as readonly string[]).includes(view);

/** The pane's own navigation. It projects the five views onto three standing
 *  purposes; it does not own them, and it never holds a selection of its own —
 *  a second piece of state saying which detail is open is a second answer to a
 *  question the pane has already answered. */
export function PaneNav({ view, onPick, done, steps, nodes, rows }: {
  view: PaneView;
  onPick: (to: PaneView) => void;
  done: number;
  steps: number;
  // The run graph's size, which decides both the timeline's existence and the
  // two counts drawn from it.
  nodes: number;
  rows: number;
}) {
  const bar = useRef<HTMLDivElement>(null);
  const detail = isDetail(view) ? view : null;
  // Read at render: t() answers out of a dictionary boot() installs, and a
  // table built in the module body freezes the labels in the source language.
  const name: Record<PaneView, string> = {
    flow: t("对话"), task: t("任务"),
    line: t("时间线"), traj: t("事件轨迹"), graph: t("运行图"),
  };
  // The timeline is offered only while there is a graph to draw one from — the
  // same condition that sends a session watching it back to the transcript when
  // the graph goes away. A disabled row would be a control for a view that does
  // not exist, which is the dead control this product does not draw.
  const items: MenuItem[] = [
    ...(nodes > 0 ? [{ value: "line", label: name.line, right: String(nodes) }] : []),
    { value: "traj", label: name.traj, right: String(rows) },
    { value: "graph", label: name.graph, right: nodes > 0 ? String(nodes) : undefined },
  ];

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
        {/* This one stays: steps done out of steps planned is progress, and
            progress is the thing the count was supposed to be. */}
        <button
          className="tab" role="tab" data-action="pane.view" data-value="task"
          aria-selected={view === "task"} onClick={() => onPick("task")}
        >
          {name.task}
          {steps > 0 && <span className="n">{done}/{steps}</span>}
        </button>
      </div>
      <Picker
        className={detail ? "tab more on" : "tab more"}
        // One identity, because it is one intent: a button on the bar and a row
        // in this menu are two places to say "show me the run this way", not two
        // things a person can do.
        data-action="pane.view"
        place="top"
        // Right-aligned: it is the last thing on the bar, and a menu wider than
        // the trigger has the room on that side, not past the window edge.
        align="end"
        current={detail ?? undefined}
        items={items}
        onPick={(to) => onPick(to as PaneView)}
        label={
          <>
            {/* Degrading the entrance must not hide the position: with one of
                the three open the trigger carries that name alone. 运行详情
                names the category and stands in only while nobody is inside it. */}
            <span className="now">{detail ? name[detail] : t("运行详情")}</span>
            <i className="cv" aria-hidden>▾</i>
          </>
        }
      />
    </div>
  );
}
