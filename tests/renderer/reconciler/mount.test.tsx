// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useEffect, useState } from "react";
import { describe, expect, it } from "vitest";
import { CharPool } from "../../../src/renderer/pools/char-pool.js";
import { HyperlinkPool } from "../../../src/renderer/pools/hyperlink-pool.js";
import { type AnsiCode, StylePool } from "../../../src/renderer/pools/style-pool.js";
import { Box, Text } from "../../../src/renderer/react/components.js";
import { mount } from "../../../src/renderer/reconciler/mount.js";
import { makeTestWriter } from "../../../src/renderer/runtime/test-writer.js";

const RED: AnsiCode = { apply: "\x1b[31m", revert: "\x1b[39m" };

function pools() {
  return {
    char: new CharPool(),
    style: new StylePool(),
    hyperlink: new HyperlinkPool(),
  };
}

function flush(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

describe("mount — initial render", () => {
  it("paints the initial element via the reconciler", async () => {
    const w = makeTestWriter();
    const handle = mount(<Text>hi</Text>, {
      viewportWidth: 5,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    await flush();
    expect(w.output()).toContain("hi");
    handle.destroy();
  });

  it("renders Box composition", async () => {
    const w = makeTestWriter();
    const handle = mount(
      <Box flexDirection="row">
        <Text>L</Text>
        <Text>R</Text>
      </Box>,
      { viewportWidth: 10, viewportHeight: 1, pools: pools(), write: w.write },
    );
    await flush();
    const out = w.output();
    expect(out).toContain("L");
    expect(out).toContain("R");
    handle.destroy();
  });
});

describe("mount — explicit update()", () => {
  it("rendering the same tree again writes nothing", async () => {
    const w = makeTestWriter();
    const handle = mount(<Text>same</Text>, {
      viewportWidth: 5,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    await flush();
    w.flush();
    handle.update(<Text>same</Text>);
    await flush();
    expect(w.output()).toBe("");
    handle.destroy();
  });

  it("rendering a changed tree writes only the diff", async () => {
    const w = makeTestWriter();
    const handle = mount(<Text>aaaa</Text>, {
      viewportWidth: 5,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    await flush();
    w.flush();
    handle.update(<Text>aaba</Text>);
    await flush();
    const out = w.output();
    expect(out).toContain("b");
    expect(out).not.toContain("a");
    handle.destroy();
  });
});

describe("mount — useState drives re-render", () => {
  it("a setState call triggers a paint without the caller calling update", async () => {
    const w = makeTestWriter();
    let setCount: ((n: number) => void) | null = null;
    function Counter() {
      const [count, setC] = useState(0);
      setCount = setC;
      return <Text>{`n=${count}`}</Text>;
    }
    const handle = mount(<Counter />, {
      viewportWidth: 10,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    await flush();
    expect(w.output()).toContain("n=0");
    w.flush();
    setCount?.(7);
    await flush();
    expect(w.output()).toContain("7");
    handle.destroy();
  });

  it("useEffect runs and its setState triggers a second paint", async () => {
    const w = makeTestWriter();
    function Async() {
      const [v, setV] = useState("init");
      useEffect(() => {
        setV("done");
      }, []);
      return <Text>{v}</Text>;
    }
    const handle = mount(<Async />, {
      viewportWidth: 10,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    await flush();
    expect(w.output()).toContain("done");
    handle.destroy();
  });
});

describe("mount — destroy", () => {
  it("emits SGR reset on destroy", async () => {
    const w = makeTestWriter();
    const handle = mount(<Text>x</Text>, {
      viewportWidth: 5,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    await flush();
    w.flush();
    handle.destroy();
    expect(w.output()).toContain("\x1b[0m");
  });

  it("update after destroy is a no-op", async () => {
    const w = makeTestWriter();
    const handle = mount(<Text>x</Text>, {
      viewportWidth: 5,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    await flush();
    handle.destroy();
    w.flush();
    handle.update(<Text>ignored</Text>);
    await flush();
    expect(w.output()).toBe("");
  });
});

describe("mount — style pass-through", () => {
  it("RED text writes the ANSI sequence to the byte stream", async () => {
    const w = makeTestWriter();
    const handle = mount(<Text style={[RED]}>x</Text>, {
      viewportWidth: 3,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    await flush();
    expect(w.output()).toContain("\x1b[31m");
    handle.destroy();
  });
});
