/**
 * Synchronous variant of the TUI streaming profile: drives all dispatches
 * in a single tick to expose the React reconciler + reducer cost without
 * Ink's frame throttle. CPU profile dumps next to the script.
 */

import { render } from "ink";
import React from "react";
import { CardStream } from "../../src/cli/ui/layout/CardStream.js";
import { ChatScrollProvider } from "../../src/cli/ui/state/chat-scroll-provider.js";
import { AgentStoreProvider, useAgentStore } from "../../src/cli/ui/state/provider.js";
import type { SessionInfo } from "../../src/cli/ui/state/state.js";
import { makeFakeStdin, makeFakeStdout } from "../../tests/helpers/ink-stdio.js";

const SESSION: SessionInfo = {
  id: "perf",
  branch: "main",
  workspace: "/tmp/perf",
  model: "deepseek-v4-flash",
};

const TURNS = 50;
const CHUNKS_PER_TURN = 30;
const CHUNK = "function returns Promise<Result> ";

function Driver({ done }: { done: () => void }): React.ReactElement {
  const store = useAgentStore();
  React.useEffect(() => {
    const dispatch = store.dispatch;
    const start = performance.now();
    let n = 0;
    for (let t = 0; t < TURNS; t++) {
      dispatch({ type: "user.submit", text: `turn ${t}` } as never);
      dispatch({ type: "turn.start", turnId: `t-${t}` } as never);
      dispatch({
        type: "tool.start",
        id: `tool-${t}`,
        name: "read_file",
        args: { path: `f-${t}.ts` },
      } as never);
      dispatch({ type: "tool.end", id: `tool-${t}`, output: "ok", elapsedMs: 12 } as never);
      dispatch({ type: "streaming.start", id: `s-${t}` } as never);
      for (let c = 0; c < CHUNKS_PER_TURN; c++) {
        dispatch({ type: "streaming.chunk", id: `s-${t}`, text: CHUNK } as never);
      }
      dispatch({ type: "streaming.end", id: `s-${t}` } as never);
      dispatch({
        type: "turn.end",
        usage: { prompt: 1000, reason: 0, output: 500, cacheHit: 0.85, cost: 0.001 },
        promptCap: 1_000_000,
      } as never);
      n += 6 + CHUNKS_PER_TURN + 1;
    }
    const ms = performance.now() - start;
    process.stdout.write(
      `[perf-sync] ${n} dispatches in ${ms.toFixed(1)}ms · ${(n / (ms / 1000)).toFixed(0)} ev/s · ${(ms / n).toFixed(3)} ms/ev\n`,
    );
    setTimeout(done, 200);
  }, [store]);
  return React.createElement(CardStream, null);
}

async function main(): Promise<void> {
  const stdout = makeFakeStdout();
  await new Promise<void>((resolve) => {
    const { unmount } = render(
      React.createElement(
        AgentStoreProvider,
        { session: SESSION },
        React.createElement(
          ChatScrollProvider,
          null,
          React.createElement(Driver, { done: () => resolve() }),
        ),
      ),
      { stdout: stdout as never, stdin: makeFakeStdin() as never },
    );
    void unmount;
  });
}

main().then(
  () => process.exit(0),
  (e) => {
    console.error(e);
    process.exit(1);
  },
);
