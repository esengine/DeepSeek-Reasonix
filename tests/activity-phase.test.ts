import { describe, expect, it } from "vitest";
import { deriveActivityLabel } from "../src/cli/ui/hooks/useActivityPhase.js";
import type { Card } from "../src/cli/ui/state/cards.js";

function user(id: string): Card {
  return { id, ts: 0, kind: "user", text: "" };
}
function reasoning(id: string, streaming: boolean): Card {
  return { id, ts: 0, kind: "reasoning", text: "", paragraphs: 0, tokens: 0, streaming };
}
function tool(id: string, done: boolean): Card {
  return { id, ts: 0, kind: "tool", name: "read_file", args: {}, output: "", done, elapsedMs: 0 };
}
function streaming(id: string, done: boolean): Card {
  return { id, ts: 0, kind: "streaming", text: "", done };
}

describe("deriveActivityLabel", () => {
  it("returns 'waiting for model…' when only the user card exists", () => {
    expect(deriveActivityLabel([user("u1")])).toBe("waiting for model…");
  });

  it("returns 'waiting for model…' when card list is empty", () => {
    expect(deriveActivityLabel([])).toBe("waiting for model…");
  });

  it("returns 'thinking…' while a reasoning card is streaming", () => {
    expect(deriveActivityLabel([user("u1"), reasoning("r1", true)])).toBe("thinking…");
  });

  it("returns 'processing…' once reasoning has settled but no follow-up card exists yet", () => {
    expect(deriveActivityLabel([user("u1"), reasoning("r1", false)])).toBe("processing…");
  });

  it("returns 'processing…' between a finished tool and the next event", () => {
    expect(deriveActivityLabel([user("u1"), tool("t1", true)])).toBe("processing…");
  });

  it("prefers 'thinking…' even when a settled reasoning card sits later in the list", () => {
    expect(deriveActivityLabel([user("u1"), reasoning("r1", true), reasoning("r2", false)])).toBe(
      "thinking…",
    );
  });

  it("returns 'processing…' when the last card is a streaming content card", () => {
    expect(deriveActivityLabel([user("u1"), streaming("s1", false)])).toBe("processing…");
  });
});
