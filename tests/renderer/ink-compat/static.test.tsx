// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import { describe, expect, it } from "vitest";
import {
  CharPool,
  HyperlinkPool,
  StylePool,
  inkCompat,
  mount,
} from "../../../src/renderer/index.js";
import { renderToBytes } from "../../../src/renderer/runtime/render-to-bytes.js";
import { makeTestWriter } from "../../../src/renderer/runtime/test-writer.js";

function pools() {
  return { char: new CharPool(), style: new StylePool(), hyperlink: new HyperlinkPool() };
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

describe("renderToBytes — offline render to a flat string", () => {
  it("emits row content followed by trailing CRLF", () => {
    const out = renderToBytes(
      <inkCompat.Box flexDirection="column">
        <inkCompat.Text>row1</inkCompat.Text>
        <inkCompat.Text>row2</inkCompat.Text>
      </inkCompat.Box>,
      10,
      pools(),
    );
    expect(out).toContain("row1");
    expect(out).toContain("row2");
    expect(out).toMatch(/row1[^\n]*\r\n[^\n]*row2/);
    expect(out.endsWith("\r\n")).toBe(true);
  });

  it("empty element yields empty string", () => {
    expect(renderToBytes(null, 10, pools())).toBe("");
  });
});

describe("inkCompat.Static — appends rows to scrollback", () => {
  it("emits each new item batch via emitStatic before live patches", async () => {
    const w = makeTestWriter();
    function App({ items }: { items: ReadonlyArray<string> }) {
      return (
        <inkCompat.Box flexDirection="column">
          <inkCompat.Static items={items}>
            {(item) => <inkCompat.Text>{`static:${item}`}</inkCompat.Text>}
          </inkCompat.Static>
          <inkCompat.Text>live</inkCompat.Text>
        </inkCompat.Box>
      );
    }
    const handle = mount(<App items={["a", "b"]} />, {
      viewportWidth: 20,
      viewportHeight: 5,
      pools: pools(),
      write: w.write,
    });
    await flush();
    expect(w.output()).toContain("static:a");
    expect(w.output()).toContain("static:b");
    expect(w.output()).toContain("live");

    w.flush();
    handle.update(<App items={["a", "b", "c"]} />);
    await flush();
    const after = w.output();
    expect(after).toContain("static:c");
    expect(after).not.toContain("static:a");
    expect(after).not.toContain("static:b");
    expect(after).toContain("live");
    handle.destroy();
  });

  it("appended items only — never re-emits previously-emitted rows", async () => {
    const w = makeTestWriter();
    function App({ items }: { items: ReadonlyArray<number> }) {
      return (
        <inkCompat.Box flexDirection="column">
          <inkCompat.Static items={items}>
            {(n) => <inkCompat.Text>{`s${n}`}</inkCompat.Text>}
          </inkCompat.Static>
          <inkCompat.Text>L</inkCompat.Text>
        </inkCompat.Box>
      );
    }
    const handle = mount(<App items={[]} />, {
      viewportWidth: 10,
      viewportHeight: 3,
      pools: pools(),
      write: w.write,
    });
    await flush();
    w.flush();

    handle.update(<App items={[1]} />);
    await flush();
    expect(w.output()).toContain("s1");
    w.flush();

    handle.update(<App items={[1, 2]} />);
    await flush();
    const out2 = w.output();
    expect(out2).toContain("s2");
    expect(out2).not.toContain("s1");

    handle.destroy();
  });

  it("Static items reach the sink even when the live tree is empty", async () => {
    const w = makeTestWriter();
    function App({ items }: { items: ReadonlyArray<string> }) {
      return (
        <inkCompat.Static items={items}>
          {(item) => <inkCompat.Text>{item}</inkCompat.Text>}
        </inkCompat.Static>
      );
    }
    const handle = mount(<App items={["one"]} />, {
      viewportWidth: 10,
      viewportHeight: 3,
      pools: pools(),
      write: w.write,
    });
    await flush();
    expect(w.output()).toContain("one");
    handle.destroy();
  });

  it("interleaves with state-driven live updates", async () => {
    const w = makeTestWriter();
    let pushItem: (s: string) => void = () => {};
    let bumpCount: () => void = () => {};
    function App() {
      const [items, setItems] = useState<string[]>([]);
      const [count, setCount] = useState(0);
      pushItem = (s) => setItems((prev) => [...prev, s]);
      bumpCount = () => setCount((n) => n + 1);
      return (
        <inkCompat.Box flexDirection="column">
          <inkCompat.Static items={items}>
            {(it) => <inkCompat.Text>{`>${it}`}</inkCompat.Text>}
          </inkCompat.Static>
          <inkCompat.Text>{`count=${count}`}</inkCompat.Text>
        </inkCompat.Box>
      );
    }
    const handle = mount(<App />, {
      viewportWidth: 20,
      viewportHeight: 4,
      pools: pools(),
      write: w.write,
    });
    await flush();
    bumpCount();
    await flush();
    pushItem("alpha");
    await flush();
    w.flush();
    bumpCount();
    await flush();
    const after = w.output();
    expect(w.output().length).toBeGreaterThan(0);
    expect(after).toContain("2");
    handle.destroy();
  });
});
