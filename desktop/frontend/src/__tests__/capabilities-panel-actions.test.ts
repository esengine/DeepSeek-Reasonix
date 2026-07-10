import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { MCPServersSettingsPage, PluginsSettingsPage } from "../components/CapabilitiesPanel";
import type { AppBindings } from "../lib/bridge";
import { LocaleProvider } from "../lib/i18n";
import { mcpServerLifecycleActions, mcpServerRetryableFromAvailableList } from "../lib/mcpServerLifecycle";
import type { Meta, PluginInstallOptions, PluginView, ServerView, TabMeta } from "../lib/types";

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
  globalThis.HTMLInputElement = dom.window.HTMLInputElement;
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

function setInputValue(input: HTMLInputElement, value: string) {
  const win = input.ownerDocument.defaultView;
  const setter = Object.getOwnPropertyDescriptor((win?.HTMLInputElement ?? HTMLInputElement).prototype, "value")?.set;
  setter?.call(input, value);
  const eventCtor = win?.Event ?? Event;
  const inputEventCtor = win?.InputEvent ?? eventCtor;
  input.dispatchEvent(new inputEventCtor("input", { bubbles: true, data: value, inputType: "insertText" } as InputEventInit));
  input.dispatchEvent(new eventCtor("change", { bubbles: true }));
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

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const meta: Meta = { label: "test", ready: true, eventChannel: "managed-mcp-channel", cwd: "/tmp/reasonix-test", workspaceRoot: "/tmp/reasonix-test" };
  const tabs: TabMeta[] = [{
    id: "tab-managed-mcp",
    scope: "project",
    workspaceRoot: "/tmp/reasonix-test",
    workspaceName: "reasonix-test",
    topicId: "topic-managed-mcp",
    topicTitle: "Managed MCP",
    label: "Managed MCP",
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "auto",
    active: true,
    cwd: "/tmp/reasonix-test",
  }];
  const servers: ServerView[] = [{
    name: "helper",
    transport: "http",
    status: "connected",
    configured: true,
    managedByPlugin: "superpowers",
    authConfigured: true,
    autoStart: true,
    tools: 1,
    prompts: 0,
    resources: 0,
    toolList: [{ name: "echo", description: "Echo input", readOnlyHint: true }],
  }];
  window.go = {
    main: {
      App: {
        Meta: async () => meta,
        ListTabs: async () => tabs,
        MCPServers: async () => servers,
      } as Partial<AppBindings> as AppBindings,
    },
  };

  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(MCPServersSettingsPage)));
    await flush();
  });
  await waitFor("plugin-managed MCP row", () => Boolean(document.querySelector(".cap-row__name")?.textContent?.includes("helper")));
  ok(document.body.textContent?.includes("Managed by plugin superpowers") ?? false, "plugin-managed MCP identifies its owner");

  const disclosure = document.querySelector<HTMLButtonElement>(".cap-disclosure");
  if (!disclosure) throw new Error("missing plugin-managed MCP disclosure");
  await act(async () => {
    disclosure.click();
    await flush();
  });
  ok(!findButton("Remove server"), "plugin-managed MCP hides the misleading remove action");
  ok(!findButton("Edit config"), "plugin-managed MCP hides direct config editing");
  ok(!findButton("Clear auth"), "plugin-managed MCP hides auth persistence actions");
  ok(!findButton("Pre-trust read-only (1)"), "plugin-managed MCP hides bulk trust persistence actions");

  const viewTools = findButton("View tools");
  if (!viewTools) throw new Error("missing managed MCP View tools button");
  await act(async () => {
    viewTools.click();
    await flush();
  });
  ok(!findButton("Pre-trust"), "plugin-managed MCP hides per-tool trust persistence actions");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const meta: Meta = { label: "test", ready: true, eventChannel: "runtime-mcp-channel", cwd: "/tmp/reasonix-test", workspaceRoot: "/tmp/reasonix-test" };
  const tabs: TabMeta[] = [{
    id: "tab-runtime-mcp",
    scope: "project",
    workspaceRoot: "/tmp/reasonix-test",
    workspaceName: "reasonix-test",
    topicId: "topic-runtime-mcp",
    topicTitle: "Runtime MCP",
    label: "Runtime MCP",
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "auto",
    active: true,
    cwd: "/tmp/reasonix-test",
  }];
  const servers: ServerView[] = [{
    name: "runtime-only",
    transport: "stdio",
    status: "failed",
    configured: false,
    autoStart: false,
    tools: 0,
    prompts: 0,
    resources: 0,
    error: "command not found",
  }];
  window.go = {
    main: {
      App: {
        Meta: async () => meta,
        ListTabs: async () => tabs,
        MCPServers: async () => servers,
      } as Partial<AppBindings> as AppBindings,
    },
  };

  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(MCPServersSettingsPage)));
    await flush();
  });
  await waitFor("runtime-only failure details", () => Boolean(findButton("View details")));
  const showDetails = findButton("View details");
  if (!showDetails) throw new Error("missing runtime-only failure details button");
  await act(async () => {
    showDetails.click();
    await flush();
  });
  ok(!findButton("Remove server"), "runtime-only MCP failure hides an action the backend cannot persist");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

console.log("capabilities panel plugin actions");

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const meta: Meta = { label: "test", ready: true, eventChannel: "plugin-channel", cwd: "/tmp/reasonix-test", workspaceRoot: "/tmp/reasonix-test" };
  const tabs: TabMeta[] = [{
    id: "tab-plugin",
    scope: "project",
    workspaceRoot: "/tmp/reasonix-test",
    workspaceName: "reasonix-test",
    topicId: "topic-plugin",
    topicTitle: "Plugins",
    label: "Plugins",
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "auto",
    active: true,
    cwd: "/tmp/reasonix-test",
  }];
  let planCalls = 0;
  let installCalls = 0;
  let toggleCalls = 0;
  let updateCalls = 0;
  let doctorCalls = 0;
  let removeCalls = 0;
  let pickFolderCalls = 0;
  const plannedSources: string[] = [];
  const installedSources: string[] = [];
  let plugins: PluginView[] = [{
    name: "superpowers",
    version: "0.1.0",
    description: "Shared agent skills and hooks.",
    source: "git:github.com/obra/superpowers",
    root: "~/.reasonix/plugins/superpowers",
    manifestKind: "reasonix",
    enabled: true,
    skills: 2,
    hooks: 1,
    mcpServers: 0,
  }];
  window.go = {
    main: {
      App: {
        Meta: async () => meta,
        ListTabs: async () => tabs,
        Plugins: async () => plugins.map((plugin) => ({ ...plugin, warnings: [...(plugin.warnings ?? [])] })),
        PlanPluginInstall: async (source: string, options: PluginInstallOptions) => {
          planCalls += 1;
          plannedSources.push(source);
          ok(options.dryRun === true, "plugin preview asks for dry-run planning");
          return JSON.stringify({
            ok: true,
            status: "planned",
            name: "superpowers",
            actions: [{ kind: "plugin", action: "install_plugin_package", name: "superpowers", source, status: "planned" }],
          });
        },
        InstallPlugin: async (source: string, _options: PluginInstallOptions) => {
          installCalls += 1;
          installedSources.push(source);
          const next: PluginView = {
            name: "superpowers",
            version: "0.1.1",
            description: "Shared agent skills and hooks.",
            source,
            root: "~/.reasonix/plugins/superpowers",
            manifestKind: "reasonix",
            enabled: true,
            skills: 3,
            hooks: 1,
            mcpServers: 1,
            skillDetails: [{ name: "plan", description: "Plan work before implementation.", invocation: "/plan", runAs: "inline" }],
            hookDetails: [{ event: "SessionStart", contextFile: "CLAUDE.md", description: "Load startup context." }],
            mcpServerDetails: [{ name: "context", transport: "stdio", command: "node server.js" }],
          };
          plugins = plugins.filter((plugin) => plugin.name !== next.name).concat(next);
          return JSON.stringify({ ok: true, status: "done", actions: [{ action: "install_plugin_package", name: next.name, status: "done" }] });
        },
        SetPluginEnabled: async (name: string, enabled: boolean) => {
          toggleCalls += 1;
          plugins = plugins.map((plugin) => plugin.name === name ? { ...plugin, enabled } : plugin);
        },
        UpdatePlugin: async (name: string) => {
          updateCalls += 1;
          plugins = plugins.map((plugin) => plugin.name === name ? { ...plugin, version: "0.1.2" } : plugin);
          return JSON.stringify({ ok: true, status: "done", name });
        },
        PluginDoctor: async (name: string) => {
          doctorCalls += 1;
          return { ...(plugins.find((plugin) => plugin.name === name) ?? plugins[0]), warnings: ["manifest exports no MCP auth metadata"] };
        },
        RemovePlugin: async (name: string) => {
          removeCalls += 1;
          plugins = plugins.filter((plugin) => plugin.name !== name);
        },
        PickPluginFolder: async () => {
          pickFolderCalls += 1;
          return "/tmp/superpowers-plugin";
        },
      } as Partial<AppBindings> as AppBindings,
    },
  };

  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(PluginsSettingsPage)));
    await flush();
  });
  await waitFor("superpowers plugin row", () => Boolean(document.querySelector(".cap-row__name")?.textContent?.includes("superpowers")));
  ok(Boolean(document.querySelector(".cap-plugin-form-grid .cap-plugin-fields--local")), "local plugin install mode uses the shared form grid");
  const localOptionTexts = Array.from(document.querySelectorAll(".cap-plugin-installer__options > .cap-plugin-option-block"))
    .map((option) => option.textContent ?? "");
  ok(localOptionTexts[0]?.includes("Overwrite same-name plugin"), "local install mode shows overwrite before link mode");
  ok(localOptionTexts[1]?.includes("Developer mode: link source folder"), "local install mode shows link mode after overwrite");

  const chooseFolder = findButton("Choose plugin folder");
  if (!chooseFolder) throw new Error("missing plugin folder picker button");
  await act(async () => {
    chooseFolder.click();
    await flush();
  });
  await waitFor("picked plugin folder source", () => document.body.textContent?.includes("/tmp/superpowers-plugin") ?? false);
  ok(pickFolderCalls === 1, "clicking Choose folder invokes the plugin folder picker once");

  const gitMode = findButton("Git repository");
  if (!gitMode) throw new Error("missing Git repository install mode");
  await act(async () => {
    gitMode.click();
    await flush();
  });
  ok(Boolean(document.querySelector(".cap-plugin-form-grid .cap-plugin-fields--git")), "Git plugin install mode uses the shared form grid");
  const sourceInput = document.querySelector<HTMLInputElement>('input[aria-label="Git repository URL"]');
  if (!sourceInput) throw new Error("missing plugin git source input");
  await act(async () => {
    setInputValue(sourceInput, "git:github.com/obra/superpowers");
    await flush();
  });
  await waitFor("plugin preview enabled", () => findButton("Preview")?.disabled === false);

  const preview = findButton("Preview");
  if (!preview) throw new Error("missing plugin preview button");
  await act(async () => {
    preview.click();
    await flush();
  });
  await waitFor("plugin install plan", () => document.body.textContent?.includes("install_plugin_package") ?? false);
  ok(planCalls === 1, "clicking Preview invokes plugin install planning once");
  ok(plannedSources[0] === "git:github.com/obra/superpowers", "plugin preview receives the entered Git source");

  const install = findButton("Install plugin");
  if (!install) throw new Error("missing plugin install button");
  await act(async () => {
    install.click();
    await flush();
  });
  await waitFor("plugin install result", () => installCalls === 1 && plugins[0]?.version === "0.1.1");
  ok(installedSources[0] === "git:github.com/obra/superpowers", "plugin install receives the entered Git source");

  const disclosure = document.querySelector<HTMLButtonElement>(".cap-plugin-entry .cap-disclosure");
  if (!disclosure) throw new Error("missing plugin disclosure");
  await act(async () => {
    disclosure.click();
    await flush();
  });
  await waitFor("plugin update action", () => Boolean(findButton("Update")));
  ok(document.body.textContent?.includes("How to use") ?? false, "expanded plugin details explain how to use the plugin");
  ok(document.body.textContent?.includes("/plan") ?? false, "expanded plugin details list exported skill invocations");
  ok(document.body.textContent?.includes("SessionStart") ?? false, "expanded plugin details list exported hooks");
  ok(document.body.textContent?.includes("context") ?? false, "expanded plugin details list exported MCP servers");

  const update = findButton("Update");
  if (!update) throw new Error("missing plugin update button");
  await act(async () => {
    update.click();
    await flush();
  });
  await waitFor("plugin update call", () => updateCalls === 1 && plugins[0]?.version === "0.1.2");

  const doctor = findButton("Doctor");
  if (!doctor) throw new Error("missing plugin doctor button");
  await act(async () => {
    doctor.click();
    await flush();
  });
  await waitFor("plugin diagnostic warning", () => document.body.textContent?.includes("manifest exports no MCP auth metadata") ?? false);
  ok(doctorCalls === 1, "clicking Doctor invokes plugin diagnostics once");

  const toggle = document.querySelector<HTMLInputElement>(".cap-plugin-entry .cap-switch input");
  if (!toggle) throw new Error("missing plugin enable toggle");
  await act(async () => {
    toggle.click();
    await flush();
  });
  await waitFor("plugin disabled", () => toggleCalls === 1 && plugins[0]?.enabled === false);

  const remove = findButton("Remove plugin");
  if (!remove) throw new Error("missing plugin remove button");
  await act(async () => {
    remove.click();
    await flush();
  });
  const confirmRemove = findButton("Confirm remove");
  if (!confirmRemove) throw new Error("missing plugin confirm remove button");
  await act(async () => {
    confirmRemove.click();
    await flush();
  });
  await waitFor("plugin removed", () => removeCalls === 1 && plugins.length === 0);

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}
