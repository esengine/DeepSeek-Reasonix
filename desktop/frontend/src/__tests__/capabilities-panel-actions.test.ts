import { JSDOM } from "jsdom";
import { readFileSync } from "node:fs";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { MCPServersSettingsPage, PluginsSettingsPage, failureKind, mcpServerDraftJSON, parseMCPQuickDefinition, parseMCPServerJSON, summarizeServerError, withExplicitMCPClears } from "../components/CapabilitiesPanel";
import { slashCommandGroup, slashCommandKindTag, sortSlashCommandsForMenu } from "../components/SlashMenu";
import { selectToolsOnFirstCustomUse } from "../components/SubagentsPanel";
import type { AppBindings } from "../lib/bridge";
import { LocaleProvider, t } from "../lib/i18n";
import { mcpServerLifecycleActions, mcpServerRetryableFromAvailableList } from "../lib/mcpServerLifecycle";
import type { MCPServerInput, Meta, PluginInstallOptions, PluginView, ServerView, TabMeta } from "../lib/types";

function ok(value: unknown, message: string) {
  if (!value) throw new Error(message);
}

{
  const dom = installDom();
  const rootEl = document.getElementById("root");
  if (!rootEl) throw new Error("missing root");
  const root = createRoot(rootEl);
  const meta: Meta = { label: "test", ready: true, eventChannel: "mcp-registry-channel", cwd: "/tmp/reasonix-test", workspaceRoot: "/tmp/reasonix-test" };
  const tabs: TabMeta[] = [{
    id: "tab-mcp-registry",
    scope: "project",
    workspaceRoot: "/tmp/reasonix-test",
    workspaceName: "reasonix-test",
    topicId: "topic-mcp-registry",
    topicTitle: "Registry",
    label: "Registry",
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "auto",
    active: true,
    cwd: "/tmp/reasonix-test",
  }];
  let servers: ServerView[] = [];
  let installed: MCPServerInput | null = null;
  let registryCached = false;
  let resolvedRegistryName = "";
  const registryEntry = {
    name: "io.example/demo",
    suggestedName: "demo",
    title: "Demo MCP",
    description: "Registry demo server",
    version: "1.0.0",
    installable: true,
    transport: "http",
    args: [],
    url: "https://mcp.example.test/mcp",
  };
  window.go = {
    main: {
      App: {
        Meta: async () => meta,
        ListTabs: async () => tabs,
        MCPServers: async () => servers,
        MCPMarketplace: async () => ({
          cached: registryCached,
          warning: registryCached ? "offline" : undefined,
          servers: [registryEntry],
        }),
        MCPMarketplaceResolve: async (registryName) => {
          resolvedRegistryName = registryName;
          return registryEntry;
        },
        AddMCPServer: async (input) => {
          installed = input;
          servers = [{
            name: input.name,
            transport: input.transport,
            status: "connected",
            configured: true,
            autoStart: true,
            tools: 1,
            prompts: 0,
            resources: 0,
            url: input.url,
          }];
          return 1;
        },
        InstallMCPServer: async (input) => {
          const app = window.go?.main?.App;
          if (!app) throw new Error("missing App bindings");
          const toolCount = await app.AddMCPServer(input);
          return { name: input.name, state: "ready", toolCount, action: "none", message: "ready" };
        },
      } as Partial<AppBindings> as AppBindings,
    },
  };

  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(MCPServersSettingsPage)));
    await flush();
  });
  await waitFor("registry browse action", () => Boolean(findButton("Browse registry")));
  await act(async () => {
    findButton("Browse registry")?.click();
    await flush();
  });
  await waitFor("registry result", () => document.body.textContent?.includes("Demo MCP") ?? false);
  await act(async () => {
    findButton("Install")?.click();
    await flush();
  });
  await waitFor("registry install", () => installed !== null && document.body.textContent?.includes("demo") === true);
  const installedEntry = installed as MCPServerInput | null;
  ok(installedEntry?.name === "demo" && installedEntry.transport === "http" && installedEntry.url === "https://mcp.example.test/mcp", "registry install converts the selected entry into the normal add-and-connect input");
  ok(resolvedRegistryName === "io.example/demo", "registry install re-resolves current metadata by canonical name");

  registryCached = true;
  installed = null;
  await act(async () => {
    findButton("Browse registry")?.click();
    await flush();
    findButton("Search")?.click();
    await flush();
  });
  await waitFor("cached registry warning", () => document.body.textContent?.includes("Showing cached results") ?? false);
  const cachedInstall = findButton("Install");
  ok(cachedInstall?.disabled === true, "cached Registry results must remain browse-only");
  cachedInstall?.click();
  await flush();
  ok(installed === null, "cached Registry result must not be installed");

  await act(async () => {
    root.unmount();
  });
  dom.window.close();
}

const quickCommand = parseMCPQuickDefinition("npx -y chrome-devtools-mcp@latest");
ok(quickCommand.name === "chrome-devtools-mcp" && quickCommand.transport === "stdio", "quick install should derive a stable name and stdio transport from one command");

const quickFilesystem = parseMCPQuickDefinition('npx -y @modelcontextprotocol/server-filesystem "/srv/shared data"');
ok(quickFilesystem.name === "server-filesystem", "quick install name should come from the launcher package, not a trailing server argument");

const quickPythonModule = parseMCPQuickDefinition("python -m mcp_server_time --local-timezone=UTC");
ok(quickPythonModule.name === "mcp-server-time", "python module quick install should derive its name from the module");
const quickURL = parseMCPQuickDefinition("https://mcp.linear.app/mcp");
ok(quickURL.name === "mcp" && quickURL.transport === "http", "quick install should derive HTTP transport from a URL");
const quickJSON = parseMCPQuickDefinition(JSON.stringify({ custom: { command: "uvx", args: ["demo-mcp"] } }));
ok(quickJSON.name === "custom" && quickJSON.args[0] === "demo-mcp", "quick install should preserve advanced JSON definitions");

const completeMCPJSON = JSON.stringify({
  admin: {
    type: "streamable-http",
    url: "https://mcp.example.test/api",
    auto_start: false,
    call_timeout_seconds: 45,
    tool_timeout_seconds: { wipe: 120 },
    trusted_read_only_tools: ["status"],
    default_tools_approval_mode: "writes",
    tools: { wipe: { approval_mode: "prompt" } },
    approvals_reviewer: "auto_review",
  },
});
const completeMCP = parseMCPServerJSON(completeMCPJSON);
ok(completeMCP.input.transport === "http", "streamable-http should normalize to http");
ok(completeMCP.input.autoStart === false, "advanced JSON should preserve auto_start=false");
ok(completeMCP.input.callTimeoutSeconds === 45 && completeMCP.input.toolTimeoutSeconds?.wipe === 120, "advanced JSON should preserve timeouts");
const completeMCPRoundTrip = parseMCPServerJSON(mcpServerDraftJSON(completeMCP.draft));
ok(completeMCPRoundTrip.input.transport === "http" && completeMCPRoundTrip.input.toolTimeoutSeconds?.wipe === 120, "Form/JSON switching should preserve connection fields");
const normalizedMCPJSON = mcpServerDraftJSON(completeMCP.draft);
ok(!normalizedMCPJSON.includes("trusted_read_only_tools"), "Form/JSON switching should drop the removed reader setting");
ok(!normalizedMCPJSON.includes("approval_mode") && !normalizedMCPJSON.includes("approvals_reviewer"), "Form/JSON switching should drop retired MCP approval settings");
let unsupportedMCPFieldRejected = false;
try {
  parseMCPServerJSON(JSON.stringify({ admin: { command: "admin-mcp", unsupported: true } }));
} catch (error) {
  unsupportedMCPFieldRejected = error instanceof Error && error.message === "unsupported";
}
ok(unsupportedMCPFieldRejected, "unsupported advanced JSON fields should fail explicitly");
const incompleteMCPJSON = JSON.stringify({ admin: { type: "stdio", command: "" } });
let incompleteMCPRejected = false;
try {
  parseMCPServerJSON(incompleteMCPJSON);
} catch (error) {
  incompleteMCPRejected = error instanceof Error && error.message === "required";
}
ok(incompleteMCPRejected, "submitting incomplete MCP JSON must still require a command or URL");
const incompleteMCPDraft = parseMCPServerJSON(incompleteMCPJSON, undefined, { allowIncomplete: true });
ok(incompleteMCPDraft.draft.name === "admin" && incompleteMCPDraft.draft.command === "", "mode switching may recover an incomplete MCP draft for form editing");
parseMCPServerJSON(JSON.stringify({ admin: { command: "admin-mcp", default_tools_approval_mode: "", approvals_reviewer: "" } }));
let nullToolTimeoutRejected = false;
try {
  parseMCPServerJSON(JSON.stringify({ admin: { command: "admin-mcp", tool_timeout_seconds: { wipe: null } } }));
} catch (error) {
  nullToolTimeoutRejected = error instanceof Error && error.message === "invalid";
}
ok(nullToolTimeoutRejected, "a null per-tool timeout must be rejected instead of silently clearing all timeouts");
const sparseEdit = withExplicitMCPClears(parseMCPServerJSON(JSON.stringify({ admin: { command: "admin-mcp" } })).input);
ok(sparseEdit.callTimeoutSeconds === 0, "editing an existing server with fields removed must clear the timeout");
ok(sparseEdit.autoStart === true && Object.keys(sparseEdit.toolTimeoutSeconds ?? { x: 1 }).length === 0, "removed timeout fields must clear");
ok(sparseEdit.env === null && sparseEdit.headers === null, "absent env/headers must stay preserve-on-absent because their values are never seeded into the editor");

const refusedRegistryError = [
  'plugin "fs": read EOF: stderr:',
  "npm error code ECONNREFUSED",
  "npm error syscall connect",
  "npm error FetchError: request to https://registry.npmjs.org/@modelcontextprotocol%2fserver-filesystem failed, reason: connect ECONNREFUSED 127.0.0.1:7890",
].join("\n");
ok(
  summarizeServerError(refusedRegistryError) === "fs: npm ECONNREFUSED · registry.npmjs.org → 127.0.0.1:7890",
  "npm connection failures should identify both the registry and the refused endpoint",
);
const legacyNpmRefusedRegistryError = [
  'plugin "fs": read EOF: stderr:',
  "npm ERR! code ECONNREFUSED",
  "npm ERR! syscall connect",
  "npm ERR! FetchError: request to https://registry.npmjs.org/@modelcontextprotocol%2fserver-filesystem failed, reason: connect ECONNREFUSED 127.0.0.1:7890",
].join("\n");
ok(
  summarizeServerError(legacyNpmRefusedRegistryError) === "fs: npm ECONNREFUSED · registry.npmjs.org → 127.0.0.1:7890",
  "legacy npm ERR! failures should identify both the registry and the refused endpoint",
);
const credentialedRegistryError =
  'plugin "private": stderr: npm error code ECONNREFUSED npm error request to https://build-user:registry-secret@packages.example.test/npm failed, reason: connect ECONNREFUSED proxy.internal.test:8443';
const credentialedRegistrySummary = summarizeServerError(credentialedRegistryError);
ok(credentialedRegistrySummary.includes("packages.example.test → proxy.internal.test:8443"), "private registries should keep actionable hosts");
ok(!credentialedRegistrySummary.includes("build-user") && !credentialedRegistrySummary.includes("registry-secret"), "registry credentials must not appear in the summary");
ok(
  failureKind({ ...server("failed"), error: refusedRegistryError }) === "network",
  "npm connection refusal should be grouped as a network/proxy issue",
);

const subagentTools = [
  { name: "read_file", description: "Read files" },
  { name: "edit_file", description: "Edit files" },
  { name: "bash", description: "Run commands" },
];
const firstCustomSelection = selectToolsOnFirstCustomUse(new Set(), subagentTools, false);
ok(firstCustomSelection.size === subagentTools.length, "first custom-mode use should select every available tool");
const savedCustomSelection = selectToolsOnFirstCustomUse(new Set(["read_file", "edit_file"]), subagentTools, true);
ok(savedCustomSelection.size === 2 && !savedCustomSelection.has("bash"), "saved custom tool selections should be preserved");
ok(selectToolsOnFirstCustomUse(new Set(), subagentTools, true).size === 0, "returning to custom mode should preserve a deliberate empty selection");

const subagentsSource = readFileSync(new URL("../components/SubagentsPanel.tsx", import.meta.url), "utf8");
const subagentsStyles = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
const customGroupIndex = subagentsSource.indexOf('aria-labelledby="subagents-custom-title"');
const builtinGroupIndex = subagentsSource.indexOf('aria-labelledby="subagents-builtin-title"');
ok(customGroupIndex >= 0 && builtinGroupIndex > customGroupIndex, "custom subagents should render before built-in subagents");
ok((subagentsSource.match(/className="subagents-profile-group"/g) ?? []).length === 2, "custom and built-in subagents should use separate sections");
ok(subagentsSource.includes('className="btn btn--small subagents-reset-override"'), "override status and reset should share one compact action");
ok(subagentsStyles.includes("repeat(2, minmax(200px, 1fr)) 152px"), "built-in subagent pickers should use equal columns and reserve one stable status column");
ok(subagentsSource.includes('className="settings-model-picker subagents-effort-picker"'), "effort and model overrides should share the same picker interaction pattern");
ok(subagentsSource.includes("<SubagentInvocation name={skill.name}"), "every subagent card should show its chat invocation affordance");
ok(subagentsSource.includes("onUseInChat(command)"), "subagent cards should send their slash command to the chat composer");

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
ok(!mcpServerRetryableFromAvailableList({ ...server("deferred"), startIntent: "automatic" }), "healthy on-demand server should not be included in retry all");
ok(mcpServerRetryableFromAvailableList({ ...server("deferred"), startIntent: "automatic", action: "retry" }), "explicit retry action should remain available for an idle server");
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
  const previous = input.value;
  const setter = Object.getOwnPropertyDescriptor((win?.HTMLInputElement ?? HTMLInputElement).prototype, "value")?.set;
  setter?.call(input, value);
  (input as HTMLInputElement & { _valueTracker?: { setValue: (next: string) => void } })._valueTracker?.setValue(previous);
  const eventCtor = win?.Event ?? Event;
  input.dispatchEvent(new eventCtor("input", { bubbles: true }));
  input.dispatchEvent(new eventCtor("change", { bubbles: true }));
}

ok(
  slashCommandKindTag({ name: "pwf:plan", description: "Plugin planning prompt.", kind: "custom", plugin: "pwf" }, t) === "plugin · pwf",
  "slash menu identifies the canonical plugin command source",
);
ok(
  slashCommandGroup({ name: "explore", description: "Explore in isolation.", kind: "subagent" }) === "subagents",
  "slash menu groups isolated skills as subagents",
);
ok(
  slashCommandGroup({ name: "plugins", description: "Manage plugins.", kind: "builtin", group: "management" }) === "management",
  "slash menu honors backend-provided command groups",
);
ok(
  slashCommandGroup({ name: "plugins", description: "Manage plugins.", kind: "builtin" }) === "management"
    && slashCommandGroup({ name: "new", description: "New session.", kind: "builtin" }) === "actions",
  "slash menu keeps a safe grouping fallback for older backends",
);
ok(
  sortSlashCommandsForMenu([
    { name: "plugins", description: "Manage plugins.", kind: "builtin", group: "management" },
    { name: "explore", description: "Explore in isolation.", kind: "subagent", group: "subagents" },
    { name: "new", description: "New session.", kind: "builtin", group: "actions" },
  ]).map((command) => command.name).join(",") === "new,explore,plugins",
  "slash menu keyboard order follows the visible group order",
);

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
      { name: "broken_read", description: "Broken tool.", readOnlyHint: true, schemaError: "invalid input schema: bad nested type" },
    ],
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
  await waitFor("github server row", () => Boolean(document.querySelector(".cap-mcp-list-row__name")?.textContent?.includes("github")));
  ok(document.body.textContent?.includes("1 unavailable"), "server list summary reports one quarantined tool");
  ok(!document.body.textContent?.includes("invalid input schema: bad nested type"), "server list keeps raw tool diagnostics out of the overview");

  const openServer = document.querySelector<HTMLButtonElement>(".cap-mcp-list-row__main");
  if (!openServer) throw new Error("missing MCP server details button");
  await act(async () => {
    openServer.click();
    await flush();
  });

  await waitFor("unavailable tool", () => Boolean(document.querySelector(".cap-tool-hint--error")?.textContent?.includes("Unavailable")));
  ok(document.body.textContent?.includes("invalid input schema: bad nested type"), "tool list shows the schema diagnostic");
  ok(document.body.textContent?.includes("issue_read") ?? false, "server details list read-only MCP tools normally");
  ok(document.body.textContent?.includes("issue_write") ?? false, "server details list write-capable MCP tools normally");
  ok(!findButton("Pre-trust read-only (1)"), "MCP details do not expose a bulk pre-trust action");
  ok(!findButton("Pre-trust"), "MCP details do not expose per-tool pre-trust actions");
  ok(!findButton("Untrust"), "MCP details do not expose an untrust action");
  ok(!document.querySelector(".cap-tool-trust"), "MCP details do not expose a separate trust state");
  ok(!document.body.textContent?.includes("read-only trust"), "MCP details do not describe the removed trust workflow");

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
  const meta: Meta = { label: "test", ready: true, eventChannel: "authorize-mcp-channel", cwd: "/tmp/reasonix-test", workspaceRoot: "/tmp/reasonix-test" };
  const tabs: TabMeta[] = [{
    id: "tab-authorize-mcp",
    scope: "project",
    workspaceRoot: "/tmp/reasonix-test",
    workspaceName: "reasonix-test",
    topicId: "topic-authorize-mcp",
    topicTitle: "Authorize MCP",
    label: "Authorize MCP",
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "auto",
    active: true,
    cwd: "/tmp/reasonix-test",
  }];
  let projectAuthorizationCount = 0;
  let servers: ServerView[] = [{
    name: "github",
    transport: "stdio",
    status: "failed",
    runtimeState: "issue",
    configured: true,
    autoStart: true,
    tools: 0,
    prompts: 0,
    resources: 0,
    error: "project-provided MCP server is blocked until the user authorizes it",
    requiresLaunchApproval: true,
    toolList: [
      { name: "issue_read", description: "Read issues.", readOnlyHint: true },
      { name: "issue_write", description: "Write issues." },
      { name: "wipe", description: "Delete data.", destructiveHint: true },
    ],
  }];
  window.go = {
    main: {
      App: {
        Meta: async () => meta,
        ListTabs: async () => tabs,
        MCPServers: async () => servers,
        AuthorizeAndConnectMCPServer: async () => {
          projectAuthorizationCount += 1;
          servers = servers.map((item) => ({
            ...item,
            status: "connected",
            runtimeState: "ready",
            requiresLaunchApproval: false,
            error: "",
          }));
        },
      } as Partial<AppBindings> as AppBindings,
    },
  };

  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(MCPServersSettingsPage)));
    await flush();
  });
  const refreshStatus = async () => {
    const refresh = document.querySelector<HTMLButtonElement>('button[aria-label="Refresh MCP status"]');
    if (!refresh) throw new Error("missing MCP status refresh action");
    await act(async () => {
      refresh.click();
      await flush();
    });
  };
  await waitFor("project MCP authorization action", () => Boolean(findButton("Authorize and connect")));
  ok(!findButton("Review changes"), "project MCP launch approval has no separate change-review workflow");
  ok(!findButton("Refresh catalog"), "catalog maintenance is not part of the normal MCP workflow");
  ok(!document.querySelector('[role="dialog"]'), "project authorization does not open a second modal");
  await act(async () => {
    findButton("Authorize and connect")?.click();
    await flush();
  });
  await waitFor("durable project launch authorization", () => projectAuthorizationCount === 1 && Boolean(document.querySelector('[data-status="connected"]')));
  ok(!document.querySelector('[role="dialog"]'), "project confirmation connects directly without a second modal");

  servers = servers.map((item) => ({
    ...item,
    status: "failed",
    runtimeState: "issue",
    error: "authentication required",
    authStatus: "required",
    authUrl: "https://mcp.example.test/authorize",
  }));
  await refreshStatus();
  await waitFor("sign-in action", () => Boolean(findButton("Sign in")));
  ok(!findButton("Review changes"), "OAuth failure does not expose a removed change-review action");

  servers = servers.map((item) => ({
    ...item,
    status: "failed",
    runtimeState: "issue",
    error: "connection refused",
    authStatus: "none",
    authUrl: "",
  }));
  await refreshStatus();
  await waitFor("ordinary retry action", () => Boolean(findButton("Retry")));
  ok(!findButton("Review changes"), "ordinary startup failures keep only the retry action");

  servers = servers.map((item) => ({
    ...item,
    status: "connected",
    runtimeState: "ready",
    error: "",
    requiresLaunchApproval: false,
  }));
  await refreshStatus();
  await waitFor("authorized project server row", () => Boolean(document.querySelector('[data-status="connected"]')));
  await act(async () => {
    (document.querySelector(".cap-mcp-list-row__main") as HTMLButtonElement | null)?.click();
    await flush();
  });
  await waitFor("connected project server detail", () => Boolean(document.querySelector(".cap-mcp-subpage")));
  ok(!findButton("Review changes"), "an authorized connected project server does not show a change alarm");
  ok(!findButton("Revoke trust"), "normal MCP details do not expose a second authorization-management workflow");

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
  await waitFor("plugin-managed MCP row", () => Boolean(document.querySelector(".cap-mcp-list-row__name")?.textContent?.includes("helper")));
  ok(document.body.textContent?.includes("Managed by plugin superpowers") ?? false, "plugin-managed MCP identifies its owner");

  const openServer = document.querySelector<HTMLButtonElement>(".cap-mcp-list-row__main");
  if (!openServer) throw new Error("missing plugin-managed MCP details button");
  await act(async () => {
    openServer.click();
    await flush();
  });
  ok(!findButton("Remove server"), "plugin-managed MCP hides the misleading remove action");
  ok(!findButton("Edit config"), "plugin-managed MCP hides direct config editing");
  ok(!findButton("Clear auth"), "plugin-managed MCP hides auth persistence actions");
  ok(!findButton("Pre-trust read-only (1)"), "plugin-managed MCP has no bulk pre-trust action");

  ok(!findButton("View tools"), "standalone server details show tools without another disclosure step");
  ok(document.body.textContent?.includes("echo") ?? false, "plugin-managed MCP details show its tools");
  ok(!findButton("Pre-trust"), "plugin-managed MCP has no per-tool pre-trust action");

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
  await waitFor("runtime-only failure row", () => Boolean(document.querySelector(".cap-mcp-list-row__name")?.textContent?.includes("runtime-only")));
  const showDetails = document.querySelector<HTMLButtonElement>(".cap-mcp-list-row__main");
  if (!showDetails) throw new Error("missing runtime-only failure details button");
  await act(async () => {
    showDetails.click();
    await flush();
  });
  ok(document.body.textContent?.includes("command not found") ?? false, "runtime-only MCP detail preserves its failure diagnostic");
  ok(!findButton("Remove server"), "runtime-only MCP failure hides an action the backend cannot persist");

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
  const meta: Meta = { label: "test", ready: true, eventChannel: "mcp-editor-channel", cwd: "/tmp/reasonix-test", workspaceRoot: "/tmp/reasonix-test" };
  const tabs: TabMeta[] = [{
    id: "tab-mcp-editor",
    scope: "project",
    workspaceRoot: "/tmp/reasonix-test",
    workspaceName: "reasonix-test",
    topicId: "topic-mcp-editor",
    topicTitle: "MCP editor",
    label: "MCP editor",
    ready: true,
    running: false,
    mode: "normal",
    toolApprovalMode: "auto",
    active: true,
    cwd: "/tmp/reasonix-test",
  }];
  let addedInput: MCPServerInput | undefined;
  let servers: ServerView[] = [
    {
      name: "github",
      transport: "stdio",
      status: "connected",
      configured: true,
      autoStart: true,
      command: "github-mcp-server",
      tools: 1,
      prompts: 0,
      resources: 0,
      toolList: [{ name: "issue_read", description: "Read GitHub issues" }],
    },
    {
      name: "yakit",
      transport: "stdio",
      status: "connected",
      configured: true,
      autoStart: true,
      command: "yakit-mcp",
      tools: 1,
      prompts: 0,
      resources: 0,
      toolList: [{ name: "generate_yso_bytes", description: "Generate bytes" }],
    },
  ];
  window.go = {
    main: {
      App: {
        Meta: async () => meta,
        ListTabs: async () => tabs,
        MCPServers: async () => servers,
        AddMCPServer: async (input: MCPServerInput) => {
          addedInput = input;
          servers = [...servers, {
            name: input.name,
            transport: input.transport,
            status: "connected",
            configured: true,
            autoStart: true,
            command: input.command,
            args: input.args,
            url: input.url,
            tools: 0,
            prompts: 0,
            resources: 0,
          }];
          return 0;
        },
        InstallMCPServer: async (input: MCPServerInput) => {
          addedInput = input;
          servers = [...servers, {
            name: input.name,
            transport: input.transport,
            status: "connected",
            configured: true,
            autoStart: true,
            command: input.command,
            args: input.args,
            url: input.url,
            tools: 0,
            prompts: 0,
            resources: 0,
          }];
          return { name: input.name, state: "ready", toolCount: 0, action: "none", message: "ready" };
        },
      } as Partial<AppBindings> as AppBindings,
    },
  };

  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(MCPServersSettingsPage)));
    await flush();
  });
  await waitFor("MCP editor server rows", () => document.querySelectorAll(".cap-mcp-list-row__name").length === 2);
  const search = document.querySelector<HTMLInputElement>('.cap-mcp-search input[type="search"]');
  if (!search) throw new Error("missing MCP server search");
  await act(async () => {
    setInputValue(search, "generate_yso_bytes");
    await flush();
  });
  ok(search.value === "generate_yso_bytes", "MCP search accepts the entered query");
  await waitFor("filtered Yakit row", () => document.querySelectorAll(".cap-mcp-list-row__name").length === 1);
  ok(document.querySelector(".cap-mcp-list-row__name")?.textContent === "yakit", "server search includes MCP tool names");

  const addServer = findButton("Add server");
  if (!addServer) throw new Error("missing Add server button");
  await act(async () => {
    addServer.click();
    await flush();
  });
  const quickInstall = findButton("Quick install");
  const manualSetup = findButton("Manual setup");
  ok(quickInstall?.getAttribute("aria-selected") === "true" && Boolean(manualSetup) && Boolean(findButton("JSON")), "new server install defaults to quick install while keeping manual and JSON configuration in the same editor");
  const definitionEditor = document.querySelector<HTMLTextAreaElement>(".cap-mcp-quick__input");
  if (!definitionEditor) throw new Error("missing quick MCP install input");
  ok(definitionEditor.placeholder.includes("chrome-devtools-mcp@latest"), "the default install path asks only for a command, URL, or JSON definition");
  await act(async () => {
    manualSetup?.click();
    await flush();
  });
  ok(Boolean(document.querySelector(".cap-mcp-field--name input")) && Boolean(findButton("Advanced options")), "manual setup restores name, transport, and advanced configuration without leaving the install page");
  ok(!addedInput, "opening the quick installer does not mutate MCP state");

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
            actions: [{
              kind: "plugin", action: "install_plugin_package", name: "superpowers", source, status: "planned",
              compatibility: "partial", mappedCapabilities: ["skills", "agents"],
              skippedCapabilities: [{ capability: "hook", path: "hooks/hooks.json", reason: "unsupported event" }],
            }],
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
            commands: 2,
            agents: 1,
            hooks: 1,
            mcpServers: 1,
            compatibility: "full",
            mappedCapabilities: ["skills", "agents", "hooks", "mcp"],
            skillDetails: [{ name: "plan", description: "Plan work before implementation.", invocation: "/superpowers:plan", runAs: "inline" }],
            agentDetails: [{ name: "reviewer", description: "Review changes.", invocation: "/superpowers:reviewer", model: "sonnet" }],
            commandDetails: [{
              name: "plan",
              description: "Plugin planning prompt.",
              invocation: "/superpowers:plan",
            }, {
              name: "blocked",
              description: "Occupied canonical command.",
              invocation: "/superpowers:blocked",
              shadowed: true,
            }],
            hookDetails: [{ event: "SessionStart", contextFile: "CLAUDE.md", description: "Load startup context." }],
            mcpServerDetails: [{ name: "context", displayName: "Context Search", transport: "stdio", command: "node server.js", autoStart: false }],
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
  ok(document.body.textContent?.includes("Partially compatible") ?? false, "preview renders compatibility status");
  ok(document.body.textContent?.includes("Mapped: skills, agents") ?? false, "preview renders mapped capabilities");
  ok(document.body.textContent?.includes("hook: unsupported event") ?? false, "preview renders skipped capability reasons");

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
  ok(document.body.textContent?.includes("/superpowers:plan") ?? false, "expanded plugin details list qualified skill invocations");
  ok(document.body.textContent?.includes("/superpowers:plan") ?? false, "plugin details show the canonical qualified invocation");
  ok(document.body.textContent?.includes("qualified name is occupied by a user or project command") ?? false, "occupied canonical command explains the winning source");
  ok(document.body.textContent?.includes("SessionStart") ?? false, "expanded plugin details list exported hooks");
  ok(document.body.textContent?.includes("Fully compatible") ?? false, "plugin details show structured compatibility");
  ok(document.body.textContent?.includes("/superpowers:reviewer") ?? false, "plugin details list imported agents");
  ok(document.body.textContent?.includes("Context Search") ?? false, "plugin details retain MCP display names");
  ok(document.body.textContent?.includes("on demand") ?? false, "imported MCP servers are labeled on demand");
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
