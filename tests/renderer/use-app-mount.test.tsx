// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useEffect } from "react";
import { describe, expect, it } from "vitest";
import { CharPool, HyperlinkPool, StylePool, inkCompat, mount } from "../../src/renderer/index.js";
import { makeTestWriter } from "../../src/renderer/runtime/test-writer.js";

function pools() {
  return { char: new CharPool(), style: new StylePool(), hyperlink: new HyperlinkPool() };
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

describe("mount — useApp().exit() routes through onExit", () => {
  it("exit() inside the tree triggers the onExit callback", async () => {
    const w = makeTestWriter();
    let exited = false;
    function App() {
      const { exit } = inkCompat.useApp();
      useEffect(() => {
        exit();
      }, [exit]);
      return <inkCompat.Text>x</inkCompat.Text>;
    }
    const handle = mount(<App />, {
      viewportWidth: 5,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
      onExit: () => {
        exited = true;
      },
    });
    await flush();
    await flush();
    expect(exited).toBe(true);
    handle.destroy();
  });

  it("exit(err) forwards the error to onExit", async () => {
    const w = makeTestWriter();
    let received: Error | undefined;
    function App() {
      const { exit } = inkCompat.useApp();
      useEffect(() => {
        exit(new Error("boom"));
      }, [exit]);
      return <inkCompat.Text>x</inkCompat.Text>;
    }
    const handle = mount(<App />, {
      viewportWidth: 5,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
      onExit: (err) => {
        received = err;
      },
    });
    await flush();
    await flush();
    expect(received?.message).toBe("boom");
    handle.destroy();
  });

  it("exit() after destroy is a no-op (no late onExit fire)", async () => {
    const w = makeTestWriter();
    let exitCalls = 0;
    let trigger: () => void = () => {};
    function App() {
      const { exit } = inkCompat.useApp();
      trigger = exit;
      return <inkCompat.Text>x</inkCompat.Text>;
    }
    const handle = mount(<App />, {
      viewportWidth: 5,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
      onExit: () => {
        exitCalls++;
      },
    });
    await flush();
    handle.destroy();
    trigger();
    await flush();
    await flush();
    expect(exitCalls).toBe(0);
  });

  it("exit() runs deferred — never fires synchronously inside a render", async () => {
    const w = makeTestWriter();
    const seen: string[] = [];
    function App() {
      const { exit } = inkCompat.useApp();
      seen.push("render");
      useEffect(() => {
        exit();
        seen.push("after-exit");
      }, [exit]);
      return <inkCompat.Text>x</inkCompat.Text>;
    }
    mount(<App />, {
      viewportWidth: 5,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
      onExit: () => {
        seen.push("onExit");
      },
    });
    await flush();
    await flush();
    expect(seen.indexOf("after-exit")).toBeLessThan(seen.indexOf("onExit"));
  });
});
