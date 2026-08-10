// Run: tsx src/__tests__/markdown-table-virtual.test.tsx
//
// Oversized markdown tables (> MARKDOWN_TABLE_VIRTUAL_MIN_ROWS body rows)
// render through a row-virtualized table: the header stays intact and the
// mounted row count stays bounded by the viewport, while small tables render
// exactly as before. Content fidelity: every row is in the virtual model.

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { createComponents } from "../components/markdownComponents";
import { MarkdownTable, MARKDOWN_TABLE_VIRTUAL_MIN_ROWS, VirtualMarkdownSourceTable } from "../components/MarkdownTable";
import { hastBlockToJsx } from "../lib/hastJsx";
import { parseMarkdownToBlocks } from "../lib/markdownPipeline";

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

class NoopResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver = NoopResizeObserver as unknown as typeof ResizeObserver;
dom.window.ResizeObserver = NoopResizeObserver as unknown as typeof ResizeObserver;

// Rows measure at a fixed height; the scroll container reports its max-height
// box so the virtualizer has a real viewport (noop ResizeObserver never fires).
Object.defineProperty(dom.window.HTMLElement.prototype, "getBoundingClientRect", {
  configurable: true,
  value(this: HTMLElement) {
    return { x: 0, y: 0, width: 640, height: 36, top: 0, right: 640, bottom: 36, left: 0, toJSON: () => ({}) };
  },
});
Object.defineProperty(dom.window.HTMLElement.prototype, "offsetHeight", {
  configurable: true,
  get(this: HTMLElement) {
    return this.classList.contains("md-table-virtual") ? 480 : 36;
  },
});
Object.defineProperty(dom.window.HTMLElement.prototype, "offsetWidth", {
  configurable: true,
  get() {
    return 640;
  },
});

const flush = () => act(async () => {
  await new Promise((resolve) => setTimeout(resolve, 20));
});

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");

console.log("\nmarkdown table virtualization");

// ── oversized table: bounded mounted rows, intact header ─────────────────────
{
  const rowCount = MARKDOWN_TABLE_VIRTUAL_MIN_ROWS * 3; // 150 rows
  const root = createRoot(rootEl);
  await act(async () => {
    root.render(
      <MarkdownTable>
        <thead>
          <tr><th>name</th><th>value</th><th>note</th></tr>
        </thead>
        <tbody>
          {Array.from({ length: rowCount }, (_, i) => (
            <tr key={i}><td>row {i}</td><td>{i}</td><td>note {i}</td></tr>
          ))}
        </tbody>
      </MarkdownTable>,
    );
  });
  await flush();
  const scroller = rootEl.querySelector(".md-table-virtual");
  ok(scroller, "oversized table renders inside the virtual scroll container");
  const headerCells = rootEl.querySelectorAll(".md-table-virtual thead th");
  eq(headerCells.length, 3, "table header stays intact");
  eq(headerCells[0]?.textContent, "name", "header content is preserved");
  const mountedRows = rootEl.querySelectorAll(".md-table-virtual tbody tr");
  ok(mountedRows.length > 0, "some body rows mount");
  ok(mountedRows.length < rowCount, `mounted rows are bounded (${mountedRows.length} < ${rowCount})`);
  ok(mountedRows.length <= 60, "mounted row count tracks the viewport, not the model");
  const firstRow = rootEl.querySelector(".md-table-virtual tbody tr");
  eq(firstRow?.textContent, "row 00note 0", "the first row renders its cells");
  await act(async () => root.unmount());
}

// ── small table: untouched plain markup ──────────────────────────────────────
{
  const root = createRoot(rootEl);
  await act(async () => {
    root.render(
      <MarkdownTable>
        <thead>
          <tr><th>a</th></tr>
        </thead>
        <tbody>
          <tr><td>1</td></tr>
          <tr><td>2</td></tr>
        </tbody>
      </MarkdownTable>,
    );
  });
  await flush();
  ok(!rootEl.querySelector(".md-table-virtual"), "small tables render without virtualization");
  eq(rootEl.querySelectorAll("tbody tr").length, 2, "small tables mount every row");
  await act(async () => root.unmount());
}

// ── integration: a big markdown table routes through the components map ──────
{
  const header = "| name | value |\n| --- | --- |\n";
  const body = Array.from({ length: 120 }, (_, i) => `| row ${i} | ${i} |`).join("\n");
  const blocks = parseMarkdownToBlocks(header + body);
  ok(Boolean(blocks[0].virtualTable), "large plain table uses the lightweight worker representation");
  eq(blocks[0].virtualTable?.rows.length, 120, "lightweight table retains every source row");
  const root = createRoot(rootEl);
  await act(async () => {
    root.render(
      <div className="md">
        {blocks[0].virtualTable
          ? <VirtualMarkdownSourceTable data={blocks[0].virtualTable} />
          : hastBlockToJsx(blocks[0], createComponents(false))}
      </div>,
    );
  });
  await flush();
  ok(rootEl.querySelector(".md-table-virtual"), "markdown tables route through the virtual table override");
  const mounted = rootEl.querySelectorAll(".md-table-virtual tbody tr").length;
  ok(mounted > 0 && mounted < 120, `markdown table body is virtualized (${mounted} < 120)`);
  eq(rootEl.querySelector(".md-table-virtual thead th")?.textContent, "name", "markdown table header renders");
  await act(async () => root.unmount());
}

// Complex inline Markdown fails closed to the complete remark-gfm path.
{
  const header = "| name | value |\n| --- | --- |\n";
  const body = Array.from({ length: 60 }, (_, i) => `| **row ${i}** | ${i} |`).join("\n");
  const blocks = parseMarkdownToBlocks(header + body);
  ok(blocks.every((block) => !block.virtualTable), "complex large table stays on the semantic parser path");
  const root = createRoot(rootEl);
  await act(async () => {
    root.render(<div className="md">{hastBlockToJsx(blocks[0], createComponents(false))}</div>);
  });
  await flush();
  eq(rootEl.querySelector("tbody strong")?.textContent, "row 0", "complex table preserves inline Markdown");
  await act(async () => root.unmount());
}

dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
