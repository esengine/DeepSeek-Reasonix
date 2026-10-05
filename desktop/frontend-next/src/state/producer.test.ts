import { describe, expect, it } from "vitest";
import { initialState, reduce, type Item, type SessionEvent } from "./session";

const ev = (kind: string, rest: Record<string, unknown> = {}) => ({ kind, ...rest }) as SessionEvent;
const says = (items: Item[]) => items.filter((i) => i.t === "say").map((i) => [i.text, i.source]);

// One turn, two models. The settled frame says who wrote each answer — the
// deltas before it carry nothing but text, which is what lets the stream
// coalesce. Nothing here counts phase markers, the only other way to guess and
// the one the record cannot support: it keeps no phase frames at all.
const TWO_MODEL: SessionEvent[] = [
  ev("turn_started", { authoredTurn: 1, msgIndex: 1 }),
  ev("phase", { text: "planner · planning" }),
  ev("reasoning", { text: "the shape is clear" }),
  ev("text", { text: "1. do the thing" }),
  ev("message", { text: "1. do the thing", reasoning: "the shape is clear", source: "planner" }),
  ev("phase", { text: "exec · executing" }),
  ev("reasoning", { text: "that settles it" }),
  ev("text", { text: "executor answered" }),
  ev("message", { text: "executor answered", reasoning: "that settles it", source: "executor" }),
  ev("turn_done"),
];

describe("which model wrote which card", () => {
  it("keeps each answer under the model that produced it", () => {
    expect(says(TWO_MODEL.reduce(reduce, initialState).items)).toEqual([
      ["1. do the thing", "planner"],
      ["executor answered", "executor"],
    ]);
  });

  // The destructive control. Strip the producer from the transport and the
  // cards must go unattributed — not fall back to the nearest phase marker,
  // which would still look right here and be wrong the moment a frame arrives
  // out of the order the markers suggest.
  it("attributes nothing when the transport drops the fact", () => {
    const stripped = TWO_MODEL.map((e) => {
      const { source: _dropped, ...rest } = e as { source?: string };
      return rest as SessionEvent;
    });
    expect(says(stripped.reduce(reduce, initialState).items)).toEqual([
      ["1. do the thing", undefined],
      ["executor answered", undefined],
    ]);
  });

  // A card built from the record alone carries it too: the settled frame is
  // what a replay has, and the producer rides that frame.
  it("carries the producer into a card rebuilt from the record", () => {
    const durable = TWO_MODEL.filter((e) => ["turn_started", "message", "turn_done"].includes((e as { kind: string }).kind));
    expect(says(durable.reduce(reduce, initialState).items)).toEqual([
      ["1. do the thing", "planner"],
      ["executor answered", "executor"],
    ]);
  });
});
