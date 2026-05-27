/** App-scroll CardStream must tolerate bursts of edit tool results. */

import type React from "react";
import { createElement, useEffect } from "react";
import { describe, expect, it, vi } from "vitest";
import { CardStream } from "../src/cli/ui/layout/CardStream.js";
import { ChatScrollProvider } from "../src/cli/ui/state/chat-scroll-provider.js";
import { AgentStoreProvider, useAgentStore } from "../src/cli/ui/state/provider.js";
import type { SessionInfo } from "../src/cli/ui/state/state.js";
import { render } from "./helpers/ink-test.js";

const SESSION: SessionInfo = {
  id: "s-edit-results",
  branch: "main",
  workspace: "/tmp/repo",
  model: "deepseek-chat",
};

function editArgs(index: number): Record<string, unknown> {
  return {
    path: `src/file-${index}.ts`,
    search: `const value${index} = "before";`,
    replace: `const value${index} = "after";`,
  };
}

function editOutput(index: number): string {
  return [
    "▸ edit blocks: 1/1 applied — /undo to roll back, or `git diff` to review",
    `  ✓ applied     src/file-${index}.ts`,
    "  @@",
    `  - const value${index} = "before";`,
    `  + const value${index} = "after";`,
  ].join("\n");
}

function pendingPreview(count: number): string {
  const lines = [
    `▸ ${count} pending edit block(s) — /apply (or y) to commit · /discard (or n) to drop  ·  /apply N or 1,3-4 for partial`,
  ];
  for (let i = 0; i < count; i++) {
    lines.push(`[${i + 1}] src/file-${i}.ts`);
    lines.push("  @@");
    lines.push(`  - const value${i} = "before";`);
    lines.push(`  + const value${i} = "after";`);
  }
  return lines.join("\n");
}

function EmitEditResultBurst(): null {
  const store = useAgentStore();
  useEffect(() => {
    for (let i = 0; i < 60; i++) {
      const id = `edit-${i}`;
      const name = i % 3 === 0 ? "multi_edit" : "edit_file";
      const args = name === "multi_edit" ? { edits: [editArgs(i)] } : editArgs(i);
      store.dispatch({ type: "tool.start", id, name, args });
      store.dispatch({ type: "tool.end", id, output: editOutput(i), elapsedMs: 4 });
    }
    store.dispatch({
      type: "live.show",
      id: "pending-edits",
      ts: Date.now(),
      variant: "stepProgress",
      tone: "info",
      text: pendingPreview(18),
    });
  }, [store]);
  return null;
}

function Harness(): React.ReactElement {
  return createElement(
    AgentStoreProvider,
    { session: SESSION },
    createElement(
      ChatScrollProvider,
      null,
      createElement(CardStream),
      createElement(EmitEditResultBurst),
    ),
  );
}

async function waitForReactWork(): Promise<void> {
  for (let i = 0; i < 8; i++) {
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
}

describe("CardStream edit result bursts", () => {
  it("renders many edit_file and multi_edit result cards without nested update overflow", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    const mounted = render(createElement(Harness));

    await waitForReactWork();

    expect(consoleError.mock.calls.flat().join("\n")).not.toContain("Maximum update depth");
    expect(mounted.lastFrame()).toContain("pending edit block");

    mounted.unmount();
    consoleError.mockRestore();
  });
});
