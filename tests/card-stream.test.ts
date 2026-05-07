import { describe, expect, it } from "vitest";
import { splitCardStream } from "../src/cli/ui/layout/CardStream.js";
import type { Card, ToolCard, UserCard } from "../src/cli/ui/state/cards.js";

function userCard(id: string): UserCard {
  return { id, ts: 0, kind: "user", text: `user ${id}` };
}

function liveToolCard(id: string): ToolCard {
  return {
    id,
    ts: 0,
    kind: "tool",
    name: "submit_plan",
    args: {},
    output: "",
    done: false,
    elapsedMs: 0,
  };
}

describe("splitCardStream", () => {
  it("keeps the last unsettled card live by default", () => {
    const cards: Card[] = [userCard("u1"), liveToolCard("t1")];
    const result = splitCardStream(cards);
    expect(result.committed.map((c) => c.id)).toEqual(["u1"]);
    expect(result.live.map((c) => c.id)).toEqual(["t1"]);
  });

  it("suppresses the last unsettled card while a modal owns the screen", () => {
    const cards: Card[] = [userCard("u1"), liveToolCard("t1")];
    const result = splitCardStream(cards, true);
    expect(result.committed.map((c) => c.id)).toEqual(["u1"]);
    expect(result.live).toEqual([]);
  });

  it("does not drop settled cards when suppression is enabled", () => {
    const settled: ToolCard = { ...liveToolCard("t1"), done: true };
    const cards: Card[] = [userCard("u1"), settled];
    const result = splitCardStream(cards, true);
    expect(result.committed.map((c) => c.id)).toEqual(["u1", "t1"]);
    expect(result.live).toEqual([]);
  });
});
