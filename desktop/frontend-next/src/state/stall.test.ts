import { describe, expect, it } from "vitest";
import { initialState, reduce, type SessionEvent, type SessionState } from "./session";
import { stallShown } from "./stall";

const run = (evs: SessionEvent[]): SessionState => evs.reduce(reduce, initialState);
const watch = (over: Record<string, unknown>): SessionEvent =>
  ({ kind: "progress_watch", progressWatch: { stalled: true, cause: "rounds", idleRounds: 20, roundLimit: 20, ...over } }) as SessionEvent;
const done = (over: Record<string, unknown> = {}): SessionEvent => ({ kind: "turn_done", ...over }) as SessionEvent;

// The strip says what the kernel's frame says, and nothing here decides a run
// has stalled: a report that it cleared, or the turn ending, takes it down.
describe("the progress watch strip", () => {
  it("holds the kernel's reading while it is true and drops it when it clears", () => {
    const stalled = run([{ kind: "turn_started" } as SessionEvent, watch({})]);
    expect(stallShown(stalled)).toMatchObject({ cause: "rounds", idleRounds: 20, paused: false });
    const cleared = reduce(stalled, watch({ stalled: false, cause: undefined, idleRounds: 0 }));
    expect(stallShown(cleared)).toBeUndefined();
  });

  it("goes when the turn ends any way but paused", () => {
    expect(stallShown(run([watch({}), done()]))).toBeUndefined();
    expect(stallShown(run([watch({}), done({ err: "boom" })]))).toBeUndefined();
  });

  it("muting hides a running stall for this session, never a pause", () => {
    const muted = run([watch({}), { kind: "__stall_mute" } as unknown as SessionEvent]);
    expect(stallShown(muted)).toBeUndefined();
    const paused = run([
      { kind: "__stall_mute" } as unknown as SessionEvent,
      watch({ idleRounds: 4, roundLimit: 4, pausing: true }),
      done({ err: "paused: …", outcome: "no_progress" }),
    ]);
    expect(stallShown(paused)).toMatchObject({ paused: true, idleRounds: 4 });
  });

  it("a pause is an incomplete turn with a plain note, not a failure card", () => {
    const s = run([watch({ idleRounds: 4, pausing: true }), done({ err: "paused: …", outcome: "no_progress" })]);
    expect(s.terminal).toEqual({ kind: "incomplete", outcome: "no_progress" });
    const notes = s.items.filter((i) => i.t === "notice");
    expect(notes).toHaveLength(1);
    expect(notes[0]).toMatchObject({ level: "info" });
    const dismissed = reduce(s, { kind: "__stall_dismiss" } as unknown as SessionEvent);
    expect(stallShown(dismissed)).toBeUndefined();
  });

  it("starts over with the next turn", () => {
    expect(stallShown(run([watch({}), { kind: "turn_started" } as SessionEvent]))).toBeUndefined();
  });
});
