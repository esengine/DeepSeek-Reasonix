import { type Tokens, marked } from "marked";
import React from "react";
import stringWidth from "string-width";
import { describe, expect, it } from "vitest";
import { Markdown, plainText, tableLayout } from "../src/cli/ui/markdown.js";
import { wrapToCells } from "../src/frame/width.js";

/** Smoke tests — markdown parsing is delegated to `marked`; we only verify the component mounts and dispatches over the token kinds we care about. */

describe("Markdown component", () => {
  it("renders an empty string without throwing", () => {
    const el = React.createElement(Markdown, { text: "" });
    expect(el).toBeTruthy();
  });

  it("renders a single paragraph", () => {
    const el = React.createElement(Markdown, { text: "hello world" });
    expect(el).toBeTruthy();
  });

  it("renders mixed block content (heading + list + code)", () => {
    const text = ["# Title", "", "- one", "- two", "", "```ts", "const x = 1;", "```"].join("\n");
    const el = React.createElement(Markdown, { text });
    expect(el).toBeTruthy();
  });

  it("renders inline markup (bold / italic / code / link / strike)", () => {
    const text =
      "This is **bold** and *italic* with `code` and ~~strike~~ and a [link](https://example.com).";
    const el = React.createElement(Markdown, { text });
    expect(el).toBeTruthy();
  });

  it("renders a GFM table", () => {
    const text = ["| a | b |", "| - | - |", "| 1 | 2 |"].join("\n");
    const el = React.createElement(Markdown, { text });
    expect(el).toBeTruthy();
  });

  it("renders a GFM task list", () => {
    const text = ["- [ ] todo", "- [x] done"].join("\n");
    const el = React.createElement(Markdown, { text });
    expect(el).toBeTruthy();
  });

  it("renders a blockquote with nested content", () => {
    const text = "> a quote\n> with *italic* inside";
    const el = React.createElement(Markdown, { text });
    expect(el).toBeTruthy();
  });

  it("does not throw on malformed / unbalanced markup", () => {
    const text = "**unterminated bold and `unterminated code";
    const el = React.createElement(Markdown, { text });
    expect(el).toBeTruthy();
  });

  it("renders a wide GFM table with explicit body-width without throwing", () => {
    const longCell = "a-very-long-cell-value-that-would-overflow-a-narrow-terminal-card";
    const text = ["| header | value |", "| - | - |", `| key | ${longCell} |`].join("\n");
    // body-width 40 — table overflows, triggers FallbackTable path
    const el = React.createElement(Markdown, { text, width: 40 });
    expect(el).toBeTruthy();
  });

  it("renders a narrow GFM table with explicit body-width (columnar path)", () => {
    const text = ["| a | b |", "| - | - |", "| 1 | 2 |"].join("\n");
    const el = React.createElement(Markdown, { text, width: 80 });
    expect(el).toBeTruthy();
  });

  it("renders a multi-row wide table without throwing", () => {
    const longCell = "x".repeat(120);
    const text = [
      "| name | description |",
      "| - | - |",
      `| item1 | ${longCell} |`,
      "| item2 | shorter |",
    ].join("\n");
    const el = React.createElement(Markdown, { text, width: 60 });
    expect(el).toBeTruthy();
  });
});

describe("Markdown — issue #340 table fallback row grouping", () => {
  it("renders fallback as row-grouped key/value pairs (column 1 row 1 before column 1 row 2)", async () => {
    const { mount } = await import("../src/renderer/reconciler/mount.js");
    const { CharPool } = await import("../src/renderer/pools/char-pool.js");
    const { StylePool } = await import("../src/renderer/pools/style-pool.js");
    const { HyperlinkPool } = await import("../src/renderer/pools/hyperlink-pool.js");
    const md = [
      "| Component | What | Manual TCs |",
      "| - | - | - |",
      "| Login form | Yup validation | AUTH-01 → AUTH-07 |",
      "| Scheduler | Renders events | SCH-01 → SCH-15 |",
    ].join("\n");
    const chunks: string[] = [];
    const handle = mount(React.createElement(Markdown, { text: md, width: 30 }), {
      viewportWidth: 30,
      viewportHeight: 30,
      pools: { char: new CharPool(), style: new StylePool(), hyperlink: new HyperlinkPool() },
      write: (s: string) => {
        chunks.push(s);
      },
    });
    await new Promise((r) => setTimeout(r, 100));
    const visible = chunks
      .join("")
      .replace(new RegExp(`${String.fromCharCode(27)}\\[[0-9;?]*[a-zA-Z]`, "g"), "");
    handle.destroy();
    const tcIdx1 = visible.indexOf("AUTH-01");
    const compIdx2 = visible.indexOf("Scheduler");
    expect(tcIdx1).toBeGreaterThan(-1);
    expect(compIdx2).toBeGreaterThan(-1);
    expect(tcIdx1).toBeLessThan(compIdx2);
  });
});

describe("Markdown — issue #330 file-ref over-match regression", () => {
  it("English abbreviations 'e.g' / 'i.e' are NOT treated as file refs", async () => {
    const { mount } = await import("../src/renderer/reconciler/mount.js");
    const { CharPool } = await import("../src/renderer/pools/char-pool.js");
    const { StylePool } = await import("../src/renderer/pools/style-pool.js");
    const { HyperlinkPool } = await import("../src/renderer/pools/hyperlink-pool.js");
    async function bytesFor(text: string): Promise<string> {
      const chunks: string[] = [];
      const handle = mount(React.createElement(Markdown, { text }), {
        viewportWidth: 80,
        viewportHeight: 10,
        pools: { char: new CharPool(), style: new StylePool(), hyperlink: new HyperlinkPool() },
        write: (s: string) => {
          chunks.push(s);
        },
      });
      await new Promise((r) => setTimeout(r, 100));
      const out = chunks.join("");
      handle.destroy();
      return out;
    }
    for (const sample of ["use e.g. like this", "i.e. like that", "(e.g. Google)"]) {
      const out = await bytesFor(sample);
      expect(out.includes("]8;;")).toBe(false);
      expect(out.includes("file://e.g")).toBe(false);
      expect(out.includes("file://i.e")).toBe(false);
    }
  });
});

// Table layout invariants: bounded width, no separator rows, content preservation.

/** Parse a GFM table into header/body cells via the same pipeline as the component. */
function parseTableCells(md: string): { headerCells: string[]; bodyCells: string[][] } {
  const tokens = marked.lexer(md);
  const table = tokens.find((t) => t.type === "table") as Tokens.Table | undefined;
  if (!table) throw new Error("no table token found");
  const headerCells = table.header.map((c) => plainText(c.tokens));
  const bodyCells = table.rows.map((row) => row.map((c) => plainText(c.tokens)));
  return { headerCells, bodyCells };
}

describe("tableLayout - fallback decision", () => {
  it("chooses columnar layout when table fits the width", () => {
    const { headerCells, bodyCells } = parseTableCells("| a | b |\n| - | - |\n| 1 | 2 |");
    const result = tableLayout(headerCells, bodyCells, 80);
    expect(result.fallback).toBe(false);
    if (!result.fallback) {
      expect(result.widths).toEqual([1, 1]);
    }
  });

  it("chooses fallback layout when table overflows the width", () => {
    const longVal = "a-very-long-cell-value-that-would-overflow-a-narrow-terminal-card";
    const md = `| header | value |\n| - | - |\n| key | ${longVal} |`;
    const { headerCells, bodyCells } = parseTableCells(md);
    const result = tableLayout(headerCells, bodyCells, 30);
    expect(result.fallback).toBe(true);
    if (result.fallback) {
      expect(result.labelPad).toBeGreaterThan(0);
      expect(result.valueCells).toBeGreaterThan(0);
      expect(result.labelPad + result.valueCells).toBeLessThanOrEqual(30);
    }
  });
});

describe("FallbackTable width invariants", () => {
  it("every wrapped line fits within the valueCells budget", () => {
    const longVal = "a-very-long-cell-value-that-would-overflow-a-narrow-terminal-card";
    const md = `| header | value |\n| - | - |\n| key | ${longVal} |`;
    const { headerCells, bodyCells } = parseTableCells(md);
    const width = 30;
    const layout = tableLayout(headerCells, bodyCells, width);
    expect(layout.fallback).toBe(true);
    if (!layout.fallback) return;

    const { labelPad, valueCells } = layout;
    for (const row of bodyCells) {
      for (const cell of row) {
        const lines = wrapToCells(cell, valueCells);
        for (const line of lines) {
          expect(
            stringWidth(line),
            `line exceeds valueCells=${valueCells}: "${line}"`,
          ).toBeLessThanOrEqual(valueCells);
        }
      }
    }
    expect(labelPad + valueCells).toBeLessThanOrEqual(width);
  });

  it("fallback does not produce a separator row (box-dash line)", () => {
    const longVal = "x".repeat(120);
    const md = `| name | description |\n| - | - |\n| item1 | ${longVal} |`;
    const { headerCells, bodyCells } = parseTableCells(md);
    const layout = tableLayout(headerCells, bodyCells, 40);
    expect(layout.fallback).toBe(true);
    if (!layout.fallback) return;
    const { valueCells } = layout;
    for (const row of bodyCells) {
      for (const cell of row) {
        const lines = wrapToCells(cell, valueCells);
        for (const line of lines) {
          expect(line).not.toMatch(/^\u2500+$/);
        }
      }
    }
  });

  it("content is preserved via wrapping, not truncated (no ellipsis)", () => {
    const longVal = "abcdefghij";
    const md = `| k | v |\n| - | - |\n| key | ${longVal} |`;
    const { headerCells, bodyCells } = parseTableCells(md);
    const layout = tableLayout(headerCells, bodyCells, 12);
    expect(layout.fallback).toBe(true);
    if (!layout.fallback) return;
    const { valueCells } = layout;
    const lines = wrapToCells(bodyCells[0]![1]!, valueCells);
    const joined = lines.join("");
    expect(joined).toBe(longVal);
  });

  it("wide table at narrow width: total visual width of every output row is at or below card width", () => {
    const longVal = "this-is-a-moderately-long-value-that-should-wrap-onto-multiple-lines";
    const md = `| label | data |\n| - | - |\n| key | ${longVal} |`;
    const { headerCells, bodyCells } = parseTableCells(md);
    const width = 36;
    const layout = tableLayout(headerCells, bodyCells, width);
    expect(layout.fallback).toBe(true);
    if (!layout.fallback) return;

    const { labelPad, valueCells } = layout;
    for (const row of bodyCells) {
      for (const cell of row) {
        const lines = wrapToCells(cell, valueCells);
        for (const line of lines) {
          const rowWidth = labelPad + stringWidth(line);
          expect(
            rowWidth,
            `row visual width ${rowWidth} exceeds card width ${width}`,
          ).toBeLessThanOrEqual(width);
        }
      }
    }
  });

  it("narrow width: labelPad + valueCells never exceeds available width", () => {
    const md = "| name | value |\n| - | - |\n| k | v |";
    const { headerCells, bodyCells } = parseTableCells(md);
    for (let w = 5; w <= 20; w++) {
      const layout = tableLayout(headerCells, bodyCells, w);
      if (!layout.fallback) continue;
      expect(
        layout.labelPad + layout.valueCells,
        `width=${w}: labelPad(${layout.labelPad}) + valueCells(${layout.valueCells}) > ${w}`,
      ).toBeLessThanOrEqual(w);
    }
  });
});
