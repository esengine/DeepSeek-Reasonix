// Run: tsx src/__tests__/virtual-menu-identity.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { SlashMenu } from "../components/SlashMenu";
import { LocaleProvider } from "../lib/i18n";
import type { CommandInfo } from "../lib/types";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function rect(height: number): DOMRect {
  return {
    left: 0,
    top: 0,
    right: 600,
    bottom: height,
    width: 600,
    height,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  } as DOMRect;
}

class TestResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});

(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Node = dom.window.Node;
globalThis.ResizeObserver = TestResizeObserver as unknown as typeof ResizeObserver;
dom.window.ResizeObserver = TestResizeObserver as unknown as typeof ResizeObserver;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

Object.defineProperty(dom.window.HTMLElement.prototype, "getBoundingClientRect", {
  configurable: true,
  value(this: HTMLElement) {
    if (this.classList.contains("slashmenu")) return rect(360);
    if (this.classList.contains("slashmenu__row")) {
      return rect(this.firstElementChild?.classList.contains("slashmenu__group") ? 26 : 34);
    }
    return rect(0);
  },
});
Object.defineProperty(dom.window.HTMLElement.prototype, "offsetWidth", {
  configurable: true,
  get() {
    return 600;
  },
});
Object.defineProperty(dom.window.HTMLElement.prototype, "offsetHeight", {
  configurable: true,
  get(this: HTMLElement) {
    if (this.classList.contains("slashmenu")) return 360;
    if (this.classList.contains("slashmenu__row")) {
      return this.firstElementChild?.classList.contains("slashmenu__group") ? 26 : 34;
    }
    return 0;
  },
});

const initialItems: CommandInfo[] = [
  { name: "documentation", description: "Documentation", kind: "skill" },
  { name: "docx", description: "Word documents", kind: "skill" },
  { name: "lark-doc", description: "Lark documents", kind: "skill" },
  { name: "grill-with-docs", description: "Review with docs", kind: "skill" },
  { name: "tencent-docs", description: "Tencent documents", kind: "skill" },
  { name: "docs", description: "Search bundled documentation", kind: "builtin", group: "integrations" },
];

const filteredItems = [initialItems[0]!, initialItems[4]!, initialItems[5]!];

function Menu({ items }: { items: CommandInfo[] }) {
  return (
    <LocaleProvider>
      <SlashMenu items={items} activeIndex={-1} onPick={() => {}} onHover={() => {}} />
    </LocaleProvider>
  );
}

function rowPositions(): Array<{ label: string; position: number; transform: string }> {
  return Array.from(document.querySelectorAll<HTMLElement>(".slashmenu__row")).map((row) => {
    const match = row.style.transform.match(/translate3d\(0(?:px)?, ([\d.-]+)px, 0(?:px)?\)/);
    const name = row.querySelector(".slashmenu__name")?.textContent;
    return {
      label: name ?? "group",
      position: match ? Number(match[1]) : Number.NaN,
      transform: row.style.transform,
    };
  });
}

console.log("\nvirtual menu item identity");

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);

await act(async () => {
  root.render(<Menu items={initialItems} />);
  await new Promise((resolve) => setTimeout(resolve, 0));
});
const initialDocsRow = Array.from(document.querySelectorAll<HTMLElement>(".slashmenu__row"))
  .find((row) => row.querySelector(".slashmenu__name")?.textContent === "/docs");
await act(async () => {
  root.render(<Menu items={filteredItems} />);
  await new Promise((resolve) => setTimeout(resolve, 0));
});

const positions = rowPositions();
ok(
  JSON.stringify(positions) === JSON.stringify([
    { label: "group", position: 0, transform: "translate3d(0, 0px, 0)" },
    { label: "/documentation", position: 26, transform: "translate3d(0, 26px, 0)" },
    { label: "/tencent-docs", position: 60, transform: "translate3d(0, 60px, 0)" },
    { label: "group", position: 94, transform: "translate3d(0, 94px, 0)" },
    { label: "/docs", position: 120, transform: "translate3d(0, 120px, 0)" },
  ]),
  `filtered rows keep contiguous semantic positions: got ${JSON.stringify(positions)}`,
);
const filteredDocsRow = Array.from(document.querySelectorAll<HTMLElement>(".slashmenu__row"))
  .find((row) => row.querySelector(".slashmenu__name")?.textContent === "/docs");
ok(initialDocsRow === filteredDocsRow, "/docs keeps the same semantic DOM row while its filtered index changes");

await act(async () => root.unmount());
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
