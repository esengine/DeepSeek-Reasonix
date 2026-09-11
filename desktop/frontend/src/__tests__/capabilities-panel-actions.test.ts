import { findButton, flush, installDom, setInputValue, waitFor } from "./capabilities-test-helpers";
import { readFileSync } from "node:fs";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { MCPServersSettingsPage, failureKind, mcpServerDraftJSON, parseMCPQuickDefinition, parseMCPServerJSON, summarizeServerError, withExplicitMCPClears } from "../components/CapabilitiesPanel";
import { slashCommandGroup, slashCommandKindTag, sortSlashCommandsForMenu } from "../components/SlashMenu";
import { selectToolsOnFirstCustomUse } from "../components/SubagentsPanel";
import type { AppBindings } from "../lib/bridge";
import { LocaleProvider, t } from "../lib/i18n";
import { mcpServerLifecycleActions, mcpServerRetryableFromAvailableList } from "../lib/mcpServerLifecycle";
import type { MCPServerInput, Meta, ServerView, TabMeta } from "../lib/types";
import { installDesktopHostStub } from "./desktopHostStub";

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
  const appStubTable: AppBindings = ({
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
          const toolCount: number = await appStubTable.AddMCPServer(input);
          return { name: input.name, state: "ready", toolCount, action: "none", message: "ready" };
        },
      } as Partial<AppBindings> as AppBindings,
    },
  }).main.App;
  installDesktopHostStub(appStubTable);

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
ok(subagentsStyles.includes("repeat(2, minmax(0, 1fr)) 152px"), "built-in subagent pickers should use equal shrinkable columns and reserve one stable status column");
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
  installDesktopHostStub(({
    main: {
      App: {
        Meta: async () => meta,
        ListTabs: async () => tabs,
        MCPServers: async () => servers,
      } as Partial<AppBindings> as AppBindings,
    },
  }).main.App);

  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(MCPServersSettingsPage)));
    await flush();
  });
  await waitFor("github server row", () => Boolean(document.querySelector(".cap-mcp-list-row__name")?.textContent?.includes("github")));
  ok(Boolean(findButton("Remove server")), "configured MCP exposes a removal action directly in the server list");
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
  let servers: ServerView[] = [{
    name: "github",
    transport: "stdio",
    status: "connected",
    runtimeState: "ready",
    configured: true,
    source: "project",
    configSource: "reasonix.toml",
    autoStart: true,
    tools: 3,
    prompts: 0,
    resources: 0,
    toolList: [
      { name: "issue_read", description: "Read issues.", readOnlyHint: true },
      { name: "issue_write", description: "Write issues." },
      { name: "wipe", description: "Delete data.", destructiveHint: true },
    ],
  }, {
    name: "linear",
    transport: "http",
    status: "connected",
    runtimeState: "ready",
    configured: true,
    source: "user",
    autoStart: true,
    tools: 1,
    prompts: 0,
    resources: 0,
    toolList: [{ name: "get_issue", description: "Read an issue.", readOnlyHint: true }],
  }];
  installDesktopHostStub(({
    main: {
      App: {
        Meta: async () => meta,
        ListTabs: async () => tabs,
        MCPServers: async () => servers,
      } as Partial<AppBindings> as AppBindings,
    },
  }).main.App);

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
  await waitFor("trusted project MCP", () => Boolean(document.querySelector('[data-status="connected"]')));
  ok(document.body.textContent?.includes("This project"), "project MCP is grouped under This project");
  ok(document.body.textContent?.includes("Global MCP"), "user-installed MCP is grouped by its global scope");
  ok(document.body.textContent?.includes("Install once and use automatically in every Reasonix project."), "global MCP explains its cross-project availability");
  ok(document.body.textContent?.includes("Project"), "project MCP row shows a project source badge");
  ok(document.body.textContent?.includes("Declared by this project and available automatically."), "project MCP explains zero-confirmation availability");
  ok(!findButton("Install and use"), "trusted project MCP has no install confirmation");
  ok(!findButton("Authorize and connect"), "trusted project MCP has no authorization action");
  ok(!findButton("Review changes"), "project MCP has no separate change-review workflow");
  ok(!findButton("Refresh catalog"), "catalog maintenance is not part of the normal MCP workflow");
  ok(!document.querySelector('[role="dialog"]'), "project MCP does not open a confirmation modal");

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
  await waitFor("trusted project server row", () => Boolean(document.querySelector('[data-status="connected"]')));
  await act(async () => {
    (document.querySelector(".cap-mcp-list-row__main") as HTMLButtonElement | null)?.click();
    await flush();
  });
  await waitFor("connected project server detail", () => Boolean(document.querySelector(".cap-mcp-subpage")));
  ok(document.body.textContent?.includes("Current project · reasonix.toml"), "project MCP details show their configuration source");
  ok(!findButton("Review changes"), "a trusted connected project server does not show a change alarm");
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
  installDesktopHostStub(({
    main: {
      App: {
        Meta: async () => meta,
        ListTabs: async () => tabs,
        MCPServers: async () => servers,
      } as Partial<AppBindings> as AppBindings,
    },
  }).main.App);

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
  installDesktopHostStub(({
    main: {
      App: {
        Meta: async () => meta,
        ListTabs: async () => tabs,
        MCPServers: async () => servers,
      } as Partial<AppBindings> as AppBindings,
    },
  }).main.App);

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
  installDesktopHostStub(({
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
  }).main.App);

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
