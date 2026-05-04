// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import {
  ChatV2Shell,
  DEMO_SESSION,
  type ScriptStep,
  makeCannedRunTurn,
} from "../../src/cli/commands/chat-v2.js";
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

/** Zero-delay reply builder so tests don't wait on real timers. */
function instantReply(userText: string, turn: number): ReadonlyArray<ScriptStep> {
  const reasonId = `r-${turn}`;
  const replyId = `s-${turn}`;
  return [
    { delayMs: 0, event: { type: "turn.start", turnId: `t-${turn}` } },
    { delayMs: 0, event: { type: "reasoning.start", id: reasonId } },
    { delayMs: 0, event: { type: "reasoning.chunk", id: reasonId, text: "thinking…" } },
    { delayMs: 0, event: { type: "reasoning.end", id: reasonId, paragraphs: 1, tokens: 5 } },
    { delayMs: 0, event: { type: "streaming.start", id: replyId } },
    { delayMs: 0, event: { type: "streaming.chunk", id: replyId, text: `echo: ${userText}` } },
    { delayMs: 0, event: { type: "streaming.end", id: replyId } },
    {
      delayMs: 0,
      event: {
        type: "turn.end",
        usage: { prompt: 10, reason: 5, output: 5, cacheHit: 0, cost: 0.0001 },
      },
    },
  ];
}

const instantRunTurn = makeCannedRunTurn(instantReply);

describe("chat-v2 shell — initial paint", () => {
  it("renders the header and the empty prompt", async () => {
    const w = makeTestWriter();
    const handle = mount(
      <AgentStoreProvider session={DEMO_SESSION}>
        <ChatV2Shell onExit={() => {}} />
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
    expect(out).toContain("type a message");
    handle.destroy();
  });
});

describe("chat-v2 shell — interactive submit", () => {
  it("typed text is submitted on Enter and the canned reply lands", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(
      <AgentStoreProvider session={DEMO_SESSION}>
        <ChatV2Shell onExit={() => {}} runTurn={instantRunTurn} />
      </AgentStoreProvider>,
      {
        viewportWidth: 80,
        viewportHeight: 24,
        pools: pools(),
        write: w.write,
        stdin,
      },
    );
    await flush();
    stdin.push("hello");
    await flush();
    stdin.push("\r");
    // Drain the zero-delay setTimeout chain (8 events × 1 microtask each).
    for (let i = 0; i < 30; i++) await flush();
    const out = w.output();
    expect(out).toContain("hello");
    expect(out).toContain("thinking");
    expect(out).toMatch(/echo/);
    handle.destroy();
  });

  it("two consecutive submits append two user cards", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(
      <AgentStoreProvider session={DEMO_SESSION}>
        <ChatV2Shell onExit={() => {}} runTurn={instantRunTurn} />
      </AgentStoreProvider>,
      {
        viewportWidth: 80,
        viewportHeight: 24,
        pools: pools(),
        write: w.write,
        stdin,
      },
    );
    await flush();
    stdin.push("first\r");
    for (let i = 0; i < 30; i++) await flush();
    stdin.push("second\r");
    for (let i = 0; i < 30; i++) await flush();
    const out = w.output();
    expect(out).toContain("first");
    expect(out).toContain("second");
    handle.destroy();
  });

  it("blank submit is a no-op", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let userSubmits = 0;
    const recordingRunTurn = makeCannedRunTurn((text, turn) => {
      userSubmits++;
      return instantReply(text, turn);
    });
    const handle = mount(
      <AgentStoreProvider session={DEMO_SESSION}>
        <ChatV2Shell onExit={() => {}} runTurn={recordingRunTurn} />
      </AgentStoreProvider>,
      {
        viewportWidth: 60,
        viewportHeight: 10,
        pools: pools(),
        write: w.write,
        stdin,
      },
    );
    await flush();
    stdin.push("\r");
    for (let i = 0; i < 5; i++) await flush();
    expect(userSubmits).toBe(0);
    handle.destroy();
  });
});

describe("chat-v2 shell — history navigation", () => {
  it("up arrow on the empty prompt recalls the most recent submission", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(
      <AgentStoreProvider session={DEMO_SESSION}>
        <ChatV2Shell onExit={() => {}} runTurn={instantRunTurn} />
      </AgentStoreProvider>,
      {
        viewportWidth: 80,
        viewportHeight: 24,
        pools: pools(),
        write: w.write,
        stdin,
      },
    );
    await flush();
    stdin.push("alpha\r");
    for (let i = 0; i < 30; i++) await flush();
    stdin.push("beta\r");
    for (let i = 0; i < 30; i++) await flush();
    // Both submissions should be in history; ↑ recalls "beta", ↑ again recalls "alpha".
    stdin.push("\x1b[A");
    await flush();
    stdin.push("\r");
    for (let i = 0; i < 30; i++) await flush();
    // The third submission should be "beta" (recalled). Easy assertion: a fourth
    // user.submit lands the text "beta" again.
    const out = w.output();
    // beta appears at least twice in the rendered output (original submission + recall).
    const occurrences = (out.match(/beta/g) ?? []).length;
    expect(occurrences).toBeGreaterThanOrEqual(2);
    handle.destroy();
  });
});

describe("chat-v2 shell — tool flow", () => {
  it("a runTurn that emits tool events produces a tool card", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const toolRunTurn = makeCannedRunTurn((userText, turn) => {
      const reasonId = `r-${turn}`;
      const replyId = `s-${turn}`;
      const toolId = `t-${turn}`;
      return [
        { delayMs: 0, event: { type: "turn.start", turnId: `t-${turn}` } },
        { delayMs: 0, event: { type: "reasoning.start", id: reasonId } },
        {
          delayMs: 0,
          event: { type: "reasoning.chunk", id: reasonId, text: "I'll list the directory." },
        },
        { delayMs: 0, event: { type: "reasoning.end", id: reasonId, paragraphs: 1, tokens: 5 } },
        {
          delayMs: 0,
          event: { type: "tool.start", id: toolId, name: "shell", args: { cmd: "ls" } },
        },
        {
          delayMs: 0,
          event: {
            type: "tool.end",
            id: toolId,
            output: "src/\nrenderer/\n",
            exitCode: 0,
            elapsedMs: 12,
          },
        },
        { delayMs: 0, event: { type: "streaming.start", id: replyId } },
        {
          delayMs: 0,
          event: { type: "streaming.chunk", id: replyId, text: `Listed ${userText}.` },
        },
        { delayMs: 0, event: { type: "streaming.end", id: replyId } },
        {
          delayMs: 0,
          event: {
            type: "turn.end",
            usage: { prompt: 10, reason: 5, output: 5, cacheHit: 0, cost: 0.0001 },
          },
        },
      ];
    });
    const handle = mount(
      <AgentStoreProvider session={DEMO_SESSION}>
        <ChatV2Shell onExit={() => {}} runTurn={toolRunTurn} />
      </AgentStoreProvider>,
      {
        viewportWidth: 80,
        viewportHeight: 30,
        pools: pools(),
        write: w.write,
        stdin,
      },
    );
    await flush();
    stdin.push("./somewhere\r");
    for (let i = 0; i < 30; i++) await flush();
    const out = w.output();
    expect(out).toContain("shell");
    expect(out).toContain("ok");
    expect(out).toMatch(/Listed/);
    handle.destroy();
  });
});

describe("chat-v2 shell — resumed session", () => {
  it("rendering with initialCards replays prior user turns immediately", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(
      <AgentStoreProvider
        session={DEMO_SESSION}
        initialCards={[
          { kind: "user", id: "u-1", ts: 1, text: "first question" },
          { kind: "user", id: "u-2", ts: 2, text: "second question" },
        ]}
      >
        <ChatV2Shell onExit={() => {}} runTurn={instantRunTurn} />
      </AgentStoreProvider>,
      {
        viewportWidth: 80,
        viewportHeight: 12,
        pools: pools(),
        write: w.write,
        stdin,
      },
    );
    await flush();
    const out = w.output();
    expect(out).toContain("first question");
    expect(out).toContain("second question");
    handle.destroy();
  });
});

describe("chat-v2 shell — long-conversation overflow", () => {
  it("settled cards from older turns appear in the byte stream as scrollback writes", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(
      <AgentStoreProvider session={DEMO_SESSION}>
        <ChatV2Shell onExit={() => {}} runTurn={instantRunTurn} />
      </AgentStoreProvider>,
      {
        // Tiny viewport — without Static promotion the live region would either
        // truncate older cards or thrash on every chunk. With Static, the
        // settled cards write to scrollback so the live region stays bounded.
        viewportWidth: 60,
        viewportHeight: 8,
        pools: pools(),
        write: w.write,
        stdin,
      },
    );
    await flush();
    for (const text of ["one", "two", "three", "four", "five"]) {
      stdin.push(`${text}\r`);
      for (let i = 0; i < 30; i++) await flush();
    }
    const out = w.output();
    // Each user submission must show up SOMEWHERE in the byte stream — even if
    // it scrolled past the live region's top. Static guarantees it landed in
    // scrollback rather than getting clipped.
    for (const text of ["one", "two", "three", "four", "five"]) {
      expect(out).toContain(text);
    }
    handle.destroy();
  });
});

describe("chat-v2 shell — exit", () => {
  it("Esc on the empty prompt invokes onExit", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let exited = false;
    const handle = mount(
      <AgentStoreProvider session={DEMO_SESSION}>
        <ChatV2Shell
          onExit={() => {
            exited = true;
          }}
          runTurn={instantRunTurn}
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

  it("Esc on a non-empty prompt clears the value but does NOT exit", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let exited = false;
    const handle = mount(
      <AgentStoreProvider session={DEMO_SESSION}>
        <ChatV2Shell
          onExit={() => {
            exited = true;
          }}
          runTurn={instantRunTurn}
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
    stdin.push("draft");
    await flush();
    stdin.push("\x1b");
    await flush();
    expect(exited).toBe(false);
    handle.destroy();
  });
});
