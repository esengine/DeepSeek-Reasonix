// Run: tsx src/__tests__/tool-card-rename.test.tsx
//
// 验证 ToolCard 在 fileDiff.kind="rename" 时渲染 "src → dst" 路径变更卡片，
// 而非 unified-diff 或空白。rename 的 diff 为空，传统渲染会折叠为 none，
// 用户看不到路径变更——rename 分支让 move_file 的重命名结果可视化。

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { ToolCard } from "../components/ToolCard";
import { LocaleProvider } from "../lib/i18n";
import type { Item } from "../lib/useController";

type ToolItem = Extract<Item, { kind: "tool" }>;

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

function flushTimers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

Object.defineProperty(navigator, "language", { value: "en-US", configurable: true });

function installDom() {
  const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  dom.window.matchMedia = () => ({
    matches: true,
    media: "(prefers-reduced-motion: reduce)",
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  });
  return dom;
}

console.log("\ntool card rename rendering");

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const item: ToolItem = {
    kind: "tool",
    id: "move-rename-1",
    name: "move_file",
    args: JSON.stringify({ source_path: "/a.txt", destination_path: "/b.txt" }),
    readOnly: false,
    status: "done",
    durationMs: 10,
    fileDiff: { diff: "", added: 0, removed: 0, kind: "rename", srcPath: "/a.txt", dstPath: "/b.txt" },
  } as ToolItem;

  await act(async () => {
    root.render(
      React.createElement(LocaleProvider, null,
        React.createElement(ToolCard, { item }),
      ),
    );
    await flushTimers();
  });

  // rename 卡片应有 src 和 dst 路径
  const renameEl = document.querySelector(".tool__rename");
  ok(renameEl, "rename card renders .tool__rename element");
  ok(
    document.body.textContent?.includes("/a.txt"),
    "rename card shows source path",
  );
  ok(
    document.body.textContent?.includes("/b.txt"),
    "rename card shows destination path",
  );
  // 不应渲染 unified-diff（DiffView）
  ok(!document.querySelector(".diff-view"), "rename card does not render unified diff");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
