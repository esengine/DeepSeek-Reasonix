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
  ok(stopped.includes("SSH · Disconnected"), "configured SSH entry remains visible while disconnected");
  ok(stopped.indexOf("SSH · Disconnected") < stopped.indexOf("workspace/repo"), "window-level SSH entry leads the status bar");

  const connected = renderStatusBar({
    workspacePath: "/workspace/repo",
    workspaceName: "repo",
    remoteHosts,
    remoteStatuses: { demo: { hostId: "demo", state: "connected" } },
  });
  ok(connected.includes("demo · Connected"), "SSH entry includes host and connected state text");

  const failed = renderStatusBar({
    workspacePath: "/workspace/repo",
    remoteHosts,
    remoteStatuses: { demo: { hostId: "demo", state: "stopped", error: "handshake failed" } },
  });
  ok(failed.includes("demo · Connection failed"), "SSH entry keeps a recoverable failure summary visible");
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
  const defaultItems = DEFAULT_STATUS_BAR_ITEMS as readonly string[];
  ok(!defaultItems.includes("autoresearch"), "autoresearch is not a configurable status bar UI item");
}

{
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
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
  await act(async () => { root.unmount(); });
  dom.window.close();
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
