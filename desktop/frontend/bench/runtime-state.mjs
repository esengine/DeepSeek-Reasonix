#!/usr/bin/env node
// Real Chromium UI with controlled runtime frames; backend ownership is covered
// separately by the controller/Serve/remote HTTP regression tests.
import path from "node:path";
import fs from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
process.env.PLAYWRIGHT_BROWSERS_PATH = path.join(root, ".pw-browsers");
const { chromium } = await import("playwright");
const server = await createServer({ root, logLevel: "error", server: { host: "127.0.0.1", port: 4668, strictPort: true } });
await server.listen();
const browser = await chromium.launch({ headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const errors = [];
page.on("pageerror", error => errors.push(error.message));
const check = (yes, message) => { if (!yes) throw new Error(message); console.log("PASS " + message); };
try {
  await page.goto("http://127.0.0.1:4668/?mock=bench&bench=1");
  const input = page.locator("textarea.composer__input:not([aria-hidden=true])");
  await input.waitFor();
  await page.locator('.project-tree__topic-main:has-text("bench:small-6t")').click();
  await page.waitForFunction(() => document.querySelector(".transcript")?.textContent?.includes("ASYNC LAYOUT EXPANSION COMPLETE"));
  await page.evaluate(async () => {
    const { app, onRemoteTabOpened, onRemoteTabUpdated } = await import("/src/lib/bridge.ts");
    const { runtimeStateStore } = await import("/src/lib/runtimeStateStore.ts");
    const { acceptRuntimeState } = await import("/src/lib/runtimeStateReducer.ts");
    const { installDesktopHostStub } = await import("/src/__tests__/desktopHostStub.ts");
    const { DESKTOP_COMMANDS } = await import("/src/generated/desktopContract.generated.ts");
    const fallback = Object.fromEntries(DESKTOP_COMMANDS.map(key => [key, app[key]]));
    const tabs = await app.ListTabs();
    const tree = { topics: [] };
    const selected = tabs.find(tab => tab.sessionPath?.includes("small")) ?? tabs[0];
    window.__runtimeFixture = { tab: selected, revision: 0, calls: [], queries: [], fail: true, accept: (...args) => acceptRuntimeState(runtimeStateStore, ...args), topics: tree.topics };
    onRemoteTabOpened(tab => { window.__runtimeFixture.tab = tab; });
    onRemoteTabUpdated(tab => { window.__runtimeFixture.tab = tab; });
    const host = installDesktopHostStub(new Proxy(fallback, { get(_target, key) {
      if (key === "CaptureInboxTarget") return async (tabId, sessionPath) => {
        if (!sessionPath || sessionPath !== window.__runtimeFixture.tab.sessionPath) throw new Error("Composer did not bind its selected session path: " + JSON.stringify({ tabId, sessionPath, expected: window.__runtimeFixture.tab.sessionPath }));
        const remote = window.__runtimeFixture.tab.remote;
        return { tabId, sessionPath, generation: 1, selection: 0, remote: Boolean(remote), hostId: remote?.hostId, workspace: remote?.workspace };
      };
      if (key === "LookupInboxFollowupForTarget") return async (...args) => {
        window.__runtimeFixture.queries.push(args);
        if (window.__runtimeFixture.fail) throw new Error("receipt unavailable");
        return { itemId: "runtime-queued", disposition: "idempotent_hit", position: 0, paused: false };
      };
      if (key === "EnqueueInboxFollowupForTarget") return async (...args) => {
        window.__runtimeFixture.calls.push(args);
        if (window.__runtimeFixture.fail) throw new Error("fixture enqueue unavailable");
        return { itemId: "runtime-queued", disposition: "queued", position: 1, paused: false };
      };
      const value = fallback[key];
      if (key === "OpenRemoteProjectTab") return async (...args) => { const tab = await value(...args); window.__runtimeFixture.tab = tab; return tab; };
      return value;
    } }));
    window.__runtimeFixture.emit = (tabId, channel, payload) => host.emit(`remote-tab:${tabId}:${channel}`, payload);
  });
  const publish = async (phase, extra = {}, remote = false) => page.evaluate(({ phase, extra, remote }) => {
    const f = window.__runtimeFixture;
    const tab = f.tab;
    const state = { schemaVersion: 1, runtimeEpoch: "fixture-controller", revision: ++f.revision, phase,
      running: phase === "executing" || phase === "finishing", turnId: "fixture-turn", turnStatus: phase === "executing" ? "in_progress" : "completed",
      turnEventSeq: 1, pendingPrompt: false, cancelRequested: false, cancellable: phase === "executing", backgroundJobs: 0, activity: phase === "executing" ? "thinking" : "", ...extra };
    return f.accept({ epoch: "fixture-app", revision: f.revision, topics: f.topics, sessions: [{
      tabId: tab.id, scope: tab.scope ?? "project", workspaceRoot: tab.workspaceRoot, topicId: tab.topicId ?? "",
      sessionPath: tab.sessionPath ?? "", sessionGeneration: 1, open: true, remote,
      hostId: tab.remote?.hostId, freshness: extra.freshness ?? "synced", state,
    }] }, true);
  }, { phase, extra, remote });
  await publish("finishing");
  await page.locator(".composer-run-strip").filter({ hasText: /Finishing|正在收尾/ }).waitFor();
  check(await page.locator(".composer__btn--stop").count() === 0, "finishing hides Stop");
  check(await page.locator(".composer-card--running,.composer-run-strip__dot").count() === 0, "finishing has no animated run marker");
  await input.fill("durable next turn");
  await input.press("Enter");
  await page.waitForFunction(() => window.__runtimeFixture.calls.length === 1);
  check(await input.inputValue() === "durable next turn", "failed enqueue preserves draft");
  await fs.mkdir("/tmp/reasonix-runtime-evidence", { recursive: true });
  await page.screenshot({ path: "/tmp/reasonix-runtime-evidence/pending-followup.png" });
  await publish("idle");
  check(await page.locator(".composer__btn--send").getAttribute("aria-label") === "Check send result", "phase transition keeps receipt confirmation action");
  await page.evaluate(() => { window.__runtimeFixture.fail = false; });
  await input.press("Enter");
  await page.waitForFunction(() => document.querySelector("textarea.composer__input:not([aria-hidden=true])")?.value === "");
  const calls = await page.evaluate(() => window.__runtimeFixture.calls);
  const queries = await page.evaluate(() => window.__runtimeFixture.queries);
  check(calls.length === 1 && queries.length === 1 && calls[0].at(-1) === queries[0].at(-1), "retry only queries the original durable idempotency key");
  await publish("idle", { backgroundJobs: 2 });
  await page.locator(".composer-run-strip").filter({ hasText: /2/ }).waitFor();
  check(await page.locator(".project-tree__folder-active-indicator:not(.project-tree__folder-active-indicator--static)").count() > 0, "background jobs keep project activity visible");
  await publish("idle");
  await page.locator(".composer-run-strip").waitFor({ state: "hidden" });
  check(await page.locator(".project-tree__folder-active-indicator").count() === 0, "last job completion clears project activity");
  await page.locator('.project-tree__folder-main:has(svg.lucide-cloud)').click();
  await page.locator('.project-tree__topic-main:has-text("Remote demo session")').click();
  await page.locator(".remote-surface--ready").waitFor();
  await publish("finishing", {}, true);
  await page.locator(".composer-run-strip").filter({ hasText: /Finishing|正在收尾/ }).waitFor();
  await input.fill("remote durable next turn");
  await input.press("Enter");
  await page.waitForFunction(() => window.__runtimeFixture.calls.length === 2);
  check(await input.inputValue() === "", "remote finishing queues the next input and clears it after receipt");
  await publish("executing", { freshness: "unknown" }, true);
  await page.locator(".composer-run-strip").filter({ hasText: /sync|同步/i }).waitFor();
  check(await input.isDisabled(), "remote disconnect blocks send while preserving unknown state");
  check(await page.locator(".composer__btn--stop").count() === 0, "unknown remote state hides Stop");
  await fs.mkdir("/tmp/reasonix-runtime-evidence", { recursive: true });
  await page.screenshot({ path: "/tmp/reasonix-runtime-evidence/remote-unknown.png" });
  await publish("executing", {}, true);
  await page.locator(".composer__btn--stop").waitFor();
  check(!(await input.isDisabled()), "remote reconnect restores authoritative execution controls");
  await page.evaluate(async () => {
    const __emitMockRemoteTab = window.__runtimeFixture.emit;
    const tabId = window.__runtimeFixture.tab.id;
    __emitMockRemoteTab(tabId, "event", { kind: "turn_started", turnId: "fixture-turn" });
    __emitMockRemoteTab(tabId, "event", { kind: "text", text: "runtime missing completion fixture" });
  });
  await page.locator(".remote-surface").getByText("runtime missing completion fixture", { exact: true }).waitFor();
  await publish("idle", {}, true);
  await page.locator(".composer-run-strip").waitFor({ state: "hidden" });
  check(await page.locator(".composer__btn--stop").count() === 0, "remote completion removes the run control");
  await page.waitForFunction(() => !document.querySelector('.remote-surface [data-transcript-block-phase="active"]'));
  check(await page.locator(".remote-surface").getByText("runtime missing completion fixture", { exact: true }).count() === 0,
    "trusted idle without turn_done settles the real transcript and reconciles durable history");
  await page.locator('.project-tree__topic-main:has-text("bench:geometry")').click();
  await page.waitForFunction(() => document.querySelector(".transcript")?.textContent?.includes("Geometry contract fixture complete."));
  check(await page.locator(".remote-surface").count() === 0, "local switch retains ownership after remote runtime frames");
  check(errors.length === 0, "runtime scenarios produce no browser errors: " + errors.join("; "));
} catch (error) {
  console.error("Runtime fixture toasts:", await page.locator(".toast__text").allTextContents());
  throw error;
} finally { await browser.close(); await server.close(); }
