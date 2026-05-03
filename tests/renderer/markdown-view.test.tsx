// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { describe, expect, it } from "vitest";
import { MarkdownView } from "../../src/cli/ui/markdown-view.js";
import { CharPool, HyperlinkPool, StylePool, mount } from "../../src/renderer/index.js";
import { makeTestWriter } from "../../src/renderer/runtime/test-writer.js";

function pools() {
  return { char: new CharPool(), style: new StylePool(), hyperlink: new HyperlinkPool() };
}

function flush(): Promise<void> {
  return new Promise((r) => setTimeout(r, 0));
}

async function render(text: string, width = 60, height = 20): Promise<string> {
  const w = makeTestWriter();
  const handle = mount(<MarkdownView text={text} />, {
    viewportWidth: width,
    viewportHeight: height,
    pools: pools(),
    write: w.write,
  });
  await flush();
  const out = w.output();
  handle.destroy();
  return out;
}

describe("MarkdownView — through cell-diff renderer", () => {
  it("renders a heading + paragraph", async () => {
    const out = await render("# Title\n\nbody text");
    expect(out).toContain("Title");
    expect(out).toContain("body text");
  });

  it("renders ordered + unordered list markers", async () => {
    const out = await render("- alpha\n- beta\n\n1. first\n2. second");
    expect(out).toContain("alpha");
    expect(out).toContain("beta");
    expect(out).toContain("first");
    expect(out).toContain("second");
    expect(out).toMatch(/[·•]/);
    expect(out).toContain("1.");
  });

  it("task list shows checked / unchecked glyphs", async () => {
    const out = await render("- [x] done item\n- [ ] todo item");
    expect(out).toContain("done item");
    expect(out).toContain("todo item");
    expect(out).toContain("✓");
    expect(out).toContain("○");
  });

  it("renders a fenced code block with the lang label", async () => {
    const out = await render("```ts\nconst x = 1;\n```");
    expect(out).toContain("const x = 1;");
    expect(out).toContain("ts");
  });

  it("inline bold + italic + code emit SGR codes", async () => {
    const out = await render("plain **bold** *italic* `code`");
    expect(out).toContain("bold");
    expect(out).toContain("italic");
    expect(out).toContain("code");
    expect(out).toContain("\x1b[1m");
    expect(out).toContain("\x1b[3m");
  });

  it("file refs render with hyperlink underline", async () => {
    const out = await render("see src/foo.ts:42");
    expect(out).toContain("src/foo.ts:42");
    expect(out).toContain("\x1b[4m");
  });

  it("blockquote renders with the leading bar glyph", async () => {
    const out = await render("> a quoted line");
    expect(out).toContain("a quoted line");
    expect(out).toContain("▎");
  });

  it("horizontal rule renders as a row of em-dashes", async () => {
    const out = await render("before\n\n---\n\nafter");
    expect(out).toContain("before");
    expect(out).toContain("after");
    expect(out).toContain("─");
  });
});
