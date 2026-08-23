// Run: node --import ./scripts/css-stub-register.mjs --import tsx src/__tests__/heartbeat-filter-cache.test.tsx

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
// 模拟真实 webview：localStorage 可用（HeartbeatPanel 内部读写过滤/详情缓存）
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
    title: "Daily review",
    prompt: "prompt",
    interval: "30m",
    enabled: true,
    createdAt: 1,
  },
  {
    id: "task-2",
    title: "Weekly report",
    prompt: "prompt",
    interval: "1h",
    enabled: false,
    createdAt: 2,
  },
];
Object.assign(window, {
  go: {
    main: {
      App: {
        async HeartbeatReloadConfig() {
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

// jsdom 下 dispatchEvent 不驱动 React 19 受控组件的 onChange，直接调用
// React 绑定的 onChange prop（测试环境锁定 React 19，内部属性稳定）。
function typeInto(el: Element | null, value: string) {
  if (!el) return;
  const key = Object.keys(el).find((k) => k.startsWith("__reactProps"));
  const props = (el as unknown as Record<string, { onChange?: (e: { target: { value: string } }) => void }>)[key ?? ""];
  props?.onChange?.({ target: { value } });
}

console.log("\nheartbeat filter cache");

localStorage.removeItem("reasonix-heartbeat-filter");
localStorage.removeItem("reasonix-heartbeat-detail");

// 1. 输入搜索词 → 写入过滤缓存，列表被过滤
await act(async () => {
  renderView();
  await flush();
  await flush();
});
await act(async () => {
  typeInto(document.querySelector<HTMLInputElement>(".heartbeat-list-search__input"), "daily");
  await flush();
});
let filterCache = JSON.parse(localStorage.getItem("reasonix-heartbeat-filter") ?? "{}");
ok(filterCache.searchQuery === "daily", "typing a search query writes the filter cache");
ok(document.querySelectorAll(".worktree-node--task").length === 1, "search filters the list to the matching task");

// 2. 卸载后重新挂载（模拟切换对话再回到自动化面板）→ 搜索词恢复，列表仍被过滤
await act(async () => { unmountView(); await flush(); });
await act(async () => {
  renderView();
  await flush();
  await flush();
});
ok(document.querySelector<HTMLInputElement>(".heartbeat-list-search__input")?.value === "daily", "search query restores from cache after remount");
ok(document.body.textContent?.includes("Weekly report") === false, "remounted list is still filtered by the restored query");

// 3. 清空搜索 → 缓存更新为空 → 再挂载不恢复旧词
await act(async () => {
  typeInto(document.querySelector<HTMLInputElement>(".heartbeat-list-search__input"), "");
  await flush();
});
filterCache = JSON.parse(localStorage.getItem("reasonix-heartbeat-filter") ?? "{}");
ok(filterCache.searchQuery === "", "clearing the search writes the empty query to cache");
await act(async () => { unmountView(); await flush(); });
await act(async () => {
  renderView();
  await flush();
  await flush();
});
ok(document.querySelector<HTMLInputElement>(".heartbeat-list-search__input")?.value === "", "no stale search query after clearing");

// 4. 切换状态筛选（Paused）→ 写缓存 → 重挂载恢复
await act(async () => {
  button("Paused")?.click();
  await flush();
});
filterCache = JSON.parse(localStorage.getItem("reasonix-heartbeat-filter") ?? "{}");
ok(filterCache.statusFilter === "disabled", "switching the status filter writes the filter cache");
ok(document.querySelectorAll(".worktree-node--task").length === 1, "status filter hides enabled tasks");
await act(async () => { unmountView(); await flush(); });
await act(async () => {
  renderView();
  await flush();
  await flush();
});
ok(document.querySelector<HTMLButtonElement>('[role="tab"][aria-selected="true"]')?.textContent?.trim() === "Paused", "status filter restores from cache after remount");
ok(document.body.textContent?.includes("Daily review") === false, "remounted list respects the restored status filter");

// 5. scopeFilter 恢复：缓存含项目范围筛选时重挂载恢复（flat 视图下下拉按钮文本）
await act(async () => { unmountView(); await flush(); });
localStorage.setItem("reasonix-heartbeat-filter", JSON.stringify({ searchQuery: "", statusFilter: "all", scopeFilter: "global" }));
await act(async () => {
  renderView();
  await flush();
  await flush();
});
await act(async () => {
  // 默认 grouped 视图无范围下拉，切到 flat 视图后可见
  document.querySelector<HTMLButtonElement>(".heartbeat-toolbar__btn--icon")?.click();
  await flush();
});
ok(document.querySelector<HTMLButtonElement>(".heartbeat-toolbar__btn--select")?.textContent?.trim() === "Global", "scope filter restores from cache after remount");

// 6. 组合：恢复的搜索词过滤掉缓存详情任务 → 详情被关闭（而非展示被过滤的任务）
await act(async () => { unmountView(); await flush(); });
localStorage.setItem("reasonix-heartbeat-detail", JSON.stringify({ open: true, taskId: "task-1" }));
localStorage.setItem("reasonix-heartbeat-filter", JSON.stringify({ searchQuery: "zzz-no-match", statusFilter: "all", scopeFilter: "all" }));
await act(async () => {
  renderView();
  await flush();
  await flush();
});
ok(document.querySelector<HTMLInputElement>(".heartbeat-list-search__input")?.value === "zzz-no-match", "search query restores alongside the detail cache");
ok(document.querySelector('[aria-label="Title"]') == null, "editor is closed when the restored query filters out the cached task");
ok(localStorage.getItem("reasonix-heartbeat-detail") == null, "implicit editor close clears the detail cache");
ok(document.body.textContent?.includes("No matching tasks") === true, "restored query leaves the list in the no-match state");
await act(async () => { unmountView(); await flush(); });
await act(async () => {
  renderView();
  await flush();
  await flush();
});
ok(document.querySelector(".heartbeat-split__right") == null, "no detail restore after the filtered-out task was deselected");

await act(async () => root.unmount());
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
