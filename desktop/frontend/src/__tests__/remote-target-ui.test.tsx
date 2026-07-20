// Run: tsx src/__tests__/remote-target-ui.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";

import { RemoteSettingsPage } from "../components/RemoteSettingsPage";
import { RemoteTargetSurfaces } from "../components/RemoteTargetSurfaces";
import { RemoteTargetToolbarButton } from "../components/RemoteTargetToolbarButton";
import { RemoteWorkspaceSetup } from "../components/RemoteWorkspaceSetup";
import type { AppBindings } from "../lib/bridge";
import { LocaleProvider } from "../lib/i18n";
import type {
  RemoteAskPassView,
  RemoteConnectionLogView,
  RemoteCreateWorkspaceSessionInput,
  RemoteDirectoryView,
  RemoteHostInput,
  RemoteHostRuntimeSummaryView,
  RemoteHostView,
  RemoteTargetStatusView,
  RemoteWorkbenchStatusView,
  RemoteWorkspaceBrowseInput,
  RemoteWorkspacePageView,
} from "../lib/types";
import { workspaceCreationRoute } from "../lib/workspaceTargetRouting";

let passed = 0;

function ok(value: unknown, label: string): asserts value {
  if (!value) throw new Error(`FAIL: ${label}`);
  passed += 1;
  process.stdout.write(`  PASS  ${label}\n`);
}

function flush(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function waitFor(label: string, predicate: () => boolean) {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    await act(async () => {
      await flush();
    });
    if (predicate()) return;
  }
  throw new Error(`timed out waiting for ${label}: ${document.body.textContent?.slice(0, 500) ?? ""}`);
}

type EventHandler = (...payload: unknown[]) => void;

function installDOM() {
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
  globalThis.HTMLInputElement = dom.window.HTMLInputElement;
  globalThis.HTMLButtonElement = dom.window.HTMLButtonElement;
  globalThis.Event = dom.window.Event;
  globalThis.InputEvent = dom.window.InputEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
  globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
  // React's legacy input-event capability probe is fixed at module load; when
  // jsdom windows are replaced between cases it can enter the IE polyfill path.
  // Provide the no-op hooks that path expects so autofocus remains testable.
  (dom.window.HTMLElement.prototype as HTMLElement & { attachEvent?: () => void }).attachEvent = () => {};
  (dom.window.HTMLElement.prototype as HTMLElement & { detachEvent?: () => void }).detachEvent = () => {};
  const handlers = new Map<string, Set<EventHandler>>();
  window.runtime = {
    EventsOn(name: string, callback: EventHandler) {
      const set = handlers.get(name) ?? new Set<EventHandler>();
      set.add(callback);
      handlers.set(name, set);
      return () => set.delete(callback);
    },
    BrowserOpenURL() {},
  };
  return {
    rootElement: document.getElementById("root")!,
    emit(name: string, payload: unknown) {
      handlers.get(name)?.forEach((handler) => handler(payload));
    },
    close: () => dom.window.close(),
  };
}

function render(component: React.ReactElement): { root: Root; rootElement: HTMLElement } {
  const rootElement = document.getElementById("root");
  if (!rootElement) throw new Error("missing root");
  const root = createRoot(rootElement);
  act(() => {
    root.render(React.createElement(LocaleProvider, null, component));
  });
  return { root, rootElement };
}

function button(root: HTMLElement, label: string): HTMLButtonElement {
  const found = Array.from(root.querySelectorAll("button")).find((candidate) => candidate.textContent?.trim() === label);
  if (!found) throw new Error(`button not found: ${label}; saw ${Array.from(root.querySelectorAll("button")).map((item) => item.textContent?.trim()).join(" | ")}`);
  return found as HTMLButtonElement;
}

function directoryButton(root: HTMLElement, name: string): HTMLButtonElement {
  const found = Array.from(root.querySelectorAll<HTMLButtonElement>(".remote-workspace-directory-list button"))
    .find((candidate) => candidate.querySelector("strong")?.textContent?.trim() === name);
  if (!found) throw new Error(`directory button not found: ${name}`);
  return found;
}

function inputValue(input: HTMLInputElement, value: string) {
  const win = input.ownerDocument.defaultView;
  const previous = input.value;
  const setter = Object.getOwnPropertyDescriptor((win?.HTMLInputElement ?? HTMLInputElement).prototype, "value")?.set;
  if (!setter) throw new Error("missing input value setter");
  act(() => {
    setter.call(input, value);
    (input as HTMLInputElement & { _valueTracker?: { setValue(next: string): void } })._valueTracker?.setValue(previous);
    const eventCtor = win?.Event ?? Event;
    input.dispatchEvent(new eventCtor("input", { bubbles: true }));
    input.dispatchEvent(new eventCtor("change", { bubbles: true }));
  });
}

ok(workspaceCreationRoute({ state: "LocalConnected", canReconnect: false }) === "local-picker", "project creation routes a connected Local target to the Desktop picker");
ok(workspaceCreationRoute({ state: "RemoteConnected", hostId: "host-route", canReconnect: false }) === "remote-setup", "project creation routes a connected Remote target to Host workspace setup");
ok(workspaceCreationRoute({ state: "RemoteReconnecting", hostId: "host-route", canReconnect: true }) === "blocked", "project creation never falls back to a local picker while Remote recovery is pending");

console.log("\nRemote target UI");

// The topic-bar entry is always available for connection management and folds
// the stable Remote identity into the existing compact action row.
{
  const dom = installDOM();
  let opens = 0;
  const localTarget: RemoteTargetStatusView = { state: "LocalConnected", canReconnect: false };
  const connectedTarget: RemoteTargetStatusView = {
    state: "RemoteConnected",
    hostId: "host-toolbar",
    hostLabel: "Linux Host",
    canReconnect: false,
  };
  const renderButton = (status: RemoteTargetStatusView) => React.createElement(RemoteTargetToolbarButton, {
    status,
    onOpen: () => { opens += 1; },
  });
  const { root, rootElement } = render(renderButton(localTarget));
  const toolbarButton = () => rootElement.querySelector<HTMLButtonElement>(".remote-target-toolbar-btn");

  ok(toolbarButton()?.getAttribute("aria-label") === "Connect to Remote", "Local target exposes a direct Remote connection entry");
  ok(toolbarButton()?.dataset.remoteState === "LocalConnected", "Remote toolbar entry exposes the current target state to compact chrome");
  act(() => toolbarButton()?.click());
  ok(opens === 1, "Remote toolbar entry invokes its connection-management action");

  act(() => root.render(React.createElement(LocaleProvider, null, renderButton(connectedTarget))));
  ok(toolbarButton()?.classList.contains("remote-target-toolbar-btn--connected") === true, "healthy Remote target renders a compact connected state");
  ok(toolbarButton()?.getAttribute("aria-label") === "Remote connected · Linux Host", "connected toolbar state exposes both target state and Host identity");

  act(() => root.render(React.createElement(LocaleProvider, null, renderButton({ ...connectedTarget, state: "RemoteReconnecting", canReconnect: true }))));
  ok(toolbarButton()?.classList.contains("remote-target-toolbar-btn--transition") === true && toolbarButton()?.querySelector(".spin") !== null, "Remote transition uses the in-row progress state");

  act(() => root.render(React.createElement(LocaleProvider, null, renderButton({ ...connectedTarget, state: "Disconnected", failure: "ssh exited", canReconnect: true }))));
  ok(toolbarButton()?.classList.contains("remote-target-toolbar-btn--attention") === true, "Remote failure remains visible as an actionable in-row state");

  await act(async () => root.unmount());
  dom.close();
}

// Host CRUD, backend errors, connection state, reconnect, and destructive
// switch confirmation all exercise the actual bridge-facing component contract.
{
  const dom = installDOM();
  let hosts: RemoteHostView[] = [{ id: "host-1", mode: "config", alias: "devbox", label: "Dev box" }];
  let status: RemoteTargetStatusView = { state: "LocalConnected", canReconnect: false };
  const savedInputs: RemoteHostInput[] = [];
  const deleted: string[] = [];
  const connected: string[] = [];
  const switched: boolean[] = [];
  let reconnects = 0;
  let saveFailure = "";
  let logReads = 0;
  let hostSummaryReads = 0;
  const hostSummary: RemoteHostRuntimeSummaryView = {
    capabilities: {
      hostConfig: true, workspaceBrowse: true, sessionCreate: true, sessionAttach: true,
      composerSubmit: true, turnSteer: true, turnCancel: true, promptApprove: true, promptAnswer: true,
      features: {
        coreSession: true, primaryFileQueries: true, userShell: true, jobCancel: true, memory: true, research: false,
        mediaPreview: false, attachments: false, clipboardImages: false, sftp: false, localPathOperations: false,
        gitWrite: false, pty: false, deliveryWorktree: false,
      },
      limits: { historyMaxTurns: 200 },
    },
    config: {
      available: true, models: [], collaborationModes: [], tokenModes: [], toolApprovalModes: [], revision: "config-opaque",
      effectiveScopes: [{ name: "workspace", active: true }],
      displayPaths: [{ scope: "workspace", displayPath: "/srv/project/.reasonix/config.toml" }],
      featureStates: [{ feature: "memory", available: true, summary: "Host memory enabled" }],
      cliHints: [{ label: "Inspect Host", command: "reasonix remote doctor" }],
    },
    catalog: {
      available: true,
      mcpServers: [
        { name: "browser", status: "available", toolCount: 3 },
        { name: "offline", status: "unavailable", toolCount: 0 },
      ],
      skills: [{ name: "review", description: "Review changes", scope: "workspace" }],
      plugins: [{ name: "quality", enabled: true }],
    },
  };
  const logs: RemoteConnectionLogView[] = [
    { atMillis: 1_700_000_000_000, state: "LocalConnected", message: "Desktop Local target ready" },
  ];

  window.go = {
    main: {
      App: {
        RemoteHosts: async () => hosts.map((host) => ({ ...host })),
        RemoteTargetStatus: async () => ({ ...status }),
        RemoteConnectionLogs: async () => {
          logReads += 1;
          return logs.map((entry) => ({ ...entry }));
        },
        RemoteHostRuntimeSummary: async () => {
          hostSummaryReads += 1;
          return {
            ...hostSummary,
            catalog: {
              ...hostSummary.catalog,
              mcpServers: [...hostSummary.catalog.mcpServers],
              skills: [...hostSummary.catalog.skills],
              plugins: [...hostSummary.catalog.plugins],
            },
          };
        },
        SaveRemoteHost: async (input: RemoteHostInput) => {
          savedInputs.push({ ...input });
          if (saveFailure) throw new Error(saveFailure);
          const savedLabel = input.label.trim() || (input.mode === "direct" ? input.destination : input.alias) || "";
          const saved: RemoteHostView = input.mode === "direct" ? {
            id: input.id ?? "host-2",
            mode: input.mode,
            destination: input.destination,
            port: input.port,
            label: savedLabel,
          } : {
            id: input.id ?? "host-2",
            mode: input.mode,
            alias: input.alias,
            label: savedLabel,
            sshConfigPath: input.sshConfigPath,
          };
          hosts = input.id ? hosts.map((host) => host.id === input.id ? saved : host) : [...hosts, saved];
          return saved;
        },
        DeleteRemoteHost: async (id: string) => {
          deleted.push(id);
          hosts = hosts.filter((host) => host.id !== id);
        },
        ConnectRemoteHost: async (id: string) => {
          connected.push(id);
          const host = hosts.find((candidate) => candidate.id === id)!;
          status = { state: "RemoteConnected", hostId: id, hostLabel: host.label, canReconnect: false };
          logs.push({ atMillis: 1_700_000_000_100, state: status.state, hostId: id, hostLabel: host.label, message: "Protocol initialized" });
        },
        ReconnectRemoteTarget: async () => {
          reconnects += 1;
          status = { state: "RemoteConnected", hostId: "host-1", hostLabel: "Dev box", canReconnect: false };
        },
        SwitchToLocalTarget: async (confirmed: boolean) => {
          switched.push(confirmed);
          status = { state: "LocalConnected", canReconnect: false };
        },
      } as Partial<AppBindings> as AppBindings,
    },
  };

  const { root, rootElement } = render(React.createElement(RemoteSettingsPage));
  await waitFor("host list", () => rootElement.textContent?.includes("Dev box") === true);
  ok(rootElement.textContent?.includes("Local connected"), "current Local target state renders");
  await waitFor("structured connection log", () => rootElement.textContent?.includes("Desktop Local target ready") === true);
  ok(rootElement.textContent?.includes("Connection lifecycle"), "Remote settings exposes the safe structured lifecycle log");
  const readsBeforeRefresh = logReads;
  await act(async () => {
    button(rootElement, "Refresh logs").click();
    await flush();
  });
  ok(logReads === readsBeforeRefresh + 1, "connection lifecycle log can be refreshed explicitly");

  await act(async () => {
    button(rootElement, "Edit").click();
    await flush();
  });
  ok(button(rootElement, "SSH config (advanced)").getAttribute("aria-pressed") === "true", "existing alias entry opens in advanced SSH config mode");
  const legacyAlias = rootElement.querySelector<HTMLInputElement>('input[name="remote-host-alias"]');
  const legacyConfig = rootElement.querySelector<HTMLInputElement>('input[name="remote-host-ssh-config"]');
  ok(legacyAlias?.value === "devbox" && legacyConfig !== null, "advanced editor preserves the saved alias and optional config field");
  inputValue(legacyConfig!, "/tmp/ssh/config");
  await act(async () => {
    button(rootElement, "Save").click();
    await flush();
  });
  ok(savedInputs[0]?.mode === "config" && savedInputs[0]?.alias === "devbox" && savedInputs[0]?.sshConfigPath === "/tmp/ssh/config", "advanced Host edit sends config mode without direct fields");

  await act(async () => {
    button(rootElement, "Add Host").click();
    await flush();
  });
  ok(button(rootElement, "Direct connection").getAttribute("aria-pressed") === "true", "new Host defaults to direct connection without requiring SSH config");
  const destination = rootElement.querySelector<HTMLInputElement>('input[name="remote-host-destination"]');
  const port = rootElement.querySelector<HTMLInputElement>('input[name="remote-host-port"]');
  const hostLabel = rootElement.querySelector<HTMLInputElement>('input[name="remote-host-label"]');
  ok(destination !== null && port?.value === "22" && hostLabel?.value === "", "direct editor exposes username@host, default port 22, and an optional display label");
  inputValue(destination!, "developer@10.0.0.8");
  ok(destination?.value === "developer@10.0.0.8" && port?.value === "22" && hostLabel?.value === "", "direct Host requires only one user-entered username@host value");
  ok(!button(rootElement, "Save").disabled, "Host Save enables with destination only while port stays at its default");
  await act(async () => {
    button(rootElement, "Save").click();
    await flush();
  });
  await waitFor("saved Host", () => rootElement.textContent?.includes("developer@10.0.0.8") === true);
  ok(savedInputs[1]?.mode === "direct" && savedInputs[1]?.destination === "developer@10.0.0.8" && savedInputs[1]?.port === 22 && savedInputs[1]?.label === "" && !savedInputs[1]?.alias && !savedInputs[1]?.sshConfigPath, "direct Host create sends destination and default numeric port with no required label or config fields");
  ok(hosts.find((host) => host.id === "host-2")?.label === "developer@10.0.0.8", "backend default display label is accepted and rendered for a destination-only Host");

  await act(async () => {
    button(rootElement, "Edit").click();
    await flush();
  });
  const editLabel = rootElement.querySelector<HTMLInputElement>('input[name="remote-host-label"]');
  ok(button(rootElement, "Direct connection").getAttribute("aria-pressed") === "true" && rootElement.querySelector<HTMLInputElement>('input[name="remote-host-destination"]')?.value === "developer@10.0.0.8", "editing a direct Host restores its mode and destination");
  inputValue(editLabel!, "GPU Host renamed");
  await act(async () => {
    button(rootElement, "Save").click();
    await flush();
  });
  await waitFor("edited Host", () => rootElement.textContent?.includes("GPU Host renamed") === true);
  ok(savedInputs[2]?.id === "host-2" && savedInputs[2]?.label === "GPU Host renamed", "Host edit preserves the stable Host id");

  await act(async () => {
    button(rootElement, "Delete").click();
    await flush();
  });
  ok(deleted.length === 0, "Host deletion waits for explicit confirmation");
  await act(async () => {
    button(rootElement, "Confirm").click();
    await flush();
  });
  ok(deleted[0] === "host-2", "confirmed deletion removes the selected disconnected Host");

  saveFailure = "alias rejected by backend";
  inputValue(rootElement.querySelector<HTMLInputElement>('input[name="remote-host-destination"]')!, "developer@bad-host");
  inputValue(rootElement.querySelector<HTMLInputElement>('input[name="remote-host-label"]')!, "Bad Host");
  await act(async () => {
    button(rootElement, "Save").click();
    await flush();
  });
  await waitFor("save error", () => rootElement.textContent?.includes(saveFailure) === true);
  ok(rootElement.querySelector('[role="alert"]')?.textContent?.includes(saveFailure), "backend CRUD errors remain visible instead of being swallowed");

  await act(async () => {
    button(rootElement, "Connect").click();
    await flush();
  });
  await waitFor("connected target", () => rootElement.textContent?.includes("Remote connected") === true);
  ok(connected[0] === "host-1", "Connect uses the stable selected Host id");
  await waitFor("connected lifecycle log", () => rootElement.textContent?.includes("Protocol initialized") === true);
  ok(rootElement.textContent?.includes("Dev box"), "connection log renders the structured Host label and safe message");
  await waitFor("Host runtime summary", () => rootElement.textContent?.includes("Remote Host runtime") === true && hostSummaryReads > 0);
  ok(rootElement.textContent?.includes("Host memory enabled") && rootElement.textContent?.includes("Attachments and media bodies"), "Remote Settings renders Host capabilities, config summary and deferred V1 state");
  const sessionCatalog = rootElement.querySelector<HTMLElement>('[data-testid="remote-session-catalog"]');
  ok(sessionCatalog?.textContent?.includes("browser") && sessionCatalog.textContent.includes("3 tools"), "Remote Settings renders MCP name, status and tool count from the current Session catalog");
  ok(sessionCatalog?.textContent?.includes("review") && sessionCatalog.textContent.includes("Review changes") && sessionCatalog.textContent.includes("workspace"), "Remote Settings renders only the safe Skill summary fields");
  ok(sessionCatalog?.textContent?.includes("quality") && sessionCatalog.textContent.includes("Enabled"), "Remote Settings renders only the safe Plugin name and enabled state");
  ok(sessionCatalog?.querySelectorAll("button, input, select, textarea").length === 0, "current Session catalog is read-only and exposes no mutation controls");
  ok(!rootElement.textContent?.includes("DEEPSEEK_API_KEY"), "Remote Settings never renders a Desktop-local provider secret as Host state");

  hostSummary.catalog = { available: false, mcpServers: [], skills: [], plugins: [] };
  await act(async () => {
    button(rootElement, "Refresh Host").click();
    await flush();
  });
  await waitFor("generic unavailable catalog", () => sessionCatalog?.textContent?.includes("Attach a Remote session to view its catalog.") === true);
  ok(!sessionCatalog?.textContent?.includes("ssh") && !sessionCatalog?.textContent?.includes("transport"), "catalog unavailability stays generic and does not render raw Host errors");
  await act(async () => {
    button(rootElement, "Switch to Local").click();
    await flush();
  });
  ok(switched.length === 0, "switching from Remote requires an explicit confirmation step");
  await act(async () => {
    button(rootElement, "Confirm").click();
    await flush();
  });
  ok(switched.length === 1 && switched[0] === true, "confirmed Remote detach sends confirmed=true exactly once");

  status = { state: "Disconnected", hostId: "host-1", hostLabel: "Dev box", failure: "ssh exited", canReconnect: true };
  act(() => dom.emit("remote:target-state", status));
  await waitFor("failure state", () => rootElement.textContent?.includes("ssh exited") === true);
  await act(async () => {
    button(rootElement, "Reconnect").click();
    await flush();
  });
  ok(reconnects === 1, "recoverable connection state invokes reconnect");

  await act(async () => root.unmount());
  dom.close();
}

// AskPass stays entirely in memory: secret values live only in a password input
// until submit, then the modal is removed before the IPC promise resolves.
{
  const dom = installDOM();
  const responses: Array<{ requestId: string; value: string; cancelled: boolean }> = [];
  window.go = {
    main: {
      App: {
        RemoteTargetStatus: async () => ({ state: "LocalConnected", canReconnect: false }),
        ReconnectRemoteTarget: async () => {},
        SwitchToLocalTarget: async () => {},
        RespondRemoteAskPass: async (requestId: string, value: string, cancelled: boolean) => {
          responses.push({ requestId, value, cancelled });
          await flush();
        },
      } as Partial<AppBindings> as AppBindings,
    },
  };
  const { root, rootElement } = render(React.createElement(RemoteTargetSurfaces));
  await waitFor("target status probe", () => true);

  const secret = "never-render-this-after-submit";
  const passwordPrompt: RemoteAskPassView = {
    requestId: "ask-1",
    kind: "password",
    prompt: "user@host's password:",
    hostLabel: "Dev box",
    secret: true,
  };
  act(() => dom.emit("remote:askpass", passwordPrompt));
  await waitFor("AskPass modal", () => rootElement.querySelector('[role="dialog"]') !== null);
  const password = rootElement.querySelector<HTMLInputElement>('input[name="remote-askpass-response"]');
  ok(password?.type === "password", "secret AskPass response uses a password input");
  inputValue(password!, secret);
  await act(async () => {
    button(rootElement, "Continue").click();
    await flush();
  });
  ok(responses.length === 1 && responses[0].value === secret && !responses[0].cancelled, "AskPass submits the response once through the bound call");
  ok(rootElement.querySelector('[role="dialog"]') === null, "AskPass modal is removed immediately after submit");
  ok(!rootElement.textContent?.includes(secret) && !rootElement.innerHTML.includes(secret), "submitted secret is not rendered or retained in DOM markup");

  const cancelPrompt: RemoteAskPassView = {
    requestId: "ask-2",
    kind: "key_passphrase",
    prompt: "Enter passphrase for key '/home/user/.ssh/id_ed25519':",
    secret: true,
  };
  act(() => dom.emit("remote:askpass", cancelPrompt));
  await waitFor("second AskPass modal", () => rootElement.querySelector('[role="dialog"]') !== null);
  inputValue(rootElement.querySelector<HTMLInputElement>('input[name="remote-askpass-response"]')!, "discard-me");
  await act(async () => {
    button(rootElement, "Cancel").click();
    await flush();
  });
  ok(responses[1]?.requestId === "ask-2" && responses[1]?.cancelled === true && responses[1]?.value === "", "AskPass cancel sends no credential value");
  ok(!rootElement.innerHTML.includes("discard-me"), "cancelled AskPass secret is removed from the DOM");

  await act(async () => root.unmount());
  dom.close();
}

// A connected Remote target with no attached Session exposes the real opaque-ref
// directory workflow. A connected-but-unattached target leaves the catalog
// reachable; only explicit Add Project opens the cancellable setup dialog.
// The dialog supports paging/navigation, a primary root, repeatable additional
// roots, backend errors, and closes as soon as creation succeeds.
{
  const dom = installDOM();
  const target: RemoteTargetStatusView = {
    state: "RemoteConnected",
    hostId: "host-remote",
    hostLabel: "Linux Host",
    canReconnect: false,
  };
  let workbench: RemoteWorkbenchStatusView = { hostId: target.hostId, sessionAttached: false };
  const browseInputs: RemoteWorkspaceBrowseInput[] = [];
  const createInputs: RemoteCreateWorkspaceSessionInput[] = [];
  let createFailure = "workspace lease changed";
  let holdCreate = false;
  let releaseCreate: (() => void) | null = null;
  let reconnects = 0;

  const home: RemoteDirectoryView = { ref: "dir-home", name: "dev", displayPath: "/home/dev" };
  const projects: RemoteDirectoryView = { ref: "dir-projects", name: "projects", displayPath: "/home/dev/projects", parentRef: home.ref };
  const scratch: RemoteDirectoryView = { ref: "dir-scratch", name: "scratch", displayPath: "/home/dev/scratch", parentRef: home.ref };

  function page(input: RemoteWorkspaceBrowseInput): RemoteWorkspacePageView {
    if (input.directoryRef === projects.ref) return { directory: projects, entries: [], hasMore: false };
    if (input.directoryRef === scratch.ref) return { directory: scratch, entries: [], hasMore: false };
    if (input.cursor === "home-next") return { directory: home, entries: [scratch], hasMore: false };
    return { directory: home, entries: [projects], hasMore: true, next: "home-next" };
  }

  window.go = {
    main: {
      App: {
        RemoteTargetStatus: async () => ({ ...target }),
        RemoteWorkbenchStatus: async () => ({ ...workbench }),
        BrowseRemoteWorkspace: async (input: RemoteWorkspaceBrowseInput) => {
          browseInputs.push({ ...input });
          return page(input);
        },
        CreateRemoteWorkspaceSession: async (input: RemoteCreateWorkspaceSessionInput) => {
          createInputs.push({ ...input, additionalDirectoryRefs: [...input.additionalDirectoryRefs] });
          if (holdCreate) await new Promise<void>((resolve) => { releaseCreate = resolve; });
          if (createFailure) throw new Error(createFailure);
          workbench = {
            hostId: target.hostId,
            workspaceName: "projects",
            workspaceDisplayPath: projects.displayPath,
            sessionAttached: true,
            tabId: "remote-tab-1",
            topicTitle: input.topicTitle,
          };
          dom.emit("remote:workbench-state", workbench);
          return { ...workbench };
        },
        ReconnectRemoteTarget: async () => { reconnects += 1; },
        SwitchToLocalTarget: async () => {},
        RespondRemoteAskPass: async () => {},
      } as Partial<AppBindings> as AppBindings,
    },
  };

  const { root, rootElement } = render(React.createElement(RemoteTargetSurfaces, { target }));
  ok(rootElement.querySelector(".remote-target-surface") === null, "healthy Remote connection no longer renders a floating card over the workbench");
  ok(rootElement.querySelector(".remote-workspace-modal") === null, "unattached Remote target does not open a blocking workspace dialog automatically");
  ok(browseInputs.length === 0, "unattached Remote target does not start workspace creation browsing without Add Project");

  const disconnectedTarget: RemoteTargetStatusView = {
    state: "Disconnected",
    hostId: target.hostId,
    hostLabel: target.hostLabel,
    failure: "ssh exited",
    canReconnect: true,
  };
  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(RemoteTargetSurfaces, { target: disconnectedTarget })));
    await flush();
  });
  ok(rootElement.textContent?.includes("ssh exited") === true && button(rootElement, "Reconnect") !== null, "connection failure keeps its recovery card and action visible");
  ok(rootElement.querySelector(".remote-target-surface__toggle") === null, "connection failure recovery cannot be collapsed away");
  await act(async () => {
    button(rootElement, "Reconnect").click();
    await flush();
  });
  ok(reconnects === 1, "connection failure recovery still invokes the backend reconnect action");
  act(() => root.render(React.createElement(LocaleProvider, null, React.createElement(RemoteTargetSurfaces, { target }))));
  ok(rootElement.querySelector(".remote-target-surface") === null, "healthy recovery returns status ownership to the toolbar without a floating card");

  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(RemoteTargetSurfaces, { target, workspaceSetupRequest: 1 })));
    await flush();
  });
  await waitFor("explicit Remote workspace setup", () => rootElement.textContent?.includes("Open a Remote workspace") === true);
  ok(rootElement.querySelector(".remote-workspace-modal")?.getAttribute("role") === "dialog", "explicit Add Project opens Remote workspace setup");
  ok(browseInputs[0]?.limit === 100 && !browseInputs[0]?.directoryRef && !browseInputs[0]?.typedPath, "initial browse uses the Host default directory with a bounded page size");

  await act(async () => {
    button(rootElement, "Load more").click();
    await flush();
  });
  await waitFor("paged directory", () => rootElement.textContent?.includes("scratch") === true);
  ok(browseInputs[1]?.directoryRef === home.ref && browseInputs[1]?.cursor === "home-next", "directory pagination preserves the opaque directory ref and cursor");

  await act(async () => {
    directoryButton(rootElement, "scratch").click();
    await flush();
  });
  await waitFor("scratch directory", () => rootElement.querySelector<HTMLInputElement>('input[name="remote-workspace-path"]')?.value === scratch.displayPath);
  await act(async () => {
    button(rootElement, "Add directory").click();
    await flush();
  });
  ok(rootElement.textContent?.includes(scratch.displayPath), "current directory can be added as a repeatable additional root");

  await act(async () => {
    button(rootElement, "Parent").click();
    await flush();
  });
  await waitFor("parent directory", () => rootElement.querySelector<HTMLInputElement>('input[name="remote-workspace-path"]')?.value === home.displayPath);
  await act(async () => {
    directoryButton(rootElement, "projects").click();
    await flush();
  });
  await waitFor("projects directory", () => rootElement.querySelector<HTMLInputElement>('input[name="remote-workspace-path"]')?.value === projects.displayPath);
  await act(async () => {
    button(rootElement, "Use as primary").click();
    await flush();
  });
  ok(rootElement.textContent?.includes("Primary selected"), "current directory can be selected as the primary workspace root");

  const topicInput = rootElement.querySelector<HTMLInputElement>(".remote-workspace-topic input");
  inputValue(topicInput!, "Remote V1 implementation");
  ok(!button(rootElement, "Create Remote session").disabled, "session creation enables only after primary directory and Topic title are present");
  await act(async () => {
    button(rootElement, "Create Remote session").click();
    await flush();
  });
  await waitFor("create error", () => rootElement.textContent?.includes(createFailure) === true);
  ok(createInputs.length === 1 && rootElement.querySelector('[role="alert"]')?.textContent?.includes(createFailure), "Session create failures remain actionable without discarding the selected workspace");

  createFailure = "";
  await act(async () => {
    button(rootElement, "Create Remote session").click();
    await flush();
  });
  await waitFor("attached workbench", () => rootElement.querySelector(".remote-workspace-modal") === null);
  ok(createInputs[1]?.primaryDirectoryRef === projects.ref, "Session create sends the opaque primary directory ref");
  ok(createInputs[1]?.additionalDirectoryRefs.length === 1 && createInputs[1]?.additionalDirectoryRefs[0] === scratch.ref, "Session create sends every selected additional opaque directory ref");
  ok(createInputs[1]?.topicTitle === "Remote V1 implementation", "Session create sends the trimmed Topic title");
  ok(!rootElement.textContent?.includes("Open a Remote workspace"), "successful Remote Session creation closes setup instead of rendering a second chat surface");

  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(RemoteTargetSurfaces, { target, workspaceSetupRequest: 2 })));
    await flush();
  });
  await waitFor("explicit Remote workspace setup", () => rootElement.querySelector(".remote-workspace-modal") !== null);
  ok(browseInputs.at(-1)?.limit === 100, "an add-project request reopens Host browsing after a Remote Session is attached");
  const closeSetup = rootElement.querySelector<HTMLButtonElement>('button[aria-label="Close"]');
  ok(closeSetup, "explicit Remote workspace setup is cancellable");
  await act(async () => {
    closeSetup.click();
    await flush();
  });
  ok(rootElement.querySelector(".remote-workspace-modal") === null && workbench.sessionAttached, "closing explicit setup preserves the attached Remote Session");

  await act(async () => {
    root.render(React.createElement(LocaleProvider, null, React.createElement(RemoteTargetSurfaces, { target, workspaceSetupRequest: 3 })));
    await flush();
  });
  await waitFor("second explicit Remote workspace setup", () => rootElement.textContent?.includes("Use as primary") === true);
  await act(async () => {
    button(rootElement, "Use as primary").click();
    await flush();
  });
  inputValue(rootElement.querySelector<HTMLInputElement>(".remote-workspace-topic input")!, "Concurrent Remote create");
  holdCreate = true;
  await act(async () => {
    button(rootElement, "Create Remote session").click();
  });
  await waitFor("Remote create in progress", () => rootElement.querySelector<HTMLButtonElement>('button[aria-label="Close"]')?.disabled === true);
  ok(rootElement.querySelector<HTMLButtonElement>('button[aria-label="Close"]')?.disabled, "explicit setup cannot close while Remote Session creation is in flight");
  await act(async () => {
    holdCreate = false;
    releaseCreate?.();
    await flush();
  });
  await waitFor("concurrent create completion", () => rootElement.querySelector(".remote-workspace-modal") === null);

  await act(async () => root.unmount());
  dom.close();
}

// Create is scoped to the exact connected Host and explicit setup request.
// An old Host completion must not close or unlock a new Host's in-flight modal,
// and an old Host error must not appear in the new Host setup.
{
  const dom = installDOM();
  const hostA: RemoteTargetStatusView = {
    state: "RemoteConnected",
    hostId: "host-create-a",
    hostLabel: "Create Host A",
    canReconnect: false,
  };
  const hostB: RemoteTargetStatusView = {
    state: "RemoteConnected",
    hostId: "host-create-b",
    hostLabel: "Create Host B",
    canReconnect: false,
  };
  const hostC: RemoteTargetStatusView = {
    state: "RemoteConnected",
    hostId: "host-create-c",
    hostLabel: "Create Host C",
    canReconnect: false,
  };
  const project: RemoteDirectoryView = { ref: "create-project", name: "project", displayPath: "/srv/project" };
  const pendingCreates: Array<{
    input: RemoteCreateWorkspaceSessionInput;
    resolve: (value: RemoteWorkbenchStatusView) => void;
    reject: (cause: Error) => void;
  }> = [];

  window.go = {
    main: {
      App: {
        BrowseRemoteWorkspace: async () => ({ directory: project, entries: [], hasMore: false }),
        CreateRemoteWorkspaceSession: async (input: RemoteCreateWorkspaceSessionInput) => new Promise<RemoteWorkbenchStatusView>((resolve, reject) => {
          pendingCreates.push({ input, resolve, reject });
        }),
      } as Partial<AppBindings> as AppBindings,
    },
  };

  const setup = (target: RemoteTargetStatusView, requestSignal: number) => (
    React.createElement(LocaleProvider, null, React.createElement(RemoteWorkspaceSetup, { target, requestSignal }))
  );
  const { root, rootElement } = render(React.createElement(RemoteWorkspaceSetup, { target: hostA, requestSignal: 1 }));

  const startCreate = async (title: string, expectedPending: number) => {
    await waitFor(`${title} workspace browse`, () => rootElement.textContent?.includes("Use as primary") === true);
    await act(async () => {
      button(rootElement, "Use as primary").click();
      await flush();
    });
    inputValue(rootElement.querySelector<HTMLInputElement>(".remote-workspace-topic input")!, title);
    await act(async () => {
      button(rootElement, "Create Remote session").click();
      await flush();
    });
    await waitFor(`${title} create request`, () => pendingCreates.length === expectedPending);
  };

  await startCreate("Host A pending create", 1);
  ok(rootElement.querySelector<HTMLButtonElement>('button[aria-label="Close"]')?.disabled, "Host A setup locks close only after its create begins");

  await act(async () => {
    root.render(setup(hostA, 2));
    await flush();
  });
  ok(
    rootElement.querySelector(".remote-workspace-modal") !== null &&
      rootElement.querySelector<HTMLInputElement>(".remote-workspace-topic input")?.value === "Host A pending create",
    "same-Host Add Project request preserves the create-pending modal",
  );
  ok(rootElement.querySelector<HTMLButtonElement>('button[aria-label="Close"]')?.disabled, "same-Host Add Project request cannot reopen Close while create is pending");
  ok(pendingCreates.length === 1, "same-Host Add Project request cannot queue a second create");

  await act(async () => {
    root.render(setup(hostB, 3));
    await flush();
  });
  await startCreate("Host B current create", 2);
  ok(rootElement.textContent?.includes("Create Host B"), "Host switch renders the new Host setup while the old create is pending");
  ok(rootElement.querySelector<HTMLButtonElement>('button[aria-label="Close"]')?.disabled, "current Host B create keeps its own close control disabled");

  await act(async () => {
    pendingCreates[0].resolve({ hostId: hostA.hostId, sessionAttached: true, topicTitle: pendingCreates[0].input.topicTitle });
    await flush();
  });
  ok(rootElement.querySelector(".remote-workspace-modal") !== null && rootElement.textContent?.includes("Create Host B"), "stale Host A success cannot close the Host B setup");
  ok(rootElement.querySelector<HTMLButtonElement>('button[aria-label="Close"]')?.disabled, "stale Host A finally cannot unlock the current Host B create");

  await act(async () => {
    pendingCreates[1].resolve({ hostId: hostB.hostId, sessionAttached: true, topicTitle: pendingCreates[1].input.topicTitle });
    await flush();
  });
  await waitFor("current Host B create completion", () => rootElement.querySelector(".remote-workspace-modal") === null);

  await act(async () => {
    root.render(setup(hostB, 4));
    await flush();
  });
  await startCreate("Host B stale error", 3);
  await act(async () => {
    root.render(setup(hostC, 5));
    await flush();
  });
  await waitFor("Host C setup", () => rootElement.textContent?.includes("Create Host C") === true && rootElement.textContent?.includes("Use as primary") === true);
  await act(async () => {
    pendingCreates[2].reject(new Error("stale Host B lease error"));
    await flush();
  });
  ok(!rootElement.textContent?.includes("stale Host B lease error"), "stale Host B error cannot paint the Host C setup");
  ok(rootElement.querySelector<HTMLButtonElement>('button[aria-label="Close"]')?.disabled === false, "stale Host B finally cannot change Host C's idle close state");

  await act(async () => {
    rootElement.querySelector<HTMLButtonElement>('button[aria-label="Close"]')?.click();
    await flush();
    root.unmount();
  });
  dom.close();
}

// Browse commits require both their request sequence and committed setup
// authority. Old Host success/error/finally cannot replace or unlock the new
// Host's pending browser before its own response arrives.
{
  const dom = installDOM();
  const browseHostA: RemoteTargetStatusView = {
    state: "RemoteConnected",
    hostId: "host-browse-a",
    hostLabel: "Browse Host A",
    canReconnect: false,
  };
  const browseHostB: RemoteTargetStatusView = {
    state: "RemoteConnected",
    hostId: "host-browse-b",
    hostLabel: "Browse Host B",
    canReconnect: false,
  };
  const browseHostC: RemoteTargetStatusView = {
    state: "RemoteConnected",
    hostId: "host-browse-c",
    hostLabel: "Browse Host C",
    canReconnect: false,
  };
  const directoryA: RemoteDirectoryView = { ref: "browse-a", name: "a", displayPath: "/srv/host-a" };
  const directoryB: RemoteDirectoryView = { ref: "browse-b", name: "b", displayPath: "/srv/host-b" };
  const directoryC: RemoteDirectoryView = { ref: "browse-c", name: "c", displayPath: "/srv/host-c" };
  const pendingBrowses: Array<{
    resolve: (page: RemoteWorkspacePageView) => void;
    reject: (cause: Error) => void;
  }> = [];

  window.go = {
    main: {
      App: {
        BrowseRemoteWorkspace: async () => new Promise<RemoteWorkspacePageView>((resolve, reject) => {
          pendingBrowses.push({ resolve, reject });
        }),
      } as Partial<AppBindings> as AppBindings,
    },
  };

  const setup = (target: RemoteTargetStatusView, requestSignal: number) => (
    React.createElement(LocaleProvider, null, React.createElement(RemoteWorkspaceSetup, { target, requestSignal }))
  );
  const { root, rootElement } = render(React.createElement(RemoteWorkspaceSetup, { target: browseHostA, requestSignal: 1 }));
  await waitFor("Host A browse request", () => pendingBrowses.length === 1);

  await act(async () => {
    root.render(setup(browseHostB, 2));
    await flush();
  });
  await waitFor("Host B browse request", () => pendingBrowses.length === 2 && rootElement.textContent?.includes("Browse Host B") === true);
  ok(rootElement.querySelector(".remote-workspace-loading") !== null, "Host B browser remains loading while its current request is pending");

  await act(async () => {
    pendingBrowses[0].resolve({ directory: directoryA, entries: [], hasMore: false });
    await flush();
  });
  ok(!rootElement.textContent?.includes(directoryA.displayPath), "stale Host A browse success cannot paint its directory into Host B");
  ok(rootElement.querySelector(".remote-workspace-loading") !== null, "stale Host A browse finally cannot clear Host B's loading state");

  await act(async () => {
    pendingBrowses[1].resolve({ directory: directoryB, entries: [], hasMore: false });
    await flush();
  });
  await waitFor("Host B directory", () => rootElement.querySelector<HTMLInputElement>('input[name="remote-workspace-path"]')?.value === directoryB.displayPath);

  await act(async () => {
    root.render(setup(browseHostB, 3));
    await flush();
  });
  await waitFor("next Host B browse request", () => pendingBrowses.length === 3);
  await act(async () => {
    root.render(setup(browseHostC, 4));
    await flush();
  });
  await waitFor("Host C browse request", () => pendingBrowses.length === 4 && rootElement.textContent?.includes("Browse Host C") === true);
  await act(async () => {
    pendingBrowses[2].reject(new Error("stale Host B browse error"));
    await flush();
  });
  ok(!rootElement.textContent?.includes("stale Host B browse error"), "stale Host B browse error cannot paint Host C");
  ok(rootElement.querySelector(".remote-workspace-loading") !== null, "stale Host B browse error/finally cannot unlock Host C loading");

  await act(async () => {
    pendingBrowses[3].resolve({ directory: directoryC, entries: [], hasMore: false });
    await flush();
  });
  await waitFor("Host C directory", () => rootElement.querySelector<HTMLInputElement>('input[name="remote-workspace-path"]')?.value === directoryC.displayPath);

  await act(async () => {
    root.unmount();
  });
  dom.close();
}

console.log(`Remote target UI: ${passed} checks passed`);
