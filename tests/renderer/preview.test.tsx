// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it, vi } from "vitest";
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

describe("preview shell — submit + streaming", () => {
  it("Enter starts a streaming spinner; the echo lands after the stream completes", async () => {
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
    stdin.push("hello\r");
    await flush();
    await flush();
    const mid = w.output();
    expect(mid).toContain("thinking");
    expect(mid).not.toContain("you said: hello");

    await new Promise((r) => setTimeout(r, 2500));
    await flush();
    const out = w.output();
    expect(out).toContain("you said: hello");
    handle.destroy();
  }, 8000);

  it("input is paused while streaming — keystrokes during the stream are dropped", async () => {
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
    stdin.push("first\r");
    await flush();
    await flush();
    stdin.push("X");
    await flush();
    await new Promise((r) => setTimeout(r, 2500));
    await flush();
    const after = w.output();
    expect(after).toContain("you said: first");
    expect(after).not.toContain("you said: firstX");
    handle.destroy();
  }, 8000);

  it("empty submit is a no-op (no streaming starts)", async () => {
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
    const out = w.output();
    expect(out).not.toContain("you said:");
    expect(out).not.toContain("thinking");
    handle.destroy();
  });
});

describe("preview shell — slash commands", () => {
  it("/help lists available commands", async () => {
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
    stdin.push("/help\r");
    await flush();
    await flush();
    const out = w.output();
    expect(out).toContain("available commands");
    expect(out).toContain("/help");
    expect(out).toContain("/clear");
    expect(out).toContain("/exit");
    handle.destroy();
  });

  it("/clear empties the history", async () => {
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
    stdin.push("/help\r");
    await flush();
    await flush();
    stdin.push("/clear\r");
    await flush();
    await flush();
    w.flush();
    stdin.push("/help\r");
    await flush();
    await flush();
    const after = w.output();
    expect(after).toContain("available commands");
    handle.destroy();
  });

  it("/exit triggers onExit", async () => {
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
        viewportHeight: 12,
        pools: pools(),
        write: w.write,
        stdin,
      },
    );
    await flush();
    stdin.push("/exit\r");
    await flush();
    expect(exited).toBe(true);
    handle.destroy();
  });

  it("unknown / command renders an error row", async () => {
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
    stdin.push("/banana\r");
    await flush();
    await flush();
    const out = w.output();
    expect(out).toContain("unknown command");
    expect(out).toContain("/banana");
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
