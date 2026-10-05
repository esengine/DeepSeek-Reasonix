// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import "./testkit";
import type { TrajRow } from "../state/trajectory";
import { RunAnalysis } from "./RunAnalysis";

afterEach(cleanup);

const rows: TrajRow[] = [
  { seq: 1, at: 0, dur: 4, kind: "model_round", payload: [{ t: "model_round" }], subs: [] },
  { seq: 2, at: 1, dur: 2, kind: "tool", tool: "web_fetch", payload: [{ t: "tool " }, { b: "web_fetch" }], subs: [] },
  { seq: 3, at: 3.2, kind: "protocol_recovery", payload: [{ t: "retry 1/2" }], subs: [] },
];

const onSave = async () => null;

describe("RunAnalysis", () => {
  it("summarises recorded activity and exposes an inspectable timeline", () => {
    render(<RunAnalysis rows={rows} onSave={onSave} />);
    expect(screen.getByText("运行分析")).toBeTruthy();
    expect(screen.getByText("工具耗时")).toBeTruthy();
    expect(screen.getByText("活动回合")).toBeTruthy();

    const tool = screen.getByRole("button", { name: /web_fetch/ });
    fireEvent.click(tool);
    expect(tool.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getAllByText("web_fetch").length).toBeGreaterThan(1);
  });

  it("does not invent metrics before a run exists", () => {
    render(<RunAnalysis rows={[]} onSave={onSave} />);
    expect(screen.getByText("还没有可分析的运行")).toBeTruthy();
  });

  it("exports the same recorded rows directly from analysis", async () => {
    const save = vi.fn(async (_name: string, _content: string) => "D:/exports/trajectory.json");
    render(<RunAnalysis rows={rows} availability="complete" onSave={save} />);
    fireEvent.click(screen.getByRole("button", { name: "导出轨迹" }));
    await vi.waitFor(() => expect(save).toHaveBeenCalledOnce());
    const [name, content] = save.mock.calls[0];
    expect(name).toMatch(/^trajectory-.*\.json$/);
    const exported = JSON.parse(content);
    expect(exported.availability).toBe("complete");
    expect(exported.rows.map((row: { seq: number }) => row.seq)).toEqual([1, 2, 3]);
  });

  it("opens the record under the row that was clicked, without remounting the list", () => {
    const many: TrajRow[] = Array.from({ length: 60 }, (_, i) => ({
      seq: i + 1, at: i, dur: 1, kind: "tool", tool: "bash", payload: [{ t: `step ${i + 1}` }], subs: [],
    }));
    const scrollIntoView = vi.fn();
    Element.prototype.scrollIntoView = scrollIntoView;
    const { container } = render(<RunAnalysis rows={many} onSave={onSave} />);
    const items = [...container.querySelectorAll<HTMLLIElement>(".run-rounds li")];
    const before = items[40];
    fireEvent.click(before.querySelector("button")!);
    const after = container.querySelectorAll<HTMLLIElement>(".run-rounds li")[40];
    expect(after).toBe(before);
    expect(after.querySelector(".run-inspect")?.textContent).toContain("step 41");
    expect(container.querySelectorAll(".run-inspect")).toHaveLength(1);
    expect(before.querySelector("button")!.getAttribute("aria-expanded")).toBe("true");
    expect(scrollIntoView).toHaveBeenCalledWith({ block: "nearest" });
    fireEvent.click(after.querySelector("button")!);
    expect(container.querySelector(".run-inspect")).toBeNull();
  });

  it("opens a signal's record beside the signal track, where it was picked", () => {
    const withSignal: TrajRow[] = [...rows, { seq: 9, at: 2, kind: "checkpoint", payload: [{ t: "marker" }], subs: [] }];
    const { container } = render(<RunAnalysis rows={withSignal} onSave={onSave} />);
    fireEvent.click(container.querySelector<HTMLButtonElement>("[data-action='analysis.signal'][data-target='9']")!);
    expect(container.querySelector(".run-signals .run-inspect")).not.toBeNull();
  });
});
