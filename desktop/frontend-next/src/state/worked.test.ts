import { describe, expect, it } from "vitest";
import { initialState, reduce, fromHistory } from "./session";
import { clock, elapsed } from "../i18n/format";

const run = (done: object) => reduce(reduce(reduce(initialState, { kind: "turn_started" }), { kind: "message", text: "答复" }), { kind: "turn_done", ...done });

describe("the turn's run time", () => {
  it("is drawn after the answer on the kernel's figure of the turn", () => {
    const s = run({ workDurationMs: 65_000 });
    expect(s.items.map((i) => i.t)).toEqual(["say", "worked"]);
    expect(s.items[1]).toMatchObject({ t: "worked", ms: 65_000 });
  });

  it("is not invented when the kernel sent none", () => {
    expect(run({}).items.map((i) => i.t)).toEqual(["say"]);
    expect(run({ workDurationMs: 300 }).items.map((i) => i.t)).toEqual(["say"]);
  });

  it("carries the kernel's start time, live and from history", () => {
    const live = run({ workDurationMs: 65_000, workStartedAt: 1_700_000_000_000 });
    expect(live.items[1]).toMatchObject({ t: "worked", ms: 65_000, startedAt: 1_700_000_000_000 });
    const { items } = fromHistory([
      { role: "user", content: "问", createdAt: 1_700_000_000_000 },
      { role: "assistant", content: "答", workDurationMs: 83_000 },
    ]);
    expect(items[2]).toMatchObject({ t: "worked", ms: 83_000, startedAt: 1_700_000_000_000 });
  });

  it("takes the start from the message that opened the turn, not a steer", () => {
    const { items } = fromHistory([
      { role: "user", content: "问", createdAt: 1_000 },
      { role: "user", content: "补充", steer: true, createdAt: 9_000 },
      { role: "assistant", content: "答", workDurationMs: 83_000 },
    ]);
    expect(items.find((i) => i.t === "worked")).toMatchObject({ startedAt: 1_000 });
  });

  it("comes back with the answer when the session is reopened", () => {
    const { items } = fromHistory([
      { role: "user", content: "问" },
      { role: "assistant", content: "答", workDurationMs: 83_000 },
      { role: "assistant", content: "旧会话没有记录" },
    ]);
    expect(items.map((i) => i.t)).toEqual(["user", "say", "worked", "say"]);
    expect(items[2]).toMatchObject({ t: "worked", ms: 83_000 });
  });
});

describe("elapsed", () => {
  it("rounds to seconds, then minutes and hours, dropping zero units", () => {
    expect(elapsed(0)).toBe("0s");
    expect(elapsed(59_400)).toBe("59s");
    expect(elapsed(60_000)).toBe("1m");
    expect(elapsed(65_000)).toBe("1m 5s");
    expect(elapsed(3_725_000)).toBe("1h 2m 5s");
    expect(elapsed(3_605_000)).toBe("1h 5s");
  });
});

describe("clock", () => {
  const at = new Date(2026, 9, 13, 14, 5).getTime();
  it("names the time of day for today and adds the date for another day", () => {
    expect(clock(at, new Date(2026, 9, 13, 18, 0).getTime())).toMatch(/^14:05$/);
    expect(clock(at, new Date(2026, 9, 14, 9, 0).getTime())).toMatch(/10.*13.*14:05|13.*10.*14:05/);
  });
});
