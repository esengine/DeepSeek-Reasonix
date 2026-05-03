// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import { ChatV2Shell, DEMO_SESSION, type ScriptStep } from "../../src/cli/commands/chat-v2.js";
import { AgentStoreProvider } from "../../src/cli/ui/state/provider.js";
import {
  CharPool,
  HyperlinkPool,
  type KeystrokeSource,
  StylePool,
  mount,
} from "../../src/renderer/index.js";
import { makeTestWriter } from "../../src/renderer/runtime/test-writer.js";

function pools() {
  return { char: new CharPool(), style: new StylePool(), hyperlink: new HyperlinkPool() };
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

function makeFakeStdin(): KeystrokeSource & { push: (s: string) => void } {
  let listener: ((c: string | Buffer) => void) | null = null;
  return {
    on(_e, cb) {
      listener = cb;
    },
    off() {
      listener = null;
    },
    setRawMode() {},
    resume() {},
    pause() {},
    push(c: string) {
      listener?.(c);
    },
  };
}

const FAST_SCRIPT: ReadonlyArray<ScriptStep> = [
  { delayMs: 0, event: { type: "user.submit", text: "hello chat-v2" } },
  { delayMs: 0, event: { type: "turn.start", turnId: "t-1" } },
  { delayMs: 0, event: { type: "reasoning.start", id: "r-1" } },
  { delayMs: 0, event: { type: "reasoning.chunk", id: "r-1", text: "thinking-line" } },
  { delayMs: 0, event: { type: "reasoning.end", id: "r-1", paragraphs: 1, tokens: 7 } },
  { delayMs: 0, event: { type: "streaming.start", id: "s-1" } },
  { delayMs: 0, event: { type: "streaming.chunk", id: "s-1", text: "answer-line" } },
  { delayMs: 0, event: { type: "streaming.end", id: "s-1" } },
  {
    delayMs: 0,
    event: {
      type: "turn.end",
      usage: { prompt: 100, reason: 7, output: 11, cacheHit: 0.4, cost: 0.0001 },
    },
  },
];

describe("chat-v2 shell — initial paint", () => {
  it("renders the header before the script plays", async () => {
    const w = makeTestWriter();
    const handle = mount(
      <AgentStoreProvider session={DEMO_SESSION}>
        <ChatV2Shell script={[]} onExit={() => {}} />
      </AgentStoreProvider>,
      {
        viewportWidth: 80,
        viewportHeight: 12,
        pools: pools(),
        write: w.write,
        stdin: makeFakeStdin(),
      },
    );
    await flush();
    const out = w.output();
    expect(out).toContain("Reasonix");
    expect(out).toContain("chat-v2");
    expect(out).toContain("Esc");
    handle.destroy();
  });
});

describe("chat-v2 shell — playback through the real reducer", () => {
  it("dispatched events flow through useAgentState into rendered card rows", async () => {
    const w = makeTestWriter();
    const handle = mount(
      <AgentStoreProvider session={DEMO_SESSION}>
        <ChatV2Shell script={FAST_SCRIPT} onExit={() => {}} />
      </AgentStoreProvider>,
      {
        viewportWidth: 80,
        viewportHeight: 24,
        pools: pools(),
        write: w.write,
        stdin: makeFakeStdin(),
      },
    );
    // FAST_SCRIPT has zero-delay setTimeout chains; flush enough microtask
    // ticks that the whole sequence drains.
    for (let i = 0; i < 30; i++) await flush();
    const out = w.output();
    expect(out).toContain("hello chat-v2");
    expect(out).toContain("thinking-line");
    expect(out).toContain("answer-line");
    expect(out).toContain("end of demo");
    handle.destroy();
  });
});

describe("chat-v2 shell — exit", () => {
  it("Esc invokes onExit", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let exited = false;
    const handle = mount(
      <AgentStoreProvider session={DEMO_SESSION}>
        <ChatV2Shell
          script={[]}
          onExit={() => {
            exited = true;
          }}
        />
      </AgentStoreProvider>,
      {
        viewportWidth: 60,
        viewportHeight: 8,
        pools: pools(),
        write: w.write,
        stdin,
      },
    );
    await flush();
    stdin.push("\x1b");
    await flush();
    expect(exited).toBe(true);
    handle.destroy();
  });
});
