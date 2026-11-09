import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fromHistory, initialState, reduce, type Item, type SessionEvent } from "./session";
import { thoughtStartedAt } from "./say";

beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(1_700_000_000_000); });
afterEach(() => vi.useRealTimers());

const ev = (kind: string, fields: Record<string, unknown> = {}) => ({ kind, ...fields }) as SessionEvent;
const tool = { id: "call-1", name: "bash", issuer: "model" };
const says = (items: Item[]) => items.filter((item): item is Extract<Item, { t: "say" }> => item.t === "say");
const thinking = () => [ev("turn_started"), ev("reasoning", { text: "Inspect first. " })].reduce(reduce, initialState);

describe("one assistant response across interleaved events", () => {
  it("stops thinking when tool arguments start, then settles the original card with the message frame", () => {
    let state = thinking();
    const id = says(state.items)[0].id;
    vi.advanceTimersByTime(1200);
    state = reduce(state, ev("tool_dispatch", { tool: { ...tool, partial: true } }));
    expect(says(state.items)[0]).toMatchObject({ id, done: false, thoughtMs: 1200 });
    expect(thoughtStartedAt(id)).toBeUndefined();
    vi.advanceTimersByTime(30_000);
    state = reduce(state, ev("text", { text: "I will inspect the files." }));
    state = reduce(state, ev("message", { reasoning: "Inspect first.", text: "I will inspect the files.", thoughtMs: 1100 }));
    expect(says(state.items)).toHaveLength(1);
    expect(says(state.items)[0]).toMatchObject({ id, done: true, thoughtMs: 1100, reasoning: "Inspect first." });
    state = reduce(state, ev("tool_dispatch", { tool }));
    state = reduce(state, ev("tool_result", { tool: { ...tool, output: "files" } }));
    state = reduce(state, ev("reasoning", { text: "Now edit." }));
    expect(says(state.items)).toHaveLength(2);
    expect(says(state.items)[1]).toMatchObject({ done: false, reasoning: "Now edit." });
  });

  it("keeps the active response across notices, pending input and child tool events", () => {
    let state = thinking();
    state = reduce(state, ev("notice", { text: "A background task finished." }));
    state = reduce(state, ev("__user", { text: "Also check styles", pending: true }));
    state = reduce(state, ev("tool_dispatch", { tool: { ...tool, parentId: "worker" } }));
    vi.advanceTimersByTime(800);
    state = reduce(state, ev("reasoning", { text: "Then compare." }));
    expect(says(state.items)).toHaveLength(1);
    expect(says(state.items)[0]).toMatchObject({ reasoning: "Inspect first. Then compare.", done: false });
    expect(says(state.items)[0].thoughtMs).toBeUndefined();
    state = reduce(state, ev("text", { text: "Compared." }));
    expect(says(state.items)[0].thoughtMs).toBe(800);
  });

  it("seals the response at full dispatch even when its message frame was missed", () => {
    let state = thinking();
    vi.advanceTimersByTime(900);
    state = reduce(state, ev("tool_dispatch", { tool }));
    expect(says(state.items)[0]).toMatchObject({ done: true, thoughtMs: 900 });
    state = reduce(state, ev("tool_result", { tool }));
    state = reduce(state, ev("reasoning", { text: "The next round." }));
    expect(says(state.items)).toHaveLength(2);
  });

  it("keeps an explicit zero measurement from the host over a local estimate", () => {
    let state = thinking();
    vi.advanceTimersByTime(900);
    state = reduce(state, ev("message", { reasoning: "Inspect first.", thoughtMs: 0 }));
    expect(says(state.items)[0].thoughtMs).toBe(0);
  });
});

describe("canonical idle state after an interrupted event stream", () => {
  it("seals the thought once without inventing a turn outcome", () => {
    let state = thinking();
    vi.advanceTimersByTime(2400);
    state = reduce(state, ev("__running", { running: false }));
    expect(says(state.items)[0]).toMatchObject({ done: true, thoughtMs: 2400 });
    expect(state.terminal).toBeNull();
    const settled = state;
    vi.advanceTimersByTime(1_800_000);
    state = reduce(state, ev("__running", { running: false }));
    expect(state).toBe(settled);
    expect(says(state.items)[0].thoughtMs).toBe(2400);
  });

  it("also seals a stream whose turn-start event was missed", () => {
    let state = reduce(initialState, ev("reasoning", { text: "Already streaming." }));
    expect(state.running).toBe(false);
    state = reduce(state, ev("__running", { running: false }));
    expect(says(state.items)[0].done).toBe(true);
  });

  it("does not close an ongoing response or start a clock on history", () => {
    const state = thinking();
    expect(reduce(state, ev("__running", { running: true }))).toBe(state);
    const history = fromHistory([{ role: "assistant", content: "Done.", reasoning: "Checked.", thoughtMs: 1600 }]);
    const restored = reduce(state, { kind: "__restore", ...history });
    const idle = reduce(restored, ev("__running", { running: false }));
    expect(says(idle.items)).toHaveLength(1);
    expect(says(idle.items)[0]).toMatchObject({ done: true, thoughtMs: 1600 });
    expect(thoughtStartedAt(says(idle.items)[0].id)).toBeUndefined();
  });
});
