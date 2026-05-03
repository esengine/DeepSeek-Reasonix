// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import { CardDemoShell } from "../../src/cli/commands/card-demo.js";
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

describe("card-demo — chat lifecycle", () => {
  it("initial paint shows status row + prompt + hint bar", async () => {
    const w = makeTestWriter();
    const handle = mount(<CardDemoShell onExit={() => {}} />, {
      viewportWidth: 80,
      viewportHeight: 30,
      pools: pools(),
      write: w.write,
      stdin: makeFakeStdin(),
    });
    await flush();
    const out = w.output();
    expect(out).toContain("Reasonix");
    expect(out).toContain("type your question");
    expect(out).toContain("auto-replays");
    handle.destroy();
  });

  it("after 1s, the demo is producing output (cards animating)", async () => {
    const w = makeTestWriter();
    const handle = mount(<CardDemoShell onExit={() => {}} />, {
      viewportWidth: 80,
      viewportHeight: 30,
      pools: pools(),
      write: w.write,
      stdin: makeFakeStdin(),
    });
    await flush();
    w.flush();
    await new Promise((r) => setTimeout(r, 1000));
    await flush();
    expect(w.output().length).toBeGreaterThan(0);
    handle.destroy();
  }, 4000);

  it("ESC triggers onExit", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let exited = false;
    const handle = mount(
      <CardDemoShell
        onExit={() => {
          exited = true;
        }}
      />,
      {
        viewportWidth: 80,
        viewportHeight: 30,
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
