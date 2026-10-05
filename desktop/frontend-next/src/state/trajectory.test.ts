import { describe, expect, it } from "vitest";
import { initialTraj, reduceTraj, type Span, type TrajState } from "./trajectory";
import type { WireEvent } from "../port/wire";

const run = (evs: WireEvent[]): TrajState => evs.reduce((s, e) => reduceTraj(s, e), initialTraj);
const flat = (spans: Span[]) => spans.map((x) => ("b" in x ? x.b : "n" in x ? x.n : x.t)).join("");
const line = (s: TrajState, i: number) => flat(s.rows[i].payload);

const begin = (id: string, attempt = 1): WireEvent =>
  ({ kind: "stream_attempt", streamAttempt: { id, action: "begin", attempt } }) as WireEvent;
const commit = (id: string): WireEvent =>
  ({ kind: "stream_attempt", streamAttempt: { id, action: "commit" } }) as WireEvent;
const discard = (id: string, reason: string): WireEvent =>
  ({ kind: "stream_attempt", streamAttempt: { id, action: "discard", reason } }) as WireEvent;
const usage = (attemptId?: string, source?: string): WireEvent =>
  ({
    kind: "usage",
    usage: {
      promptTokens: 0,
      completionTokens: 900,
      totalTokens: 900,
      cacheHitTokens: 41000,
      cacheMissTokens: 2000,
      sessionCacheHitTokens: 0,
      sessionCacheMissTokens: 0,
      attemptId,
      source,
    },
  }) as WireEvent;

describe("a round's tokens land on the round", () => {
  // Usage is emitted after the round it belongs to has committed. Attaching it
  // to "whichever round is still open" therefore resolved to nothing and the
  // numbers were dropped — a trajectory that could say how long a turn took but
  // never what it cost.
  it("attaches usage to a round that has already settled", () => {
    const s = run([begin("sa-1"), commit("sa-1"), usage("sa-1")]);
    expect(s.rows).toHaveLength(1);
    expect(line(s, 0)).toContain("model_round");
    expect(line(s, 0)).toContain("out ");
    expect(line(s, 0)).toContain("src=executor");
    expect(s.rows[0].out).toBe(900);
  });

  it("bills the attempt that committed, not the one still on the page", () => {
    const s = run([begin("sa-1"), discard("sa-1", "premature_eof"), begin("sa-2", 2), commit("sa-2"), usage("sa-2")]);
    expect(s.rows).toHaveLength(2);
    expect(line(s, 0)).not.toContain("out ");
    expect(line(s, 1)).toContain("out ");
  });

  it("gives usage that belongs to no round a row of its own", () => {
    const s = run([begin("sa-1"), commit("sa-1"), usage(undefined, "compaction")]);
    expect(s.rows).toHaveLength(2);
    expect(line(s, 1)).toContain("usage");
    expect(line(s, 1)).toContain("src=compaction");
  });

  // A log written before the kernel stamped the id still replays; it just
  // cannot say which round paid, and says so on its own line instead of vanishing.
  it("keeps usage naming a round that never reached this page", () => {
    const s = run([usage("sa-9")]);
    expect(s.rows).toHaveLength(1);
    expect(line(s, 0)).toContain("usage");
  });
});

describe("a turn's ending is legible", () => {
  // A user pressing stop and a gateway dropping the connection both arrive as
  // a context-canceled error. The reader has to be able to tell them apart.
  it("names the user as the one who stopped a cancelled turn", () => {
    const s = run([{ kind: "turn_done", err: 'Post "https://api/x": context canceled', cancelled: true } as WireEvent]);
    expect(line(s, 0)).toContain("context canceled");
    expect(line(s, 0)).toContain("cancelled by user");
  });

  it("leaves a turn that failed on its own unattributed", () => {
    const s = run([{ kind: "turn_done", err: "boom" } as WireEvent]);
    expect(line(s, 0)).toBe("turn_done · err=boom");
  });
});

describe("time that was spent and thrown away still shows", () => {
  it("marks a discarded round with why it was discarded", () => {
    const s = run([begin("sa-1"), discard("sa-1", "premature_eof")]);
    expect(line(s, 0)).toContain("discarded");
    expect(line(s, 0)).toContain("premature_eof");
  });
});

describe("frames arriving without their other half", () => {
  it("ignores a settle for an activity that never opened", () => {
    const s = run([commit("sa-unknown")]);
    expect(s.rows).toHaveLength(0);
  });

  it("keeps a tool's own line rather than opening a second one", () => {
    const s = run([
      { kind: "tool_dispatch", tool: { id: "t1", name: "bash", readOnly: false } } as WireEvent,
      { kind: "tool_result", tool: { id: "t1", name: "bash", durationMs: 420, err: "command exited: exit status 1" } } as WireEvent,
    ]);
    expect(s.rows).toHaveLength(1);
    expect(s.rows[0].dur).toBeCloseTo(0.42, 3);
    expect(line(s, 0)).toContain("exit status 1");
  });
});

describe("a retry row names why", () => {
  const retrying = (over: Partial<WireEvent>): WireEvent =>
    ({ kind: "retrying", retryAttempt: 1, retryMax: 10, retryScope: "headers", ...over }) as WireEvent;

  it("carries the failure class and the status that caused it", () => {
    const s = run([retrying({ retryCause: "upstream_status", retryStatus: 502 })]);
    expect(line(s, 0)).toBe("retry 1/10 · scope=headers · cause=upstream_status 502");
  });

  it("stays as it was for a kernel that sends no class", () => {
    expect(line(run([retrying({})]), 0)).toBe("retry 1/10 · scope=headers");
  });
});
