// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import { CharPool, HyperlinkPool, StylePool, inkCompat, mount } from "../../src/renderer/index.js";
import { makeTestWriter } from "../../src/renderer/runtime/test-writer.js";

function pools() {
  return { char: new CharPool(), style: new StylePool(), hyperlink: new HyperlinkPool() };
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

describe("mount — CJK and wide chars across diffs", () => {
  it("renders both CJK glyphs on initial paint", async () => {
    const w = makeTestWriter();
    const handle = mount(<inkCompat.Text>你好</inkCompat.Text>, {
      viewportWidth: 8,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    await flush();
    expect(w.output()).toContain("你");
    expect(w.output()).toContain("好");
    handle.destroy();
  });

  it("replacing 你好 with hi clears the SpacerTail — no ghost half-cells", async () => {
    const w = makeTestWriter();
    function App({ s }: { s: string }) {
      return <inkCompat.Text>{s}</inkCompat.Text>;
    }
    const handle = mount(<App s="你好" />, {
      viewportWidth: 8,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    await flush();
    w.flush();
    handle.update(<App s="hi" />);
    await flush();
    const after = w.output();
    expect(after).not.toContain("你");
    expect(after).not.toContain("好");
    expect(after).toContain("hi");
    handle.destroy();
  });

  it("growing narrow→wide writes the new wide char", async () => {
    const w = makeTestWriter();
    function App({ s }: { s: string }) {
      return <inkCompat.Text>{s}</inkCompat.Text>;
    }
    const handle = mount(<App s="ab" />, {
      viewportWidth: 8,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    await flush();
    w.flush();
    handle.update(<App s="你" />);
    await flush();
    expect(w.output()).toContain("你");
    handle.destroy();
  });
});

describe("mount — emoji rendering", () => {
  it("renders a single emoji", async () => {
    const w = makeTestWriter();
    const handle = mount(<inkCompat.Text>{"🎉 ok"}</inkCompat.Text>, {
      viewportWidth: 10,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    await flush();
    expect(w.output()).toContain("🎉");
    expect(w.output()).toContain("ok");
    handle.destroy();
  });

  it("multiple wide emoji in a row don't overlap", async () => {
    const w = makeTestWriter();
    const handle = mount(<inkCompat.Text>{"🎉🎉🎉"}</inkCompat.Text>, {
      viewportWidth: 10,
      viewportHeight: 1,
      pools: pools(),
      write: w.write,
    });
    await flush();
    const out = w.output();
    const matches = out.match(/🎉/g) ?? [];
    expect(matches.length).toBe(3);
    handle.destroy();
  });
});

describe("mount — wide char inside a styled Box", () => {
  it("CJK char rendered with bold + color emits a single SGR open/close around it", async () => {
    const w = makeTestWriter();
    const handle = mount(
      <inkCompat.Box>
        <inkCompat.Text bold color="cyan">
          前
        </inkCompat.Text>
      </inkCompat.Box>,
      {
        viewportWidth: 10,
        viewportHeight: 1,
        pools: pools(),
        write: w.write,
      },
    );
    await flush();
    const out = w.output();
    expect(out).toContain("前");
    expect(out).toContain("\x1b[1m");
    expect(out).toContain("\x1b[36m");
    handle.destroy();
  });
});

describe("mount — wrapping at viewport edge with wide chars", () => {
  it("wide char that doesn't fit on remaining width wraps to next row", async () => {
    const w = makeTestWriter();
    const handle = mount(<inkCompat.Text>{"abcde你好"}</inkCompat.Text>, {
      viewportWidth: 6,
      viewportHeight: 4,
      pools: pools(),
      write: w.write,
    });
    await flush();
    const out = w.output();
    expect(out).toContain("abcde");
    expect(out).toContain("你");
    expect(out).toContain("好");
    handle.destroy();
  });
});
