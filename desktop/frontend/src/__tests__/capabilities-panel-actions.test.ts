import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { MCPServersSettingsPage } from "../components/CapabilitiesPanel";
import type { AppBindings } from "../lib/bridge";
import { LocaleProvider } from "../lib/i18n";
import { mcpServerLifecycleActions, mcpServerRetryableFromAvailableList } from "../lib/mcpServerLifecycle";
import type { Meta, ServerView, TabMeta } from "../lib/types";

function ok(value: unknown, message: string) {
  if (!value) throw new Error(message);
}

function server(status: ServerView["status"]): ServerView {
  return {
    name: "codegraph",
    transport: "stdio",
    status,
    configured: true,
    autoStart: true,
    tier: "background",
    tools: 0,
    prompts: 0,
    resources: 0,
  };
}

const initializing = mcpServerLifecycleActions(server("initializing"));
ok(initializing.enabled, "initializing server should still be treated as enabled");
ok(!initializing.showRetryInRow, "initializing server should not expose retry until it fails");
ok(!initializing.canReconnect, "initializing server should not expose reconnect while already connecting");
ok(!initializing.canConnectNow, "initializing server should not use the deferred connect-now action");

const connected = mcpServerLifecycleActions(server("connected"));
ok(!connected.showRetryInRow, "connected server row should keep the toggle UI");
ok(connected.canReconnect, "connected server details should expose reconnect");

const manuallyConnected = mcpServerLifecycleActions({ ...server("connected"), autoStart: false, startIntent: "off", runtimeState: "ready" });
ok(manuallyConnected.enabled, "connected manual server should still render as enabled");
ok(!manuallyConnected.canConnectNow, "connected manual server should not expose connect-now");
ok(manuallyConnected.canReconnect, "connected manual server should expose reconnect");

const automaticIdle = mcpServerLifecycleActions({ ...server("deferred"), startIntent: "automatic" });
ok(!automaticIdle.canConnectNow, "automatic idle server should not look like a manual connector");
ok(!automaticIdle.canReconnect, "automatic idle server should wait for background connection or failure");

const failed = mcpServerLifecycleActions({ ...server("failed"), runtimeState: "issue" });
ok(failed.showRetryInRow, "failed server row should expose retry");

ok(mcpServerRetryableFromAvailableList(server("initializing")), "connecting server should be included in available-list retry all");
ok(mcpServerRetryableFromAvailableList({ ...server("deferred"), startIntent: "automatic" }), "automatic idle server should be included in available-list retry all");
ok(!mcpServerRetryableFromAvailableList(server("connected")), "connected server should be excluded from available-list retry all");
ok(!mcpServerRetryableFromAvailableList({ ...server("disabled"), startIntent: "off" }), "disabled server should be excluded from available-list retry all");
ok(!mcpServerRetryableFromAvailableList({ ...server("failed"), runtimeState: "issue" }), "failed server is handled by the failure banner retry all");

function flush(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    await act(async () => {
      await flush();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}`);
}

function installDom() {
  const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.HTMLButtonElement = dom.window.HTMLButtonElement;
  globalThis.Event = dom.window.Event;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: () => ({
      matches: true,
      media: "(prefers-reduced-motion: reduce)",
      onchange: null,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
      dispatchEvent: () => false,
    }),
  });
  return dom;
}

function findButton(label: string): HTMLButtonElement | undefined {
  return Array.from(document.querySelectorAll("button")).find((button) => button.textContent?.trim() === label) as HTMLButtonElement | undefined;
}

console.log("capabilities panel MCP actions");

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const meta: Meta = { label: "test", ready: true, eventChannel: "test-channel", cwd: "/tmp/reasonix-test", workspaceRoot: "/tmp/reasonix-test" };
  const tabs: TabMeta[] = [{
    id: "tab-1",
    scope: "project",
    workspaceRoot: "/tmp/reasonix-test",
    workspaceName: "reasonix-test",
    topicId: "topic-1",
    topicTitle: "Test",
    label: "Test",
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "auto",
    active: true,
    cwd: "/tmp/reasonix-test",
  }];
  let trustCalls = 0;
  let bulkTrustCalls = 0;
  let untrustCalls = 0;
  let servers: ServerView[] = [{
    name: "github",
    transport: "stdio",
    status: "connected",
    configured: true,
    autoStart: true,
    tools: 2,
    prompts: 0,
    resources: 0,
    toolList: [
      { name: "issue_read", description: "Read issues.", readOnlyHint: true },
      { name: "issue_write", description: "Write issues." },
    ],
    trustedReadOnlyTools: [],
  }];
  window.go = {
    main: {
      App: {
        Meta: async () => meta,
        ListTabs: async () => tabs,
        MCPServers: async () => servers.map((s) => ({
          ...s,
          toolList: s.toolList?.map((tool) => ({ ...tool })),
          trustedReadOnlyTools: [...(s.trustedReadOnlyTools ?? [])],
        })),
        TrustMCPServerTool: async (name: string, toolName: string) => {
          trustCalls += 1;
          servers = servers.map((s) => s.name === name ? { ...s, trustedReadOnlyTools: [...(s.trustedReadOnlyTools ?? []), toolName] } : s);
        },
        TrustMCPServerTools: async (name: string, toolNames: string[]) => {
          bulkTrustCalls += 1;
          servers = servers.map((s) => {
            if (s.name !== name) return s;
            const trusted = Array.from(new Set([...(s.trustedReadOnlyTools ?? []), ...toolNames]));
            return { ...s, trustedReadOnlyTools: trusted };
          });
        },
        UntrustMCPServerTool: async (name: string, toolName: string) => {
          untrustCalls += 1;
          servers = servers.map((s) => s.name === name ? { ...s, trustedReadOnlyTools: (s.trustedReadOnlyTools ?? []).filter((tool) => tool !== toolName) } : s);
        },
      } as Partial<AppBindings> as AppBindings,
    },
  };

  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(MCPServersSettingsPage)));
    await flush();
  });
  await waitFor("github server row", () => Boolean(document.querySelector(".cap-row__name")?.textContent?.includes("github")));

  const disclosure = document.querySelector<HTMLButtonElement>(".cap-disclosure");
  if (!disclosure) throw new Error("missing MCP disclosure button");
  await act(async () => {
    disclosure.click();
    await flush();
  });

  const trustReadOnly = findButton("Pre-trust read-only (1)");
  if (!trustReadOnly) throw new Error("missing bulk Pre-trust read-only button");
  await act(async () => {
    trustReadOnly.click();
    await flush();
  });
  await waitFor("bulk trusted tool", () => servers[0]?.trustedReadOnlyTools?.includes("issue_read") ?? false);

  const viewTools = findButton("View tools");
  if (!viewTools) throw new Error("missing View tools button");
  await act(async () => {
    viewTools.click();
    await flush();
  });

  await waitFor("trusted badge", () => Boolean(document.querySelector(".cap-tool-trust")?.textContent?.includes("Trusted")));
  const untrust = findButton("Untrust");
  if (!untrust) throw new Error("missing Untrust button");
  await act(async () => {
    untrust.click();
    await flush();
  });
  await waitFor("untrusted tool", () => !(servers[0]?.trustedReadOnlyTools?.includes("issue_read") ?? false));

  await waitFor("Pre-trust button", () => Boolean(findButton("Pre-trust")));
  const trust = findButton("Pre-trust");
  if (!trust) throw new Error("missing Pre-trust button");
  await act(async () => {
    trust.click();
    await flush();
  });

  await waitFor("trusted badge", () => Boolean(document.querySelector(".cap-tool-trust")?.textContent?.includes("Trusted")));
  ok(bulkTrustCalls === 1, "clicking Pre-trust read-only invokes the MCP bulk trust action once");
  ok(untrustCalls === 1, "clicking Untrust invokes the MCP untrust action once");
  ok(trustCalls === 1, "clicking Trust invokes the MCP trust action once");
  ok(servers[0]?.trustedReadOnlyTools?.includes("issue_read") ?? false, "trusted raw tool name is added to the server snapshot");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}
