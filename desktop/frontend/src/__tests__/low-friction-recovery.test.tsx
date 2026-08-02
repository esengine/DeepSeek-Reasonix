// Run: tsx src/__tests__/low-friction-recovery.test.tsx

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { ConfigRepairBanner } from "../components/ConfigRepairBanner";
import { SessionIssueCard } from "../components/SessionIssueCard";
import { TokenModeJobBlocker } from "../components/TokenModeJobBlocker";
import { LocaleProvider, t } from "../lib/i18n";
import { stopBackgroundJobsAndSwitch, visibleBackgroundJobCount } from "../lib/tokenModeRecovery";
import type { SessionRuntimeIssue } from "../lib/types";

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

function eq(actual: unknown, expected: unknown, label: string) {
  ok(actual === expected, actual === expected ? label : `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function installDom() {
  const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', { pretendToBeVisual: true, url: "http://localhost/" });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  globalThis.Node = dom.window.Node;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.HTMLButtonElement = dom.window.HTMLButtonElement;
  globalThis.Event = dom.window.Event;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: () => ({ matches: true, addEventListener() {}, removeEventListener() {} }),
  });
  return dom;
}

async function paint(node: React.ReactNode): Promise<Root> {
  const root = createRoot(document.getElementById("root")!);
  await act(async () => {
    root.render(<LocaleProvider>{node}</LocaleProvider>);
    await Promise.resolve();
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  return root;
}

console.log("\nlow-friction recovery surfaces");

{
  const jobs = [{ id: "job-a", kind: "bash", label: "A", status: "running", startedAt: 1 }];
  eq(visibleBackgroundJobCount(0, jobs), 1, "visible jobs keep recovery available while the runtime count is stale");
  eq(visibleBackgroundJobCount(2, jobs), 2, "runtime count covers jobs not yet present in the list");
}

{
  const calls: string[] = [];
  let listed = 0;
  const remaining = await stopBackgroundJobsAndSwitch(
    async () => {
      calls.push("list");
      listed += 1;
      return listed === 1
        ? [{ id: "job-a", kind: "bash", label: "A", status: "running", startedAt: 1 }, { id: "job-b", kind: "bash", label: "B", status: "running", startedAt: 2 }]
        : [];
    },
    async (jobID) => { calls.push(`cancel:${jobID}`); },
    async () => { calls.push("switch"); },
  );
  eq(remaining, 0, "work-mode recovery confirms all jobs stopped");
  eq(calls.join(","), "list,cancel:job-a,cancel:job-b,list,switch", "work-mode recovery stops, verifies, then switches in order");
}

{
  let switched = false;
  let listed = 0;
  const remaining = await stopBackgroundJobsAndSwitch(
    async () => {
      listed += 1;
      return [{ id: listed === 1 ? "job-a" : "job-new", kind: "bash", label: "A", status: "running", startedAt: listed }];
    },
    async () => {},
    async () => { switched = true; },
  );
  eq(remaining, 1, "a newly arrived job keeps recovery blocked");
  eq(switched, false, "work mode never changes while a job remains");
}

{
  const dom = installDom();
  let confirm = 0;
  let cancel = 0;
  const root = await paint(<TokenModeJobBlocker t={t} count={2} busy={false} onConfirm={() => { confirm += 1; }} onCancel={() => { cancel += 1; }} />);
  eq(document.querySelectorAll(".composer-recovery .btn").length, 2, "work-mode blocker exposes one recovery action and cancel");
  eq(document.querySelector(".composer-recovery .btn--primary")?.textContent?.trim(), "Stop and switch", "work-mode blocker recommends the combined action");
  await act(async () => { document.querySelector<HTMLButtonElement>(".composer-recovery .btn--primary")?.click(); });
  eq(confirm, 1, "combined action is routed directly");
  eq(cancel, 0, "combined action does not trigger cancel");
  await act(async () => root.unmount());
  dom.window.close();
}

{
  const dom = installDom();
  const actions: string[] = [];
  const issue = { issueId: "external-1", ownerKind: "external_process", actions: ["retry", "read_only", "copy"], message: "lease diagnostics" } as SessionRuntimeIssue;
  const root = await paint(<SessionIssueCard issue={issue} tabID="tab-1" t={t} api={{ ResolveSessionRuntimeIssue: async (_tab, _issue, action) => { actions.push(action); } }} />);
  eq(document.querySelectorAll(".session-issue-card > .btn--primary").length, 1, "external ownership shows exactly one primary action");
  eq(document.querySelector(".session-issue-card > .btn--primary")?.textContent?.trim(), "Open copy", "external ownership recommends the non-disruptive copy");
  eq(document.querySelector(".banner__more")?.textContent?.includes("lease diagnostics"), true, "technical details remain in the collapsed disclosure instead of primary copy");
  eq(document.querySelector<HTMLDetailsElement>(".banner__more")?.open, false, "secondary actions are collapsed initially");
  await act(async () => root.unmount());
  dom.window.close();
}

{
  const dom = installDom();
  const actions: string[] = [];
  const issue = { issueId: "stale-1", ownerKind: "stale_reclaimed", actions: ["retry"], message: "stale" } as SessionRuntimeIssue;
  const root = await paint(<SessionIssueCard issue={issue} tabID="tab-1" t={t} api={{ ResolveSessionRuntimeIssue: async (_tab, _issue, action) => { actions.push(action); } }} />);
  eq(actions.join(","), "retry", "stale ownership is reclaimed without asking the user");
  eq(document.querySelector(".session-issue-card"), null, "automatic recovery removes the resolved card");
  await act(async () => root.unmount());
  dom.window.close();
}

{
  const dom = installDom();
  let restores = 0;
  const root = await paint(<ConfigRepairBanner
    t={t}
    api={{
      ConfigRepairStatus: async () => ({
        outcome: "config_damaged",
        scope: "global",
        path: "",
        detail: "parse failed",
        repairedAt: "",
        canOpenFile: true,
        undoable: false,
      }),
      RestoreGlobalConfigSnapshot: async () => { restores += 1; return true; },
      OpenConfigFile: async () => {},
      UndoConfigRepair: async () => {},
    }}
  />);
  eq(document.querySelectorAll(".config-repair-banner > .btn--primary").length, 1, "damaged settings show one primary recovery action");
  eq(document.querySelector(".config-repair-banner > .btn--primary")?.textContent?.trim(), "Restore last working settings", "config recovery names the user outcome instead of the mechanism");
  eq(document.querySelector<HTMLDetailsElement>(".config-repair-banner .banner__more")?.open, false, "manual file editing stays collapsed initially");
  await act(async () => {
    document.querySelector<HTMLButtonElement>(".config-repair-banner > .btn--primary")?.click();
    await Promise.resolve();
  });
  eq(restores, 1, "primary config action restores the snapshot");
  eq(document.querySelector(".config-repair-banner"), null, "successful restore clears the blocker in place");
  await act(async () => root.unmount());
  dom.window.close();
}

{
  const here = dirname(fileURLToPath(import.meta.url));
  const source = readFileSync(resolve(here, "../components/SettingsPanel.tsx"), "utf8");
  ok(source.includes('<SettingsField label={t("settings.closeBehavior")}>'), "General settings keeps the user-owned close behavior choice visible");
  ok(source.includes('(["smart", "background", "quit"] as const)'), "close behavior exposes all three compatible choices");
  ok(source.includes("app.SetCloseBehavior(mode)"), "close behavior choices persist through the existing settings API");
  ok(source.includes("closeBehavior: normalizeCloseBehavior(view.closeBehavior)"), "existing close-behavior config remains readable for upgrades");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
