import { COMPACTION_SUMMARY_MARKER } from "@reasonix/core-utils";
import { describe, expect, it } from "vitest";
import { hydrateCardsFromMessages } from "../src/cli/ui/state/hydrate.js";
import type { ChatMessage } from "../src/types.js";

describe("hydrateCardsFromMessages", () => {
  it("returns [] for empty input", () => {
    expect(hydrateCardsFromMessages([])).toEqual([]);
  });

  it("skips system messages", () => {
    const msgs: ChatMessage[] = [{ role: "system", content: "you are helpful" }];
    expect(hydrateCardsFromMessages(msgs)).toEqual([]);
  });

  it("turns user + assistant text into UserCard + StreamingCard", () => {
    const msgs: ChatMessage[] = [
      { role: "user", content: "hi" },
      { role: "assistant", content: "hello there" },
    ];
    const cards = hydrateCardsFromMessages(msgs);
    expect(cards.map((c) => c.kind)).toEqual(["user", "streaming"]);
    expect(cards[0]).toMatchObject({ kind: "user", text: "hi" });
    expect(cards[1]).toMatchObject({ kind: "streaming", text: "hello there", done: true });
  });

  it("turns a fold summary assistant message into a CompactionCard", () => {
    const msgs: ChatMessage[] = [
      { role: "assistant", content: `${COMPACTION_SUMMARY_MARKER}earlier turns explored auth.` },
      { role: "user", content: "continue" },
    ];
    const cards = hydrateCardsFromMessages(msgs);
    expect(cards.map((c) => c.kind)).toEqual(["compaction", "user"]);
    expect(cards[0]).toMatchObject({
      kind: "compaction",
      summary: "earlier turns explored auth.",
    });
  });

  it("emits a ReasoningCard before the StreamingCard when reasoning_content is present", () => {
    const msgs: ChatMessage[] = [
      {
        role: "assistant",
        content: "answer",
        reasoning_content: "step 1\n\nstep 2",
      },
    ];
    const cards = hydrateCardsFromMessages(msgs);
    expect(cards.map((c) => c.kind)).toEqual(["reasoning", "streaming"]);
    expect(cards[0]).toMatchObject({
      kind: "reasoning",
      text: "step 1\n\nstep 2",
      streaming: false,
      paragraphs: 2,
    });
  });

  it("matches a tool result back to the originating tool_call by id", () => {
    const msgs: ChatMessage[] = [
      {
        role: "assistant",
        content: null,
        tool_calls: [
          {
            id: "call-1",
            type: "function",
            function: { name: "shell", arguments: '{"cmd":"ls"}' },
          },
        ],
      },
      { role: "tool", tool_call_id: "call-1", content: "a.txt\nb.txt" },
    ];
    const cards = hydrateCardsFromMessages(msgs);
    expect(cards).toHaveLength(1);
    expect(cards[0]).toMatchObject({
      kind: "tool",
      name: "shell",
      args: { cmd: "ls" },
      output: "a.txt\nb.txt",
      done: true,
    });
  });

  it("hydrates run_command exit markers into tool card exitCode", () => {
    const msgs: ChatMessage[] = [
      {
        role: "assistant",
        content: null,
        tool_calls: [
          {
            id: "call-1",
            type: "function",
            function: { name: "run_command", arguments: '{"command":"node test.mjs"}' },
          },
        ],
      },
      {
        role: "tool",
        tool_call_id: "call-1",
        content: "$ node test.mjs\n[exit 1]\nAssertionError: expected 9000",
      },
    ];
    const cards = hydrateCardsFromMessages(msgs);
    expect(cards[0]).toMatchObject({
      kind: "tool",
      name: "run_command",
      exitCode: 1,
      done: true,
    });
  });

  it("keeps raw string args when the tool_call arguments aren't valid JSON", () => {
    const msgs: ChatMessage[] = [
      {
        role: "assistant",
        content: null,
        tool_calls: [
          {
            id: "call-x",
            type: "function",
            function: { name: "noisy", arguments: "not-json" },
          },
        ],
      },
    ];
    const cards = hydrateCardsFromMessages(msgs);
    expect(cards[0]).toMatchObject({ kind: "tool", args: "not-json", done: false });
  });

  it("starts resumed long sessions with old heavy card fields already elided", () => {
    const huge = "large retained field\n".repeat(800);
    const msgs: ChatMessage[] = [];
    for (let i = 0; i < 260; i++) {
      msgs.push({
        role: "assistant",
        content: `answer ${i}`,
        reasoning_content: huge,
        tool_calls: [
          {
            id: `call-${i}`,
            type: "function",
            function: {
              name: "write_file",
              arguments: JSON.stringify({ path: `file-${i}.txt`, content: huge }),
            },
          },
        ],
      });
      msgs.push({ role: "tool", tool_call_id: `call-${i}`, content: huge });
    }

    const cards = hydrateCardsFromMessages(msgs);
    const oldReasoning = cards.find((c) => c.kind === "reasoning");
    const oldTool = cards.find((c) => c.kind === "tool");

    expect(oldReasoning).toMatchObject({ kind: "reasoning" });
    expect(oldTool).toMatchObject({ kind: "tool" });
    if (oldReasoning?.kind !== "reasoning" || oldTool?.kind !== "tool") {
      throw new Error("expected old heavy cards");
    }
    expect(oldReasoning.text.length).toBeLessThan(huge.length / 10);
    expect(oldTool.output.length).toBeLessThan(huge.length / 10);
    expect(JSON.stringify(oldTool.args).length).toBeLessThan(huge.length / 10);
  });

  it("produces unique card ids across the hydrated batch", () => {
    const msgs: ChatMessage[] = [
      { role: "user", content: "one" },
      { role: "assistant", content: "two" },
      { role: "user", content: "three" },
      { role: "assistant", content: "four" },
    ];
    const ids = hydrateCardsFromMessages(msgs).map((c) => c.id);
    expect(new Set(ids).size).toBe(ids.length);
  });
});
