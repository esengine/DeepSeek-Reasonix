// Run: tsx src/__tests__/statusbar-workspace.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { StatusBar } from "../components/StatusBar";
import { LocaleProvider } from "../lib/i18n";
import { DEFAULT_STATUS_BAR_ITEMS, normalizeStatusBarItems } from "../lib/statusBarItems";

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

function renderStatusBar(props: Partial<Parameters<typeof StatusBar>[0]> = {}): string {
  return renderToStaticMarkup(
    <LocaleProvider>
      <StatusBar
        context={{ used: 0, window: 0, sessionTokens: 0 }}
        running={false}
        {...props}
      />
    </LocaleProvider>,
  );
}

console.log("\nstatus bar workspace");


{
  const defaultItems = DEFAULT_STATUS_BAR_ITEMS as readonly string[];
  ok(defaultItems.includes("workspace"), "workspace is a default configurable status item");
  ok(defaultItems.includes("git_branch"), "git branch is a default configurable status item");
  ok(
    normalizeStatusBarItems(["git_branch", "workspace", "cache"]).join(",") === "git_branch,workspace,cache",
    "workspace items preserve configured order",
  );
}

{
  const remoteHosts = [
    { id: "demo", label: "demo", host: "192.0.2.10", port: 22, user: "dev", identityFile: "", proxyJump: "", defaultWorkspace: "~/app", serveInstall: "auto", useSSHConfig: false },
  ];
  const stopped = renderStatusBar({ workspacePath: "/workspace/repo", workspaceName: "repo", remoteHosts });
  ok(stopped.includes("SSH · Disconnected"), "disconnected SSH entry keeps its full accessible status");
  ok(stopped.includes('statusbar__remote--idle'), "disconnected SSH entry uses the compact idle treatment");
  ok(stopped.includes('<span class="statusbar__remote-label">SSH</span>'), "disconnected SSH entry renders only the compact SSH label");
  ok(stopped.indexOf("SSH · Disconnected") < stopped.indexOf("workspace/repo"), "window-level SSH entry leads the status bar");

  const connected = renderStatusBar({
    workspacePath: "/workspace/repo",
    workspaceName: "repo",
    remoteHosts,
    remoteStatuses: { demo: { hostId: "demo", state: "connected" } },
  });
  ok(connected.includes("demo · Connected"), "SSH entry includes host and connected state text");
  ok(connected.includes('statusbar__remote-state-dot'), "connected SSH entry renders a state dot");
  ok(connected.includes('<span class="statusbar__remote-label">demo</span>'), "connected SSH entry renders the host without redundant state text");

  const failed = renderStatusBar({
    workspacePath: "/workspace/repo",
    remoteHosts,
    remoteStatuses: { demo: { hostId: "demo", state: "stopped", error: "handshake failed" } },
  });
  ok(failed.includes("demo · Connection failed"), "SSH entry keeps a recoverable failure summary visible");
  ok(failed.includes('<span class="statusbar__remote-label">demo · Connection failed</span>'), "failed SSH entry keeps the failure visible in the status bar");
  ok(!failed.includes("handshake failed"), "status entry keeps raw connection diagnostics out of primary chrome");

  const degraded = renderStatusBar({
    workspacePath: "/workspace/repo",
    remoteHosts,
    remoteStatuses: {
      demo: {
        hostId: "demo",
        state: "degraded",
        error: "forward attach failed",
      },
    },
  });
  ok(degraded.includes("demo · Degraded"), "degraded SSH remains connected with a warning state");
  ok(!degraded.includes("demo · Connection failed"), "degraded SSH is not mislabeled as a failed connection");
}

{
  const propsWithLegacySandbox = {
    workspacePath: "/workspace/repo",
    workspaceName: "repo",
    sandboxPath: "/sandbox/repo",
    gitBranch: "feature/meta",
  };
  const html = renderStatusBar(propsWithLegacySandbox);
  ok(html.includes("workspace/repo"), "workspace chip uses workspace path");
  ok(!html.includes("sandbox/repo"), "workspace chip does not display sandbox path");
  ok(html.includes("feature/meta"), "git branch remains visible");
}

{
  const html = renderStatusBar({
    items: ["cache"],
    workspacePath: "/workspace/repo",
    workspaceName: "repo",
    gitBranch: "feature/meta",
  });
  ok(!html.includes("workspace/repo"), "workspace can be hidden by status item config");
  ok(!html.includes("feature/meta"), "git branch can be hidden by status item config");
}

{
  const html = renderStatusBar({
    items: ["git_branch", "workspace"],
    workspacePath: "/workspace/repo",
    workspaceName: "repo",
    gitBranch: "feature/meta",
  });
  ok(html.indexOf("feature/meta") >= 0 && html.indexOf("workspace/repo") >= 0, "workspace and git branch render as configured items");
  ok(html.indexOf("feature/meta") < html.indexOf("workspace/repo"), "workspace items follow configured order");
}

{
  const html = renderStatusBar({ items: ["model"] });
  ok(!html.includes("YOLO"), "status bar renders only configured status items, not mode indicators");
  ok(!html.includes("后台作业") && !html.includes("Background jobs"), "status bar hides the operational jobs entry while idle");
}

{
  const html = renderStatusBar({
    items: ["model"],
    jobs: [{ id: "bash-1", kind: "bash", label: "run tests", status: "running", startedAt: 1 }],
  });
  ok(html.includes("Background jobs"), "running background jobs remain visible outside configurable metrics");
  ok(html.includes("1"), "background jobs entry exposes the running count");
}

{
  const html = renderStatusBar({
    items: ["model"],
    backgroundRuntimes: [{
      tabId: "running-1", title: "Detached delivery", detached: true,
      running: true, pendingPrompt: false, jobs: [],
    }],
  });
  ok(html.includes("Background jobs"), "a running detached task remains visible without child jobs");
  ok(html.includes("<b>1</b>"), "a jobless active runtime contributes to the recovery count");
}

{
  const defaultItems = DEFAULT_STATUS_BAR_ITEMS as readonly string[];
  ok(!defaultItems.includes("autoresearch"), "autoresearch is not a configurable status bar UI item");
}

{
  const estimated = renderStatusBar({
    items: ["session_tokens", "turn_tokens", "turn_cost", "cost"],
    context: { used: 0, window: 0, sessionTokens: 1_200, estimated: true },
    usage: {
      promptTokens: 800,
      completionTokens: 200,
      totalTokens: 1_000,
      cacheHitTokens: 0,
      cacheMissTokens: 800,
      estimated: true,
    },
    sessionTokens: 1_200,
    turnTokens: 1_000,
    turnCost: 0.2,
    cost: 0.3,
    currency: "USD",
  });
  ok((estimated.match(/≈/g) ?? []).length === 4, "estimated token and cost metrics use an approximation marker");

  const empty = renderStatusBar({
    items: ["session_tokens", "turn_tokens", "turn_cost", "cost"],
    context: { used: 0, window: 0, sessionTokens: 0, estimated: true },
    usage: {
      promptTokens: 0,
      completionTokens: 0,
      totalTokens: 0,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      estimated: true,
    },
    currency: "USD",
  });
  ok(!empty.includes("≈-"), "empty estimated metrics remain a plain dash");
}

{
  const exact = renderStatusBar({
    items: ["turn_tps"],
    lastTurnOutputTokens: 100,
    lastTurnModelMs: 5_000,
  });
  ok(exact.includes("20 t/s"), "completed TPS uses provider-output time");

  const estimated = renderStatusBar({
    items: ["turn_tps"],
    lastTurnOutputTokens: 100,
    lastTurnModelMs: 5_000,
    lastTurnOutputEstimated: true,
  });
  ok(estimated.includes("≈20 t/s"), "fallback TPS is visibly marked as estimated");

  const perRequest = renderStatusBar({
    items: ["turn_tps"],
    lastRequestTps: 35,
    lastTurnOutputTokens: 100,
    lastTurnModelMs: 5_000,
  });
  ok(perRequest.includes("35 t/s"), "per-request TPS wins over the completed turn value");

  const slowRequest = renderStatusBar({
    items: ["turn_tps"], lastRequestTps: 1 / 3, lastTurnOutputTokens: 100, lastTurnModelMs: 5_000,
  });
  ok(slowRequest.includes("&lt;1 t/s") && !slowRequest.includes("20 t/s"), "sub-one request TPS replaces the stale turn fallback");

  const unavailable = renderStatusBar({
    items: ["turn_tps"], lastRequestTps: null, lastTurnOutputTokens: 100, lastTurnModelMs: 5_000,
  });
  ok(unavailable.includes('stat__value--empty">-</b>') && !unavailable.includes("20 t/s"), "unmeasured latest requests clear the stale turn fallback");
}

{
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  // Node's built-in navigator reflects the machine's ICU locale; pin jsdom's
  // en-US one so English-string assertions hold on zh-locale machines.
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
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

  let stopped = "";
  const rootEl = document.getElementById("root")!;
  const root = createRoot(rootEl);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <StatusBar
          context={{ used: 0, window: 0, sessionTokens: 0 }}
          running={false}
          jobs={[{ id: "bash-1", kind: "bash", label: "run tests", status: "running", startedAt: 1 }]}
          onCancelJob={async (jobID) => { stopped = jobID; return true; }}
        />
      </LocaleProvider>,
    );
  });
  const jobsButton = rootEl.querySelector<HTMLButtonElement>(".statusbar__jobs-trigger");
  await act(async () => { jobsButton?.click(); });
  const stopButton = document.body.querySelector<HTMLButtonElement>(".jobs-popover__stop");
  await act(async () => { stopButton?.click(); await Promise.resolve(); });
  ok(stopped === "bash-1", "background jobs popover routes Stop to the selected job");

  let routed = "";
  let revealed = "";
  await act(async () => {
    root.render(
      <LocaleProvider>
        <StatusBar
          context={{ used: 0, window: 0, sessionTokens: 0 }}
          running={false}
          backgroundRuntimes={[
            {
              tabId: "detached-1", title: "Detached delivery", detached: true,
              running: false, pendingPrompt: false,
              jobs: [{ id: "go-1", kind: "go", label: "go test", status: "running", startedAt: 1 }],
            },
          ]}
          onCancelRuntimeJob={async (tabID, jobID) => { routed = `${tabID}:${jobID}`; return true; }}
          onRevealRuntime={async (tabID) => { revealed = tabID; }}
        />
      </LocaleProvider>,
    );
  });
  const globalJobsButton = rootEl.querySelector<HTMLButtonElement>(".statusbar__jobs-trigger");
  if (globalJobsButton?.getAttribute("aria-expanded") !== "true") {
    await act(async () => { globalJobsButton?.click(); });
  }
  const globalStop = document.body.querySelector<HTMLButtonElement>(".jobs-popover__stop");
  const openTask = Array.from(document.body.querySelectorAll<HTMLButtonElement>(".jobs-popover__runtime-header button"))[0];
  await act(async () => { globalStop?.click(); openTask?.click(); await Promise.resolve(); });
  ok(routed === "detached-1:go-1", "global jobs route Stop to the owning detached task");
  ok(revealed === "detached-1", "global jobs can reopen the exact detached task");

  revealed = "";
  await act(async () => {
    root.render(
      <LocaleProvider>
        <StatusBar
          context={{ used: 0, window: 0, sessionTokens: 0 }}
          running={false}
          backgroundRuntimes={[
            {
              tabId: "prompt-1", title: "Waiting delivery", detached: true,
              running: false, pendingPrompt: true, jobs: [],
            },
          ]}
          onRevealRuntime={async (tabID) => { revealed = tabID; }}
        />
      </LocaleProvider>,
    );
  });
  const promptJobsButton = rootEl.querySelector<HTMLButtonElement>(".statusbar__jobs-trigger");
  if (promptJobsButton?.getAttribute("aria-expanded") !== "true") {
    await act(async () => { promptJobsButton?.click(); });
  }
  ok(document.body.textContent?.includes("Waiting for input") === true, "pending-prompt runtime explains why it remains active");
  const promptOpenTask = document.body.querySelector<HTMLButtonElement>(".jobs-popover__runtime-header button");
  await act(async () => { promptOpenTask?.click(); await Promise.resolve(); });
  ok(revealed === "prompt-1", "a pending-prompt runtime can be reopened without child jobs");

  await act(async () => {
    root.render(
      <LocaleProvider>
        <StatusBar
          context={{ used: 0, window: 0, sessionTokens: 0 }}
          running={false}
          jobs={[{ id: "local-job", kind: "go", label: "local test", status: "running", startedAt: 1 }]}
        />
      </LocaleProvider>,
    );
  });
  const mixedJobsButton = rootEl.querySelector<HTMLButtonElement>(".statusbar__jobs-trigger");
  ok(mixedJobsButton?.textContent?.includes("1") === true, "local jobs show in the status bar total");
  if (mixedJobsButton?.getAttribute("aria-expanded") !== "true") {
    await act(async () => { mixedJobsButton?.click(); });
  }
  ok(document.body.textContent?.includes("local test") === true, "local background jobs remain visible");
  await act(async () => { root.unmount(); });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
