// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "../testkit";
import { Transcript } from "../Transcript";
import { fromHistory, type Item } from "../../state/session";
import type { HistoryMessage } from "../../port/session";
import { SALES, chartCall } from "./fixtures";

afterEach(cleanup);

const noop = async () => undefined as never;
function draw(items: Item[]) {
  return render(
    <Transcript items={items} entering={[]} onEntered={() => {}} revision={1} waiting={{}} scroll={{ current: null }} hidden={false}
      onPinned={() => {}} jump={0} focus={null} onApprove={noop} onFullAccess={noop} onPlan={noop} onAnswer={noop} onForget={noop}
      onExtInvoke={() => {}} onExtSubmit={noop} checkpoints={new Map()} onPrepareRewind={noop} onCommitRewind={noop} onUndoRewind={noop}
      onPrepareFileRevert={noop} onCommitFileRevert={noop} needsProject={false} onOpenProject={() => {}} onKeepHere={() => {}} />,
  ).container;
}

const turn = (tool: ReturnType<typeof chartCall>): Item[] => [
  { t: "user", id: "u1", text: "chart it", authoredTurn: 1, msgIndex: 0 },
  { t: "say", id: "s1", text: "Here you go.", done: true },
  { t: "tool", id: "t1", running: false, children: [], tool },
];

const history = (spec: unknown, failed = false): HistoryMessage[] => {
  const call = chartCall(spec);
  return [
    { role: "user", content: "chart it", msgIndex: 0 },
    { role: "assistant", content: "", toolCalls: [{ id: "c1", name: "use_capability", arguments: call.args, resolvedName: "render_chart", capabilityId: "tool:render_chart" }] },
    { role: "tool", content: failed ? "chart.schema_invalid" : "chart_id: chart-abc", toolCallId: "c1", toolFailed: failed },
  ] as HistoryMessage[];
};

describe("a chart in the transcript", () => {
  it("is its own row, outside the folded execution group", async () => {
    const box = draw(turn(chartCall(SALES)));
    expect(await screen.findByRole("group", { name: /Sales/ })).not.toBeNull();
    expect(box.querySelector(".activity-group .chart")).toBeNull();
    expect(box.querySelector('[data-item="t1"] .chart')).not.toBeNull();
  });

  it("falls back to the ordinary tool card while the call runs", () => {
    const box = draw(turn(chartCall(SALES, { output: undefined })));
    expect(box.querySelector(".chart")).toBeNull();
    expect(box.querySelector(".activity-group")).not.toBeNull();
  });

  it("falls back to the ordinary tool card when the kernel refused the spec", () => {
    const box = draw(turn(chartCall({ ...SALES, marks: [] }, { err: "chart.schema_invalid" })));
    expect(box.querySelector(".chart")).toBeNull();
    expect(box.textContent).toContain("chart.schema_invalid");
  });

  it("draws the same chart after a reload as it drew live", async () => {
    const live = draw(turn(chartCall(SALES)));
    await screen.findByRole("group", { name: /Sales/ });
    const liveSvg = live.querySelector(".chart-svg")?.innerHTML;
    cleanup();
    const rebuilt = fromHistory(history(SALES)).items;
    const box = draw(rebuilt);
    await screen.findByRole("group", { name: /Sales/ });
    expect(box.querySelector(".chart-svg")?.innerHTML).toBe(liveSvg);
  });

  it("does not draw a rebuilt call the kernel had refused, or a hostile stored spec", () => {
    for (const items of [fromHistory(history(SALES, true)).items, fromHistory(history({ ...SALES, spec_version: 9 })).items]) {
      const box = draw(items);
      expect(box.querySelector(".chart")).toBeNull();
      cleanup();
    }
  });
});
