/** StaticCardStream must let verbose mode expand already-settled tool cards. */

import { type ComponentType, type ReactElement, createElement } from "react";
import { describe, expect, it, vi } from "vitest";
import { StaticCardStream } from "../src/cli/ui/layout/StaticCardStream.js";
import type { ToolCard, UserCard } from "../src/cli/ui/state/cards.js";
import { AgentStoreProvider } from "../src/cli/ui/state/provider.js";
import type { SessionInfo } from "../src/cli/ui/state/state.js";
import { VerboseContext } from "../src/cli/ui/state/verbose-context.js";
import { render } from "./helpers/ink-test.js";

const staticRenderSpy = vi.hoisted(() => vi.fn());

vi.mock("ink", async () => {
  const actual = await vi.importActual<typeof import("ink")>("ink");
  const React = await vi.importActual<typeof import("react")>("react");
  return {
    ...actual,
    Static: (props: Record<string, unknown>) => {
      staticRenderSpy();
      return React.createElement(actual.Static as ComponentType<Record<string, unknown>>, props);
    },
  };
});

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

const USER_CARD: UserCard = {
  id: "user-1",
  ts: 0,
  kind: "user",
  text: "plain input",
};

function Harness({ verbose }: { verbose: boolean }): ReactElement {
  return createElement(
    AgentStoreProvider,
    { session: SESSION, initialCards: [CARD] },
    createElement(VerboseContext.Provider, { value: verbose }, createElement(StaticCardStream)),
  );
}

function ParentUpdateHarness({ revision }: { revision: number }): ReactElement {
  void revision;
  return createElement(
    AgentStoreProvider,
    { session: SESSION, initialCards: [USER_CARD] },
    createElement(StaticCardStream),
  );
}

describe("StaticCardStream render isolation", () => {
  it("does not re-run static history rendering when only the parent updates", () => {
    staticRenderSpy.mockClear();
    const { rerender, unmount } = render(createElement(ParentUpdateHarness, { revision: 0 }));
    const renderCountAfterMount = staticRenderSpy.mock.calls.length;
    expect(renderCountAfterMount).toBeGreaterThan(0);

    rerender(createElement(ParentUpdateHarness, { revision: 1 }));

    expect(staticRenderSpy.mock.calls.length).toBe(renderCountAfterMount);
    unmount();
  });
});
