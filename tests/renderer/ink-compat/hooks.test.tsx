// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import {
  AppContext,
  Text,
  useApp,
  useInput,
  useStdout,
} from "../../../src/renderer/ink-compat/index.js";
import type { KeystrokeSource } from "../../../src/renderer/input/index.js";
import { CharPool } from "../../../src/renderer/pools/char-pool.js";
import { HyperlinkPool } from "../../../src/renderer/pools/hyperlink-pool.js";
import { StylePool } from "../../../src/renderer/pools/style-pool.js";
import { mount } from "../../../src/renderer/reconciler/mount.js";
import { makeTestWriter } from "../../../src/renderer/runtime/test-writer.js";

function pools() {
  return {
    char: new CharPool(),
    style: new StylePool(),
    hyperlink: new HyperlinkPool(),
  };
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

function makeFakeStdin(): KeystrokeSource & { push: (s: string) => void } {
  let listener: ((chunk: string | Buffer) => void) | null = null;
  return {
    on(_event, cb) {
      listener = cb;
    },
    off(_event, _cb) {
      listener = null;
    },
    setRawMode() {},
    resume() {},
    pause() {},
    push(chunk: string) {
      listener?.(chunk);
    },
  };
}

describe("useStdout — viewport from mount/resize", () => {
  it("reads columns/rows from the mount viewport", async () => {
    const w = makeTestWriter();
    function Probe() {
      const { stdout } = useStdout();
      return <Text>{`cols=${stdout.columns} rows=${stdout.rows}`}</Text>;
    }
    const handle = mount(<Probe />, {
      viewportWidth: 42,
      viewportHeight: 7,
      pools: pools(),
      write: w.write,
    });
    await flush();
    expect(w.output()).toContain("cols=42 rows=7");
    handle.destroy();
  });

  it("re-renders with new size after resize()", async () => {
    const w = makeTestWriter();
    function Probe() {
      const { stdout } = useStdout();
      return <Text>{`cols=${stdout.columns}`}</Text>;
    }
    const handle = mount(<Probe />, {
      viewportWidth: 30,
      viewportHeight: 4,
      pools: pools(),
      write: w.write,
    });
    await flush();
    w.flush();
    handle.resize(80, 24);
    await flush();
    expect(w.output()).toContain("cols=80");
    handle.destroy();
  });

  // Regression for #249: \x1b[2J on Windows conhost scrolls content into
  // scrollback rather than erasing, so a later grow-resize pulls duplicate
  // input/toolbar rows back. resize() must clear in place via \x1b[J.
  it("resize uses in-place clear, not \\x1b[2J", async () => {
    const w = makeTestWriter();
    const handle = mount(<Text>hello</Text>, {
      viewportWidth: 40,
      viewportHeight: 8,
      pools: pools(),
      write: w.write,
    });
    await flush();
    w.flush();
    handle.resize(60, 16);
    await flush();
    const out = w.output();
    expect(out).not.toContain("\x1b[2J");
    expect(out).toContain("\x1b[J");
    handle.destroy();
  });
});

describe("useApp — exit via context", () => {
  it("invokes the AppContext.exit callback when called", async () => {
    const w = makeTestWriter();
    let exited = false;
    function App() {
      const { exit } = useApp();
      React.useEffect(() => {
        exit();
      }, [exit]);
      return <Text>app</Text>;
    }
    const handle = mount(
      <AppContext.Provider
        value={{
          exit: () => {
            exited = true;
          },
        }}
      >
        <App />
      </AppContext.Provider>,
      {
        viewportWidth: 10,
        viewportHeight: 1,
        pools: pools(),
        write: w.write,
      },
    );
    await flush();
    expect(exited).toBe(true);
    handle.destroy();
  });
});

describe("useInput — Ink-style input + key callback", () => {
  it("invokes (input, key) on each keystroke", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    const seen: Array<{ input: string; up: boolean; esc: boolean }> = [];
    function App() {
      useInput((input, key) => {
        seen.push({ input, up: key.upArrow, esc: key.escape });
      });
      return <Text>x</Text>;
    }
    const handle = mount(<App />, {
      viewportWidth: 10,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
      stdin,
    });
    await flush();
    stdin.push("a");
    stdin.push("\x1b[A");
    stdin.push("\x1b");
    await flush();
    expect(seen[0]).toEqual({ input: "a", up: false, esc: false });
    expect(seen[1]?.up).toBe(true);
    expect(seen[2]?.esc).toBe(true);
    handle.destroy();
  });

  it("isActive=false disables the handler", async () => {
    const w = makeTestWriter();
    const stdin = makeFakeStdin();
    let count = 0;
    function App() {
      useInput(
        () => {
          count++;
        },
        { isActive: false },
      );
      return <Text>x</Text>;
    }
    const handle = mount(<App />, {
      viewportWidth: 10,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
      stdin,
    });
    await flush();
    stdin.push("abc");
    await flush();
    expect(count).toBe(0);
    handle.destroy();
  });
});
