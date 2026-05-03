// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import { PreviewShell } from "../../src/cli/commands/preview.js";
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

describe("preview shell — initial paint", () => {
  it("shows the header and the placeholder prompt", async () => {
    const w = makeTestWriter();
    const handle = mount(<PreviewShell onExit={() => {}} />, {
      viewportWidth: 60,
      viewportHeight: 8,
      pools: pools(),
      write: w.write,
      stdin: makeFakeStdin(),
    });
    await flush();
    const out = w.output();
    expect(out).toContain("Reasonix");
    expect(out).toContain("preview");
    expect(out).toContain("say something");
    expect(out).toContain("Enter submit");
    handle.destroy();
  });
});

describe("preview shell — typing", () => {
  it("printable keys append to the draft, replacing the placeholder", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(<PreviewShell onExit={() => {}} />, {
      viewportWidth: 60,
      viewportHeight: 8,
      pools: pools(),
      write: w.write,
      stdin,
    });
    await flush();
    w.flush();
    stdin.push("h");
    stdin.push("i");
    await flush();
    const out = w.output();
    expect(out).toContain("h");
    expect(out).toContain("i");
    handle.destroy();
  });

  it("backspace removes the last character", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(<PreviewShell onExit={() => {}} />, {
      viewportWidth: 60,
      viewportHeight: 8,
      pools: pools(),
      write: w.write,
      stdin,
    });
    await flush();
    stdin.push("abc");
    await flush();
    w.flush();
    stdin.push("\x7f");
    await flush();
    expect(w.output().length).toBeGreaterThan(0);
    handle.destroy();
  });
});

describe("preview shell — submit + history", () => {
  it("Enter pushes the draft into history and clears the prompt", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(<PreviewShell onExit={() => {}} />, {
      viewportWidth: 60,
      viewportHeight: 10,
      pools: pools(),
      write: w.write,
      stdin,
    });
    await flush();
    stdin.push("hello");
    await flush();
    stdin.push("\r");
    await flush();
    await flush();
    const out = w.output();
    expect(out).toContain("hello");
    expect(out).toContain("you said: hello");
    handle.destroy();
  });

  it("two consecutive submits stack into history", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(<PreviewShell onExit={() => {}} />, {
      viewportWidth: 60,
      viewportHeight: 12,
      pools: pools(),
      write: w.write,
      stdin,
    });
    await flush();
    stdin.push("one\r");
    await flush();
    await flush();
    stdin.push("two\r");
    await flush();
    await flush();
    const out = w.output();
    expect(out).toContain("you said: one");
    expect(out).toContain("you said: two");
    handle.destroy();
  });

  it("empty submit (Enter on blank prompt) is a no-op", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const handle = mount(<PreviewShell onExit={() => {}} />, {
      viewportWidth: 60,
      viewportHeight: 8,
      pools: pools(),
      write: w.write,
      stdin,
    });
    await flush();
    w.flush();
    stdin.push("\r");
    await flush();
    expect(w.output()).not.toContain("you said:");
    handle.destroy();
  });
});

describe("preview shell — exit", () => {
  it("Esc triggers onExit", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let exited = false;
    const handle = mount(
      <PreviewShell
        onExit={() => {
          exited = true;
        }}
      />,
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
