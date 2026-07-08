// Run: tsx src/__tests__/bridge-breadcrumbs.test.ts

import { snapshotBreadcrumbs } from "../lib/breadcrumbs";
import { app } from "../lib/bridge";

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

function ensureWindow() {
  if (typeof window === "undefined") {
    (globalThis as Record<string, unknown>).window = {} as Window & typeof globalThis;
  }
}

function newBreadcrumbMessages(since: number, cat: string): string[] {
  return snapshotBreadcrumbs()
    .slice(since)
    .filter((crumb) => crumb.cat === cat)
    .map((crumb) => crumb.msg);
}

console.log("\nbridge breadcrumbs");

const previousPerformance = (globalThis as { performance?: Performance }).performance;
let now = 0;
(globalThis as Record<string, unknown>).performance = { now: () => now };

ensureWindow();
const previousGo = window.go;
window.go = {
  main: {
    App: {
      async IsMainWindowMaximised() {
        now = 42;
        return true;
      },
      async CheckUpdate() {
        now = 125;
        throw new Error("offline");
      },
    } as never,
  },
};

const successStart = snapshotBreadcrumbs().length;
now = 10;
await app.IsMainWindowMaximised();
const successBridge = newBreadcrumbMessages(successStart, "bridge");
ok(
  successBridge.includes("window IsMainWindowMaximised") &&
    successBridge.includes("window IsMainWindowMaximised done ms=32"),
  "records bridge start and completion timing breadcrumbs",
);

const errorStart = snapshotBreadcrumbs().length;
now = 100;
try {
  await app.CheckUpdate();
} catch {
  // Expected: this branch verifies the breadcrumb emitted by the proxy rejection path.
}
const errorBridge = newBreadcrumbMessages(errorStart, "bridge");
const bridgeErrors = newBreadcrumbMessages(errorStart, "bridge.error");
ok(errorBridge.includes("update CheckUpdate"), "records bridge start breadcrumb before failed calls");
ok(bridgeErrors.includes("CheckUpdate ms=25"), "records bridge failure timing breadcrumbs");

window.go = previousGo;
if (previousPerformance) {
  (globalThis as Record<string, unknown>).performance = previousPerformance;
} else {
  delete (globalThis as { performance?: Performance }).performance;
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
