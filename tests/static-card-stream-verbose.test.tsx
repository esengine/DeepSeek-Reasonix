/** StaticCardStream must let verbose mode expand already-settled tool cards. */

import { render } from "ink-testing-library";
import { type ReactElement, createElement } from "react";
import { describe, expect, it } from "vitest";
import { StaticCardStream } from "../src/cli/ui/layout/StaticCardStream.js";
import type { ToolCard } from "../src/cli/ui/state/cards.js";
import { AgentStoreProvider } from "../src/cli/ui/state/provider.js";
import type { SessionInfo } from "../src/cli/ui/state/state.js";
import { VerboseContext } from "../src/cli/ui/state/verbose-context.js";

const SESSION: SessionInfo = {
  id: "session-1",
  branch: "main",
  workspace: "/tmp/repo",
  model: "deepseek-chat",
};

const OUTPUT = [
  "$ npm test",
  "[exit 1]",
  "> reasonix-node-assert-fixture@1.0.0 test",
  "> node test.mjs",
  "node:internal/modules/run_main:123",
  "    triggerUncaughtException(",
  "    ^",
  "",
  "AssertionError [ERR_ASSERTION]: VIP25 should reduce cart",
  "10200 !== 9000",
  "    at file:///repo/test.mjs:5:8",
  "    at ModuleJob.run (node:internal/modules/esm/module_job:343:25)",
  "actual: 10200",
  "expected: 9000",
  "operator: strictEqual",
  "}",
  "Node.js v22.22.0",
].join("\n");

const CARD: ToolCard = {
  id: "tool-1",
  ts: 0,
  kind: "tool",
  name: "run_command",
  args: "npm test",
  output: OUTPUT,
  done: true,
  exitCode: 1,
  elapsedMs: 410,
};

function Harness({ verbose }: { verbose: boolean }): ReactElement {
  return createElement(
    AgentStoreProvider,
    { session: SESSION, initialCards: [CARD] },
    createElement(VerboseContext.Provider, { value: verbose }, createElement(StaticCardStream)),
  );
}

describe("StaticCardStream verbose mode", () => {
  it("expands settled tool output after verbose mode is toggled on", () => {
    const { lastFrame, rerender, unmount } = render(createElement(Harness, { verbose: false }));
    expect(lastFrame()).toContain("hidden lines");

    rerender(createElement(Harness, { verbose: true }));
    const expanded = lastFrame() ?? "";

    expect(expanded).toContain("> reasonix-node-assert-fixture@1.0.0 test");
    expect(expanded).toContain("node:internal/modules/run_main:123");
    expect(expanded).not.toContain("hidden lines");

    rerender(createElement(Harness, { verbose: false }));
    expect(lastFrame()).toContain("hidden lines");
    unmount();
  });
});
