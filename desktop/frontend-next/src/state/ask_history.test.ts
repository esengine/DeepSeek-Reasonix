import { describe, expect, it } from "vitest";
import { fromHistory, type Item } from "./session";

const args = JSON.stringify({
  questions: [
    {
      header: "Library",
      question: "Which parser should we use?",
      reason: "user_decision",
      options: [
        { label: "goldmark", description: "CommonMark, already vendored" },
        { label: "blackfriday" },
      ],
    },
    { header: "Scope", question: "What should change?", multiSelect: true, options: [{ label: "API" }, { label: "UI" }] },
  ],
});
const recorded = "The user answered:\n- Library: goldmark\n- Scope: API, keep the old flag";

// The live card comes from ask_request, which a reload does not replay. The
// question and its options are the call's arguments and the answer is its
// result, so the rebuild has everything the card needs.
describe("an answered ask in a reopened session", () => {
  it("comes back as a settled question with its options and the recorded answer", () => {
    const { items } = fromHistory([
      { role: "user", content: "pick one", msgIndex: 0 },
      { role: "assistant", content: "", msgIndex: 1, toolCalls: [{ id: "call_00_x", name: "ask", arguments: args }] },
      { role: "tool", content: recorded, msgIndex: 2, toolCallId: "call_00_x", toolName: "ask" },
    ]);
    const ask = items.find((i) => i.t === "ask") as Extract<Item, { t: "ask" }> | undefined;
    expect(ask).toBeDefined();
    expect(ask!.ask.id).toBe("call_00_x");
    expect(ask!.ask.questions).toEqual([
      {
        id: "q1", header: "Library", prompt: "Which parser should we use?", reason: "user_decision", multi: false,
        options: [{ label: "goldmark", description: "CommonMark, already vendored" }, { label: "blackfriday", description: undefined }],
      },
      {
        id: "q2", header: "Scope", prompt: "What should change?", reason: "user_decision", multi: true,
        options: [{ label: "API", description: undefined }, { label: "UI", description: undefined }],
      },
    ]);
    expect(ask!.answered).toEqual([]);
    expect(ask!.recorded).toBe(recorded);
    expect(items.some((i) => i.t === "tool" && i.tool.name === "ask")).toBe(false);
  });

  it("stays a plain call when the record holds no answer", () => {
    const unanswered = fromHistory([
      { role: "assistant", content: "", msgIndex: 0, toolCalls: [{ id: "c1", name: "ask", arguments: args }] },
    ]).items;
    expect(unanswered.map((i) => i.t)).toEqual(["tool"]);

    const failed = fromHistory([
      { role: "assistant", content: "", msgIndex: 0, toolCalls: [{ id: "c2", name: "ask", arguments: args }] },
      { role: "tool", content: "ask: context canceled", msgIndex: 1, toolCallId: "c2", toolName: "ask", toolFailed: true },
    ]).items;
    expect(failed.map((i) => i.t)).toEqual(["tool"]);

    const malformed = fromHistory([
      { role: "assistant", content: "", msgIndex: 0, toolCalls: [{ id: "c3", name: "ask", arguments: "{" }] },
      { role: "tool", content: "whatever", msgIndex: 1, toolCallId: "c3", toolName: "ask" },
    ]).items;
    expect(malformed.map((i) => i.t)).toEqual(["tool"]);
  });
});
