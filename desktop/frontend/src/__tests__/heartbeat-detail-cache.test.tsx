// Run: node --import ./scripts/css-stub-register.mjs --import tsx src/__tests__/heartbeat-detail-cache.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { HeartbeatView } from "../custom/features/heartbeat/HeartbeatPanel";
import type { HeartbeatTask } from "../custom/features/heartbeat/heartbeat.types";
import { LocaleProvider } from "../lib/i18n";

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

function flush(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function button(label: string): HTMLButtonElement | undefined {
  return Array.from(document.querySelectorAll<HTMLButtonElement>("button")).find((item) => item.textContent?.trim() === label);
}

class NoopResizeObserver {
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
// 模拟真实 webview：localStorage 可用（HeartbeatPanel 内部读写详情缓存）
globalThis.localStorage = dom.window.localStorage;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.HTMLButtonElement = dom.window.HTMLButtonElement;
globalThis.HTMLInputElement = dom.window.HTMLInputElement;
globalThis.HTMLTextAreaElement = dom.window.HTMLTextAreaElement;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.ResizeObserver = NoopResizeObserver as unknown as typeof ResizeObserver;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

let backendTasks: HeartbeatTask[] = [
  {
    id: "task-1",
    title: "Cached detail task",
    prompt: "prompt",
    interval: "30m",
    enabled: true,
    createdAt: 1,
  },
  {
    id: "task-2",
    title: "Another task",
    prompt: "prompt",
    interval: "1h",
    enabled: false,
    createdAt: 2,
  },
];
// 可控延迟的 ReloadConfig：模拟初始加载慢（Codex P1 竞态）
let reloadGate: Promise<void> | null = null;
let releaseReload: (() => void) | null = null;
function gateReload() {
  reloadGate = new Promise((resolve) => { releaseReload = resolve; });
}
function releaseReloadGate() {
  releaseReload?.();
  reloadGate = null;
}
Object.assign(window, {
  go: {
    main: {
      App: {
        async HeartbeatReloadConfig() {
          if (reloadGate) await reloadGate;
          return { revision: 1, etag: "test", tasks: backendTasks };
        },
        async HeartbeatSaveConfig(update: { tasks?: HeartbeatTask[] }) {
          backendTasks = update.tasks ?? [];
          return { revision: 2, etag: "saved", tasks: backendTasks };
        },
        async HeartbeatTriggerNow() {},
        async HeartbeatGenerateID() { return "draft-1"; },
        async ListWorkspaces() { return []; },
      },
    },
  },
});

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);

function renderView() {
  root.render(
    <LocaleProvider>
      <HeartbeatView />
    </LocaleProvider>,
  );
}

// 模拟 App 条件渲染：mainView 从 automation 切走时 HeartbeatView 卸载（root 保留），
// 切回时重新挂载。用 root.render(null) 卸载组件树而不是销毁 root。
function unmountView() {
  root.render(null);
}

console.log("\nheartbeat detail cache");

// 1. 打开任务详情 → 写入缓存
await act(async () => {
  renderView();
  await flush();
  await flush();
});
await act(async () => {
  document.querySelector<HTMLDivElement>(".worktree-node--task")?.click();
  await flush();
  await flush();
});
ok(document.querySelector<HTMLInputElement>('[aria-label="Title"]')?.value === "Cached detail task", "clicking a task opens its detail editor");
ok(localStorage.getItem("reasonix-heartbeat-detail")?.includes("task-1") === true, "opening a detail writes the open-state cache");

// 2. 卸载后重新挂载（模拟切换对话再回到自动化面板）→ 详情自动恢复
await act(async () => { unmountView(); await flush(); });
await act(async () => {
  renderView();
  await flush();
  await flush();
});
ok(document.querySelector<HTMLInputElement>('[aria-label="Title"]')?.value === "Cached detail task", "detail reopens from cache after remount without a click");
ok(document.querySelector(".heartbeat-split__right") != null, "detail panel is visible after remount");

// 3. 关闭详情 → 清除缓存 → 再挂载不恢复
await act(async () => {
  document.querySelector<HTMLButtonElement>(".heartbeat-editor__close")?.click();
  await flush();
});
ok(localStorage.getItem("reasonix-heartbeat-detail") == null, "closing the detail clears the cache");
await act(async () => { unmountView(); await flush(); });
await act(async () => {
  renderView();
  await flush();
  await flush();
});
ok(document.querySelector(".heartbeat-split__right") == null, "no detail restore when the cache was cleared");

// 4. 缓存指向已删除任务 → 不恢复
await act(async () => {
  localStorage.setItem("reasonix-heartbeat-detail", JSON.stringify({ open: true, taskId: "ghost-task" }));
  unmountView();
  await flush();
});
await act(async () => {
  renderView();
  await flush();
  await flush();
});
ok(document.querySelector(".heartbeat-split__right") == null, "stale cache for a deleted task does not restore a detail");
ok(document.body.textContent?.includes("Select a task to view details") !== true, "stale cache leaves the panel in the list-only state");

// 5. Codex P1：加载慢时用户先打开草稿，恢复回调不得覆盖草稿
localStorage.setItem("reasonix-heartbeat-detail", JSON.stringify({ open: true, taskId: "task-1" }));
gateReload();
await act(async () => {
  unmountView();
  renderView();
  await flush();
});
await act(async () => {
  // 加载未完成时点开建议草稿（工具条/建议区在 loading 期间仍可交互）
  document.querySelector<HTMLButtonElement>(".heartbeat-suggestion")?.click();
  await flush();
});
await act(async () => {
  releaseReloadGate();
  await flush();
  await flush();
});
ok(document.querySelector<HTMLInputElement>('[aria-label="Title"]')?.value === "Daily review", "a draft opened during slow load is not overwritten by cache restoration");
ok(localStorage.getItem("reasonix-heartbeat-detail")?.includes("task-1") === true, "blocked restore leaves the existing cache untouched");

// 6. Codex P2：新建任务保存后缓存指向新任务，切走再回来恢复它
await act(async () => {
  unmountView();
  renderView();
  await flush();
  await flush();
});
await act(async () => {
  document.querySelector<HTMLDivElement>(".worktree-node--task")?.click();
  await flush();
  await flush();
});
ok(localStorage.getItem("reasonix-heartbeat-detail")?.includes("task-1") === true, "opening a task again re-caches it");
// 建议区草稿预填 title+prompt，无需模拟受控输入即可保存
await act(async () => {
  document.querySelector<HTMLButtonElement>(".heartbeat-suggestion")?.click();
  await flush();
  await flush();
});
ok(document.querySelector<HTMLInputElement>('[aria-label="Title"]')?.value === "Daily review", "suggestion draft opens with a prefilled title");
await act(async () => {
  button("Save")?.click();
  await flush();
  await flush();
});
ok(localStorage.getItem("reasonix-heartbeat-detail")?.includes("draft-1") === true, "saving a new draft updates the cache to the new task id");
await act(async () => { unmountView(); await flush(); });
await act(async () => {
  renderView();
  await flush();
  await flush();
});
ok(document.querySelector<HTMLInputElement>('[aria-label="Title"]')?.value === "Daily review", "remount restores the newly saved task instead of the previously opened one");

// 7. Codex P2：搜索过滤隐式关闭编辑器时清除缓存
// jsdom 下 dispatchEvent 不驱动 React 19 受控组件的 onChange，直接调用
// React 绑定的 onChange prop（测试环境锁定 React 19，内部属性稳定）。
function typeInto(el: Element | null, value: string) {
  if (!el) return;
  const key = Object.keys(el).find((k) => k.startsWith("__reactProps"));
  const props = (el as unknown as Record<string, { onChange?: (e: { target: { value: string } }) => void }>)[key ?? ""];
  props?.onChange?.({ target: { value } });
}
await act(async () => {
  unmountView();
  renderView();
  await flush();
  await flush();
});
await act(async () => {
  document.querySelector<HTMLDivElement>(".worktree-node--task")?.click();
  await flush();
  await flush();
});
ok(localStorage.getItem("reasonix-heartbeat-detail")?.includes("task-1") === true, "task detail is cached again for the filter scenario");
await act(async () => {
  typeInto(document.querySelector<HTMLInputElement>(".heartbeat-list-search__input"), "zzz-no-match");
  await flush();
  await flush();
});
ok(document.querySelector('[aria-label="Title"]') == null, "filtering out the edited task closes its editor");
ok(localStorage.getItem("reasonix-heartbeat-detail") == null, "implicit editor close clears the cache");
await act(async () => { unmountView(); await flush(); });
await act(async () => {
  renderView();
  await flush();
  await flush();
});
ok(document.querySelector(".heartbeat-split__right") == null, "no restore after the filtered-out task was deselected");

await act(async () => root.unmount());
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
