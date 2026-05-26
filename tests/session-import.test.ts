import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, describe, expect, it } from "vitest";
import { buildImportedSessionName, parseExternalSessionFile } from "../src/session-import.js";

describe("session import parsers", () => {
  it("parses Claude sessions into Reasonix messages", () => {
    const source = [
      JSON.stringify({
        isMeta: true,
        type: "user",
        message: { role: "user", content: "skip me" },
      }),
      JSON.stringify({
        type: "user",
        cwd: "/tmp/claude-proj",
        message: { role: "user", content: "Need review" },
      }),
      JSON.stringify({
        type: "assistant",
        message: {
          role: "assistant",
          content: [
            { type: "tool_use", id: "tool-1", name: "shell", input: { command: "pwd" } },
            { type: "text", text: "Running it." },
          ],
        },
      }),
      JSON.stringify({
        type: "user",
        message: {
          role: "user",
          content: [{ type: "tool_result", tool_use_id: "tool-1", content: "file1\nfile2" }],
        },
      }),
    ].join("\n");

    const tmp = writeFixture("claude-session.jsonl", source);
    const imported = parseExternalSessionFile("claude", tmp);

    expect(imported.workspace).toBe("/tmp/claude-proj");
    expect(imported.summary).toBe("Need review");
    expect(imported.messages).toEqual([
      { role: "user", content: "Need review" },
      {
        role: "assistant",
        content: "Running it.",
        tool_calls: [
          {
            id: "tool-1",
            type: "function",
            function: { name: "shell", arguments: '{"command":"pwd"}' },
          },
        ],
        reasoning_content: undefined,
      },
      { role: "tool", content: "file1\nfile2", tool_call_id: "tool-1", name: "shell" },
    ]);
  });

  it("parses Codex response items and builds a default name", () => {
    const source = [
      JSON.stringify({
        type: "session_meta",
        payload: { cwd: "/tmp/codex-proj" },
      }),
      JSON.stringify({
        type: "response_item",
        payload: {
          type: "message",
          role: "user",
          content: [{ type: "input_text", text: "Fix the deploy race" }],
        },
      }),
      JSON.stringify({
        type: "response_item",
        payload: {
          type: "message",
          role: "assistant",
          content: [{ type: "output_text", text: "On it." }],
        },
      }),
    ].join("\n");

    const tmp = writeFixture("rollout-019e.jsonl", source);
    const imported = parseExternalSessionFile("codex", tmp);

    expect(imported.workspace).toBe("/tmp/codex-proj");
    expect(imported.messages).toEqual([
      { role: "user", content: "Fix the deploy race" },
      { role: "assistant", content: "On it." },
    ]);
    expect(buildImportedSessionName("codex", tmp, imported)).toBe("codex-Fix the deploy race");
  });
});

function writeFixture(name: string, body: string): string {
  const dir = mkdtempSync(join(tmpdir(), "reasonix-session-import-"));
  const path = join(dir, name);
  writeFileSync(path, `${body}\n`, "utf8");
  fixtures.push(dir);
  return path;
}

const fixtures: string[] = [];

afterAll(() => {
  for (const dir of fixtures) rmSync(dir, { recursive: true, force: true });
});
