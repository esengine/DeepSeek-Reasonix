/**
 * Profiling harness for the TUI render path during streaming.
 *
 * Mounts CardStream against a fake stdout, dispatches a realistic event
 * stream (50 turns × user / tool / streaming chunks / tool result), and
 * lets Node's --cpu-prof flag dump a .cpuprofile next to the script.
 *
 * Run: node --cpu-prof --cpu-prof-dir=. --import tsx scripts/perf/profile-tui-streaming.tsx
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
const CHUNK_CHARS = 18;

const SAMPLE_REPLY_FRAGMENTS = [
  "Looking at the call site, the ",
  "function returns a Promise<Result> ",
  "where Result is the union of Success and Failure. ",
  "The Failure branch carries a `code` and a human message; ",
  "downstream callers should switch on the code, not ",
  "the message text. There's a related discussion in ",
  "the architecture doc — section 3.2 covers the error contract. ",
  "Want me to add a test that pins the shape? ",
];

function fragmentAt(i: number): string {
  return SAMPLE_REPLY_FRAGMENTS[i % SAMPLE_REPLY_FRAGMENTS.length]!.slice(0, CHUNK_CHARS);
}

const tick = (): Promise<void> => new Promise((r) => setImmediate(r));

function Driver({ done }: { done: () => void }): React.ReactElement {
  const store = useAgentStore();
  React.useEffect(() => {
    let aborted = false;
    const run = async () => {
      const dispatch = store.dispatch;
      const start = performance.now();
      let dispatches = 0;
      for (let t = 0; t < TURNS && !aborted; t++) {
        dispatch({ type: "user.submit", text: `turn ${t}: explore the renderer hot path` } as never);
        dispatches++;
        await tick();
        dispatch({ type: "turn.start", turnId: `t-${t}` } as never);
        dispatch({
          type: "tool.start",
          id: `tool-${t}-r`,
          name: "read_file",
          args: { path: `src/file-${t}.ts` },
        } as never);
        dispatches += 2;
        await tick();
        dispatch({
          type: "tool.end",
          id: `tool-${t}-r`,
          output: "ok",
          elapsedMs: 12,
        } as never);
        dispatches++;
        await tick();
        dispatch({ type: "streaming.start", id: `s-${t}` } as never);
        dispatches++;
        await tick();
        for (let c = 0; c < CHUNKS_PER_TURN && !aborted; c++) {
          dispatch({ type: "streaming.chunk", id: `s-${t}`, text: fragmentAt(c) } as never);
          dispatches++;
          await tick();
        }
        dispatch({ type: "streaming.end", id: `s-${t}` } as never);
        dispatch({
          type: "turn.end",
          usage: { prompt: 1000 + t * 50, reason: 0, output: 500, cacheHit: 0.85, cost: 0.0008 },
          promptCap: 1_000_000,
        } as never);
        dispatches += 2;
        await tick();
      }
      const elapsedMs = performance.now() - start;
      process.stdout.write(
        `[perf] ${dispatches} dispatches over ${elapsedMs.toFixed(1)}ms · ${(dispatches / (elapsedMs / 1000)).toFixed(0)} ev/s · ${(elapsedMs / dispatches).toFixed(2)} ms/ev\n`,
      );
      setTimeout(done, 50);
    };
    void run();
    return () => {
      aborted = true;
    };
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
    // unmount happens after `done()` resolves the outer promise.
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
