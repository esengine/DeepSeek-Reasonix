// Run: tsx src/__tests__/workspace-panel-open-migration.test.ts
// zk-ge C14 兼容性：项目级 dock 开合 key 缺失时回退旧全局 key，升级不翻转用户选择

import { JSDOM } from "jsdom";

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

function installLocalStorage(seed: Record<string, string>) {
  const dom = new JSDOM("<!doctype html><html><body></body></html>", { url: "http://localhost/" });
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.localStorage = dom.window.localStorage;
  for (const [key, value] of Object.entries(seed)) {
    dom.window.localStorage.setItem(key, value);
  }
  return dom;
}

// 动态 import（依赖 window/localStorage 已就绪）
async function loadModule() {
  return await import("../store/layout");
}

console.log("\nworkspace dock-open legacy key migration (CLAIM.PERSIST.014)");

{
  // 场景 1：项目 key 缺失 + 旧全局 key = 关闭("0") → 首次打开项目应保持关闭
  installLocalStorage({ "reasonix.workspacePanel.open": "0" });
  const { loadWorkspacePanelOpen } = await loadModule();
  ok(loadWorkspacePanelOpen("project-a") === false, "project key missing + legacy global closed => stays closed");
}

{
  // 场景 2：项目 key 缺失 + 旧全局 key = 打开("1") → 首次打开项目应打开
  installLocalStorage({ "reasonix.workspacePanel.open": "1" });
  const { loadWorkspacePanelOpen } = await loadModule();
  ok(loadWorkspacePanelOpen("project-b") === true, "project key missing + legacy global open => stays open");
}

{
  // 场景 3：项目 key 存在（优先于旧全局 key）
  installLocalStorage({ "reasonix.workspacePanel.open": "0", "reasonix.workspacePanel.open.project-c": "1" });
  const { loadWorkspacePanelOpen } = await loadModule();
  ok(loadWorkspacePanelOpen("project-c") === true, "project key present wins over legacy global");
}

{
  // 场景 4：项目 key 和旧全局 key 都缺失 → 默认打开
  installLocalStorage({});
  const { loadWorkspacePanelOpen } = await loadModule();
  ok(loadWorkspacePanelOpen("project-d") === true, "neither key present => default open");
}

console.log(`\nworkspace dock-open migration: ${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
