import { describe, expect, it } from "vitest";
import * as desktopCommand from "../src/cli/commands/desktop.js";
import type { ChatMessage } from "../src/types.js";

type BuildLoadedMessages = (records: ChatMessage[]) => Array<{
  kind: "assistant" | "user";
  segments?: Array<{ kind: string; text?: string; args?: string; result?: string }>;
}>;

describe("desktop session loading", () => {
  it("elides old heavy assistant segments before sending $session_loaded", () => {
    const buildLoadedMessages = (desktopCommand as { buildLoadedMessages?: BuildLoadedMessages })
      .buildLoadedMessages;
    expect(typeof buildLoadedMessages).toBe("function");

    const huge = "desktop retained field\n".repeat(900);
    const records: ChatMessage[] = [];
    for (let i = 0; i < 260; i++) {
      records.push({
        role: "assistant",
        content: huge,
        reasoning_content: huge,
        tool_calls: [
          {
            id: `c-${i}`,
            type: "function",
            function: {
              name: "write_file",
              arguments: JSON.stringify({ path: `file-${i}.txt`, content: huge }),
            },
          },
        ],
      });
      records.push({ role: "tool", tool_call_id: `c-${i}`, content: huge });
    }

    const loaded = buildLoadedMessages!(records);
    const firstAssistant = loaded.find((m) => m.kind === "assistant");
    expect(firstAssistant).toBeDefined();
    const reasoning = firstAssistant!.segments!.find((s) => s.kind === "reasoning");
    const text = firstAssistant!.segments!.find((s) => s.kind === "text");
    const tool = firstAssistant!.segments!.find((s) => s.kind === "tool");

    expect(reasoning?.text?.length).toBeLessThan(huge.length / 10);
    expect(text?.text?.length).toBeLessThan(huge.length / 10);
    expect(tool?.args?.length).toBeLessThan(huge.length / 10);
    expect(tool?.result?.length).toBeLessThan(huge.length / 10);
  });
});
