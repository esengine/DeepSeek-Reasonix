// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import "./testkit";
import type { TrajRow } from "../state/trajectory";
import { RunAnalysis } from "./RunAnalysis";

beforeEach(() => { vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(1000); });
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const rows: TrajRow[] = [
  { seq: 1, at: 0, dur: 4, kind: "model_round", payload: [{ t: "model_round" }], subs: [] },
  { seq: 2, at: 1, dur: 2, kind: "tool", tool: "web_fetch", payload: [{ t: "tool " }, { b: "web_fetch" }], subs: [] },
  { seq: 3, at: 3.2, kind: "protocol_recovery", payload: [{ t: "retry 1/2" }], subs: [] },
];

const onSave = async () => null;

describe("RunAnalysis", () => {
  it("says what the signal rail is for and why its marks do not move", () => {
    const moments: TrajRow[] = [...rows, { seq: 4, at: 2, kind: "note", payload: [{ t: "checkpoint" }], subs: [] }];
    render(<RunAnalysis rows={moments} onSave={onSave}  onBack={() => {}} />);
    expect(screen.getByText("信号轨")).toBeTruthy();
    expect(screen.getByText(/位置就是时间，无法拖动/)).toBeTruthy();
  });

  it("draws no rail, and no explanation of one, when nothing is a moment", () => {
    render(<RunAnalysis rows={rows.slice(0, 2)} onSave={onSave}  onBack={() => {}} />);
    expect(screen.queryByText("信号轨")).toBeNull();
    expect(screen.queryByText(/位置就是时间/)).toBeNull();
  });

  it("summarises recorded activity and exposes an inspectable timeline", () => {
    render(<RunAnalysis rows={rows} onSave={onSave}  onBack={() => {}} />);
    expect(screen.getByText("运行分析")).toBeTruthy();
    expect(screen.getByText("工具耗时")).toBeTruthy();
    expect(screen.getByText("活动回合")).toBeTruthy();

    const tool = screen.getByRole("button", { name: /web_fetch/ });
    fireEvent.click(tool);
    expect(tool.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getAllByText("web_fetch").length).toBeGreaterThan(1);
  });

  it("does not invent metrics before a run exists", () => {
    render(<RunAnalysis rows={[]} onSave={onSave}  onBack={() => {}} />);
    expect(screen.getByText("还没有可分析的运行")).toBeTruthy();
  });

  it("exports the same recorded rows directly from analysis", async () => {
    const save = vi.fn(async (_name: string, _content: string) => "D:/exports/trajectory.json");
    render(<RunAnalysis rows={rows} availability="complete" onSave={save}  onBack={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: "导出运行记录" }));
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
    const { container } = render(<RunAnalysis rows={many} onSave={onSave}  onBack={() => {}} />);
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
    const { container } = render(<RunAnalysis rows={withSignal} onSave={onSave}  onBack={() => {}} />);
    fireEvent.click(container.querySelector<HTMLButtonElement>("[data-action='analysis.signal'][data-target='9']")!);
    expect(container.querySelector(".run-signals .run-inspect")).not.toBeNull();
  });

  it("zooms one shared timeline, preserves selection and resets the scroll position", () => {
    const { container } = render(<RunAnalysis rows={rows} onSave={onSave}  onBack={() => {}} />);
    const slider = screen.getByRole("slider", { name: "时间轴缩放" });
    const scroller = screen.getByRole("region", { name: "时间轴" });
    const canvas = container.querySelector<HTMLElement>(".run-timeline-canvas")!;
    const signal = container.querySelector<HTMLButtonElement>("[data-action='analysis.signal']")!;
    fireEvent.click(signal);
    fireEvent.change(slider, { target: { value: "8" } });
    expect(canvas.style.width).toBe("800%");
    expect(canvas.contains(container.querySelector(".run-round-track"))).toBe(true);
    expect(canvas.contains(signal)).toBe(true);
    expect(signal.getAttribute("aria-expanded")).toBe("true");
    scroller.scrollLeft = 300;
    fireEvent.click(screen.getByRole("button", { name: "重置缩放" }));
    expect(canvas.style.width).toBe("100%");
    expect(scroller.scrollLeft).toBe(0);
    expect(signal.getAttribute("aria-expanded")).toBe("true");
  });

  it("folds simultaneous signals into one counted marker and keeps every one inspectable", () => {
    const moments: TrajRow[] = Array.from({ length: 12 }, (_, seq) => ({
      seq, at: 0, kind: "checkpoint", payload: [{ t: `signal ${seq}` }], subs: [],
    }));
    const { container } = render(<RunAnalysis rows={moments} onSave={onSave}  onBack={() => {}} />);
    const rail = container.querySelector<HTMLElement>(".run-signal-line")!;
    const marker = rail.querySelector<HTMLButtonElement>("[data-action='analysis.signal']")!;
    expect(rail.querySelectorAll("[data-action='analysis.signal']")).toHaveLength(1);
    expect(marker.textContent).toBe("12");
    expect(marker.getAttribute("aria-label")).toContain("12 个信号");
    expect(container.querySelector(".run-signal-group")).toBeNull();
    fireEvent.click(marker);
    const items = [...container.querySelectorAll<HTMLButtonElement>(".run-signal-group [data-action='analysis.signal']")];
    expect(items).toHaveLength(12);
    for (const [index, item] of items.entries()) {
      fireEvent.click(item);
      expect(item.getAttribute("aria-expanded")).toBe("true");
      expect(container.querySelector(".run-inspect p")?.textContent).toBe(`signal ${index}`);
    }
    fireEvent.click(items[11]);
    expect(container.querySelector(".run-inspect")).toBeNull();
    fireEvent.click(marker);
    expect(container.querySelector(".run-signal-group")).toBeNull();
  });

  it("handles a single zero-time signal and newly arriving records without resetting zoom", () => {
    const moment: TrajRow = { seq: 1, at: 0, kind: "note", payload: [{ t: "start" }], subs: [] };
    const { container, rerender } = render(<RunAnalysis rows={[moment]} onSave={onSave}  onBack={() => {}} />);
    fireEvent.change(screen.getByRole("slider"), { target: { value: "16" } });
    rerender(<RunAnalysis rows={[moment, { ...moment, seq: 2, at: 1 }]} onSave={onSave}  onBack={() => {}} />);
    expect((screen.getByRole("slider") as HTMLInputElement).value).toBe("16");
    expect(container.querySelector("[data-target='1']")?.getAttribute("style")).not.toMatch(/NaN|Infinity/);
    expect(container.querySelectorAll("[data-action='analysis.signal']")).toHaveLength(2);
    expect(container.querySelector(".run-signal-line")!.textContent).not.toMatch(/\d/);
  });


  it("splits a counted marker as the measured timeline widens, without moving any timestamp", () => {
    let measure = () => {};
    let width = 100;
    vi.stubGlobal("ResizeObserver", class {
      constructor(fn: () => void) { measure = fn; }
      observe() {}
      disconnect() {}
    });
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(() => width);
    const moments: TrajRow[] = [1, 1.1, 10].map((at, seq) => ({
      seq, at, kind: "checkpoint", payload: [{ t: `signal ${seq}` }], subs: [],
    }));
    const { container } = render(<RunAnalysis rows={moments} onSave={onSave}  onBack={() => {}} />);
    const markers = () => [...container.querySelectorAll<HTMLButtonElement>("[data-action='analysis.signal']")];
    expect(markers().map((m) => m.textContent)).toEqual(["2", ""]);
    const first = markers()[0].style.left;
    act(() => { width = 3000; measure(); });
    expect(markers()).toHaveLength(3);
    expect(markers()[0].style.left).toBe(first);
  });

  it("merges marks closer than one hit target, so no two markers sit within it", () => {
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(100);
    const moments: TrajRow[] = [0, 13, 100].map((at, seq) => ({
      seq, at, kind: "checkpoint", payload: [{ t: `signal ${seq}` }], subs: [],
    }));
    const { container } = render(<RunAnalysis rows={moments} onSave={onSave}  onBack={() => {}} />);
    const markers = [...container.querySelectorAll<HTMLButtonElement>("[data-action='analysis.signal']")];
    expect(markers.map((m) => m.textContent)).toEqual(["2", ""]);
    const [a, b] = markers.map((m) => parseFloat(m.style.left));
    expect((b - a) * 100).toBeGreaterThanOrEqual(22);
  });

  it.each([1, 16])("keeps activity duration percentages exact at %s× zoom and after reset", (zoom) => {
    const timed: TrajRow[] = [
      { seq: 1, at: 10, dur: .1, kind: "tool", tool: "bash", payload: [{ t: "short" }], subs: [] },
      { seq: 2, at: 20, dur: 0, kind: "protocol_recovery", payload: [{ t: "retry zero" }], subs: [] },
      { seq: 3, at: 30, kind: "protocol_recovery", payload: [{ t: "retry unknown" }], subs: [] },
      { seq: 4, at: 40, dur: 10, kind: "model_round", payload: [{ t: "long" }], subs: [] },
      { seq: 5, at: 100, kind: "event", payload: [], subs: [] },
    ];
    const { container } = render(<RunAnalysis rows={timed} onSave={onSave}  onBack={() => {}} />);
    const bars = [...container.querySelectorAll<HTMLElement>(".run-round-track > i")];
    fireEvent.click(container.querySelector("[data-action='analysis.row'][data-target='1']")!);
    fireEvent.change(screen.getByRole("slider"), { target: { value: String(zoom) } });
    expect(bars.map((bar) => bar.style.left)).toEqual(["10%", "20%", "30%", "40%"]);
    expect(bars.map((bar) => bar.style.width)).toEqual(["0.1%", "0%", "0%", "10%"]);
    expect(container.querySelector<HTMLElement>(".run-timeline-canvas")?.style.width).toBe(`${zoom * 100}%`);
    expect(container.querySelector(".run-inspect p")?.textContent).toBe("short");
    fireEvent.click(screen.getByRole("button", { name: "重置缩放" }));
    expect(bars.map((bar) => bar.style.width)).toEqual(["0.1%", "0%", "0%", "10%"]);
    expect(container.querySelector(".run-inspect p")?.textContent).toBe("short");
  });

  it("keeps zero-span recovery markers finite and duration-free at maximum zoom", () => {
    const recovery: TrajRow = { seq: 1, at: 0, dur: 0, kind: "protocol_recovery", payload: [{ t: "retry" }], subs: [] };
    const { container } = render(<RunAnalysis rows={[recovery]} onSave={onSave}  onBack={() => {}} />);
    fireEvent.change(screen.getByRole("slider"), { target: { value: "16" } });
    const bar = container.querySelector<HTMLElement>(".run-round-track > i")!;
    expect(bar.style.left).toBe("0%");
    expect(bar.style.width).toBe("0%");
    fireEvent.click(container.querySelector("[data-action='analysis.row']")!);
    expect(container.querySelector(".run-inspect p")?.textContent).toBe("retry");
  });

});

describe("leaving the analysis view", () => {
  it("puts a back control beside the title and hands the press to the caller", () => {
    const onBack = vi.fn();
    render(<RunAnalysis rows={rows} onSave={async () => null} onBack={onBack} />);
    fireEvent.click(screen.getByRole("button", { name: "返回正文" }));
    expect(onBack).toHaveBeenCalledTimes(1);
  });
});
