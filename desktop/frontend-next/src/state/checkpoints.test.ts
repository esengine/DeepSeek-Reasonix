import { describe, expect, it } from "vitest";
import { pairCheckpoints } from "./checkpoints";
import { fromHistory, initialState, reduce, type Item, type SessionEvent, type SessionState } from "./session";
import type { Checkpoint, HistoryMessage } from "../port/port";

const run = (evs: SessionEvent[]): SessionState => evs.reduce(reduce, initialState);
const sent = (id: string, text: string): SessionEvent => ({ kind: "__user", text, pending: false, id }) as SessionEvent;
const typed = (id: string, text: string): SessionEvent => ({ kind: "__user", text, pending: true, id }) as SessionEvent;
const started = (authoredTurn: number, msgIndex: number): SessionEvent =>
  ({ kind: "turn_started", authoredTurn, msgIndex }) as SessionEvent;
const users = (st: SessionState) => st.items.filter((i): i is Extract<Item, { t: "user" }> => i.t === "user");

describe("the message a turn is about", () => {
  it("names the row whose send caused the turn", () => {
    const st = run([sent("row-1", "第一句"), started(1, 1)]);
    expect(users(st)[0]).toMatchObject({ authoredTurn: 1, msgIndex: 1 });
  });

  // The kernel numbers turns from the conversation, not from the transcript.
  // A row that read its own position would be right exactly once.
  it("takes the kernel's numbers rather than its own position", () => {
    const st = run([sent("row-1", "一"), started(1, 1), sent("row-2", "二"), started(2, 5)]);
    expect(users(st).map((u) => [u.authoredTurn, u.msgIndex])).toEqual([
      [1, 1],
      [2, 5],
    ]);
  });

  // Guidance dropped in mid-turn starts no turn of its own. If it could take
  // the slot, the next turn_started would name the wrong row — which is the
  // failure the whole join exists to rule out.
  it("is not claimed by a steer queued while the turn runs", () => {
    const st = run([sent("row-1", "第一句"), typed("row-2", "顺便"), started(1, 1)]);
    const [first, steer] = users(st);
    expect(first).toMatchObject({ authoredTurn: 1, msgIndex: 1 });
    expect(steer.msgIndex).toBeUndefined();
  });

  // Two sends can be outstanding at once: the kernel queues the second as a
  // follow-up. They are named in the order it will run them.
  it("names outstanding sends in the order they were sent", () => {
    const st = run([sent("row-1", "一"), sent("row-2", "二"), started(1, 1), started(2, 5)]);
    expect(users(st).map((u) => [u.id, u.msgIndex])).toEqual([
      ["row-1", 1],
      ["row-2", 5],
    ]);
  });

  // The row is gone, so its name is dropped rather than handed to the turn that
  // did start — which is the row the next send puts on screen.
  it("owes no name to a line the kernel never took", () => {
    const taken = run([sent("row-1", "撤回"), { kind: "__unsent", id: "row-1" } as SessionEvent]);
    const st = [started(1, 1), sent("row-2", "改口"), started(1, 1)].reduce(reduce, taken);
    expect(users(st)).toMatchObject([{ id: "row-2", authoredTurn: 1, msgIndex: 1 }]);
  });

  // A kernel that says a turn began without saying what it is about leaves the
  // row unnamed rather than guessing at one.
  it("leaves the row unnamed when the turn names no message", () => {
    const st = run([sent("row-1", "第一句"), { kind: "turn_started" } as SessionEvent]);
    expect(users(st)[0].msgIndex).toBeUndefined();
    expect(st.awaitingTurnStart).toEqual(["row-1"]);
  });

  it("reads the index off the record when a session is rebuilt", () => {
    const msgs: HistoryMessage[] = [
      { role: "system", content: "sys", msgIndex: 0 },
      { role: "user", content: "第一句", msgIndex: 1 },
      { role: "assistant", content: "答", msgIndex: 2 },
      { role: "user", content: "第二句", msgIndex: 3 },
    ];
    expect(fromHistory(msgs).items.filter((i) => i.t === "user")).toMatchObject([{ msgIndex: 1 }, { msgIndex: 3 }]);
  });
});

describe("pairing a row with the snapshot taken for it", () => {
  const cp = (turn: number, prompt: string, msgIndex?: number): Checkpoint => ({ turn, prompt, files: 0, msgIndex });

  // The destructive case: the two numbers disagree and the prompt does not
  // match at all. The pairing has to hold anyway, because neither was ever what
  // it rested on.
  it("joins through the message index, not the turn number or the prompt", () => {
    const st = run([sent("row-1", "什么都不像的一句"), started(1, 7)]);
    const pairs = pairCheckpoints(st.items, [cp(0, "完全不同的提示词", 7)]);
    expect(pairs.get("row-1")?.turn).toBe(0);
  });

  it("keeps two identical prompts on their own snapshots", () => {
    const st = run([sent("row-1", "再来一次"), started(1, 1), sent("row-2", "再来一次"), started(2, 5)]);
    const pairs = pairCheckpoints(st.items, [cp(0, "再来一次", 1), cp(1, "再来一次", 5)]);
    expect(pairs.get("row-1")?.turn).toBe(0);
    expect(pairs.get("row-2")?.turn).toBe(1);
  });

  // A rewind cannot be undone, so a row that names no message gets no entry
  // point rather than the one that happens to sit at its position.
  it("offers nothing for a row or a snapshot that names no message", () => {
    const st = run([sent("row-1", "第一句"), started(1, 1), typed("row-2", "顺便")]);
    expect(pairCheckpoints(st.items, [cp(0, "第一句")]).size).toBe(0);
    expect(pairCheckpoints(st.items, [cp(0, "第一句", 1)]).size).toBe(1);
  });
});
