// Run: tsx src/__tests__/approval-modal-rename.test.tsx
//
// 验证 ApprovalModal 在 approval 携带 kind="rename" 时渲染 "src → dst"
// 路径变更预览卡片。用户审批 move_file 时需要看到路径变更才能决策。

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { ApprovalModal } from "../components/ApprovalModal";
import { LocaleProvider } from "../lib/i18n";
import type { WireApproval } from "../lib/types";

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

console.log("\napproval modal rename rendering");

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const approval: WireApproval = {
    id: "ap1",
    tool: "move_file",
    subject: "a.txt -> b.txt",
    diff: "",
    added: 0,
    removed: 0,
    kind: "rename",
    srcPath: "/workspace/a.txt",
    dstPath: "/workspace/b.txt",
  } as WireApproval;

  await act(async () => {
    root.render(
      React.createElement(LocaleProvider, null,
        React.createElement(ApprovalModal, {
          approval,
          onAnswer: () => undefined,
          onStop: () => undefined,
        }),
      ),
    );
    await flushTimers();
  });

  // rename 卡片应有 src 和 dst 路径
  const renameEl = document.querySelector(".tool__rename");
  ok(renameEl, "approval modal renders .tool__rename element");
  ok(
    document.body.textContent?.includes("/workspace/a.txt"),
    "approval modal shows source path",
  );
  ok(
    document.body.textContent?.includes("/workspace/b.txt"),
    "approval modal shows destination path",
  );

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
