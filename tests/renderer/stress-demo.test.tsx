// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import { StressShell } from "../../src/cli/commands/stress-demo.js";
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

describe("stress-demo — 4 concurrent live regions", () => {
  it("initial paint shows status, plan, shell, response, hint bar", async () => {
    const w = makeTestWriter();
    const handle = mount(<StressShell onExit={() => {}} />, {
      viewportWidth: 80,
      viewportHeight: 30,
      pools: pools(),
      write: w.write,
      stdin: makeFakeStdin(),
    });
    await flush();
    const out = w.output();
    expect(out).toContain("Reasonix");
    expect(out).toContain("Plan");
    expect(out).toContain("npm test");
    expect(out).toContain("4 concurrent live regions");
    handle.destroy();
  });

  it("after 1s, all four regions have ticked at least once (status time, plan spinner, shell lines, response chars)", async () => {
    const w = makeTestWriter();
    const handle = mount(<StressShell onExit={() => {}} />, {
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
    const out = w.output();
    expect(out.length).toBeGreaterThan(0);
    expect(out).toContain("PASS");
    expect(out).toContain("Working");
    handle.destroy();
  });

  it("ESC triggers onExit", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let exited = false;
    const handle = mount(
      <StressShell
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
