import { describe, expect, it } from "vitest";
import { initialState, reduce, type Item, type SessionEvent } from "./session";
import go from "../../../../internal/contract/event/subagent_progress.go?raw";
import { SUBAGENT_PROGRESS_PREFIX } from "./fold";
import { railOf } from "../ui/panels/derive";

const prof = { name: "flash-fill" };
const dispatch = (id: string): SessionEvent =>
  ({ kind: "tool_dispatch", tool: { id, name: "use_capability", args: "{}", profile: prof } }) as SessionEvent;
const started = (id: string): SessionEvent =>
  ({ kind: "tool_result", tool: { id, name: "use_capability", output: 'Started background task "task-1"', profile: prof } }) as SessionEvent;
const status = (id: string, phase: string, parentId?: string): SessionEvent =>
  ({ kind: "tool_progress", tool: { id, parentId, name: "reasonix.subagent.status", output: phase, durationMs: phase === "completed" ? 41000 : undefined } }) as SessionEvent;

const after = (evs: SessionEvent[]) => evs.reduce(reduce, initialState);
const card = (s: ReturnType<typeof after>) => s.items.find((i): i is Extract<Item, { t: "tool" }> => i.t === "tool")!;
const liveTasks = (s: ReturnType<typeof after>) => railOf(s.items, s.executions, s.subagentPhase).tasks.filter((x) => x.running).length;

describe("a background sub-agent after its dispatching call has answered", () => {
  it("is still running while its last phase is not terminal", () => {
    const s = after([dispatch("c1"), started("c1"), status("c1", "running"), status("c1", "tool")]);
    expect(card(s).running).toBe(false);
    expect(liveTasks(s)).toBe(1);
  });

  it("is done once a terminal phase arrives, and the card is not reopened", () => {
    const s = after([dispatch("c1"), started("c1"), status("c1", "running"), status("c1", "completed")]);
    expect(card(s).running).toBe(false);
    expect(liveTasks(s)).toBe(0);
  });

  it("is done on failure and on cancellation too", () => {
    for (const phase of ["failed", "cancelled"]) {
      expect(liveTasks(after([dispatch("c1"), started("c1"), status("c1", "running"), status(("c1"), phase)]))).toBe(0);
    }
  });

  it("never renames the call or its recorded execution", () => {
    const s = after([dispatch("c1"), started("c1"), status("c1", "completed")]);
    expect(card(s).tool.name).toBe("use_capability");
    expect(s.executions["c1"]).toMatchObject({ name: "use_capability" });
  });

  it("keeps a call that is still open running", () => {
    expect(liveTasks(after([dispatch("c1"), status("c1", "completed")]))).toBe(1);
  });

  it("draws no card for a preview", () => {
    expect(after([status("c1", "queued")]).items).toEqual([]);
  });

  it("follows a fleet worker through the card that holds it", () => {
    const fleet = { kind: "tool_dispatch", tool: { id: "fl", name: "use_capability", profile: { name: "fleet", count: 2 } } } as SessionEvent;
    const worker = { kind: "tool_dispatch", tool: { id: "fl/w1", parentId: "fl", name: "task" } } as SessionEvent;
    const closed = { kind: "tool_result", tool: { id: "fl", name: "use_capability", profile: { name: "fleet", count: 2 } } } as SessionEvent;
    const mid = after([fleet, worker, closed, status("fl/w1", "running", "fl")]);
    expect(liveTasks(mid)).toBe(1);
    expect(liveTasks(after([fleet, worker, closed, status("fl/w1", "running", "fl"), status("fl/w1", "completed", "fl")]))).toBe(0);
  });
});

describe("the reserved prefix", () => {
  it("is the kernel's", () => {
    expect(go.match(/SubagentProgressPrefix\s*=\s*"([^"]+)"/)?.[1]).toBe(SUBAGENT_PROGRESS_PREFIX);
  });
});

describe("a proxied sub-agent while its call is still open", () => {
  const proxied = { kind: "tool_dispatch", tool: { id: "c1", name: "use_capability", args: "{}" } } as SessionEvent;
  const marked = { kind: "tool_dispatch", tool: { id: "c1", name: "use_capability", args: "{}", refreshed: true, profile: prof } } as SessionEvent;
  const step = { kind: "tool_dispatch", tool: { id: "c1/s1", parentId: "c1", name: "read_file" } } as SessionEvent;

  it("is no task before the kernel marks it, and a running one the moment it does", () => {
    expect(liveTasks(after([proxied, step]))).toBe(0);
    const s = after([proxied, marked, step]);
    expect(liveTasks(s)).toBe(1);
    expect(card(s).children).toHaveLength(1);
  });

  it("keeps the one card when the mark arrives", () => {
    expect(after([proxied, marked, step]).items.filter((i) => i.t === "tool")).toHaveLength(1);
  });
});
