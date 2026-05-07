import { describe, expect, it } from "vitest";
import { splitCardStream } from "../src/cli/ui/layout/CardStream.js";
import type {
  Card,
  ReasoningCard,
  StreamingCard,
  ToolCard,
  UserCard,
} from "../src/cli/ui/state/cards.js";

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

function liveReasoningCard(id: string): ReasoningCard {
  return {
    id,
    ts: 0,
    kind: "reasoning",
    text: "thinking",
    paragraphs: 0,
    tokens: 0,
    streaming: true,
  };
}

function liveStreamingCard(id: string): StreamingCard {
  return { id, ts: 0, kind: "streaming", text: "", done: false };
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

  it("keeps a still-streaming reasoning card live even when a later card appears", () => {
    // Reasoning is mid-stream; a streaming-content card lands behind it.
    // Reasoning must NOT be committed to Static while streaming=true, or
    // the spinner survives reasoning.end (Static doesn't re-render frozen items).
    const cards: Card[] = [userCard("u1"), liveReasoningCard("r1"), liveStreamingCard("s1")];
    const result = splitCardStream(cards);
    expect(result.committed.map((c) => c.id)).toEqual(["u1"]);
    expect(result.live.map((c) => c.id)).toEqual(["r1", "s1"]);
  });

  it("commits a settled reasoning card once every later card is also settled", () => {
    const settledReasoning: ReasoningCard = {
      ...liveReasoningCard("r1"),
      streaming: false,
      endedAt: 1,
    };
    const settledStreaming: StreamingCard = {
      ...liveStreamingCard("s1"),
      done: true,
      endedAt: 2,
    };
    const cards: Card[] = [userCard("u1"), settledReasoning, settledStreaming];
    const result = splitCardStream(cards);
    expect(result.committed.map((c) => c.id)).toEqual(["u1", "r1", "s1"]);
    expect(result.live).toEqual([]);
  });
});
