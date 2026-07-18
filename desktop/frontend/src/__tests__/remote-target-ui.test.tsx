// Run: tsx src/__tests__/remote-target-ui.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";

import { RemoteSettingsPage } from "../components/RemoteSettingsPage";
import { RemoteTargetSurfaces } from "../components/RemoteTargetSurfaces";
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

console.log("\nRemote target UI");

// Host CRUD, backend errors, connection state, reconnect, and destructive
// switch confirmation all exercise the actual bridge-facing component contract.
{
  const dom = installDOM();
  let hosts: RemoteHostView[] = [{ id: "host-1", alias: "devbox", label: "Dev box" }];
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
          const saved = { id: input.id ?? "host-2", alias: input.alias, label: input.label, sshConfigPath: input.sshConfigPath };
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
    button(rootElement, "Add Host").click();
    await flush();
  });
  const editorInputs = rootElement.querySelectorAll<HTMLInputElement>(".remote-host-editor input");
  ok(editorInputs.length === 3, "Host editor exposes alias, label, and optional SSH config path");
  inputValue(editorInputs[0], "gpu-host");
  inputValue(editorInputs[1], "GPU Host");
  inputValue(editorInputs[2], "/tmp/ssh/config");
  ok(editorInputs[0].value === "gpu-host" && editorInputs[1].value === "GPU Host", "Host editor accepts controlled input");
  ok(!button(rootElement, "Save").disabled, "Host Save enables after required fields are filled");
  await act(async () => {
    button(rootElement, "Save").click();
    await flush();
  });
  await waitFor("saved Host", () => rootElement.textContent?.includes("GPU Host") === true);
  ok(savedInputs[0]?.alias === "gpu-host" && savedInputs[0]?.sshConfigPath === "/tmp/ssh/config", "Host create sends the documented input fields");

  await act(async () => {
    button(rootElement, "Edit").click();
    await flush();
  });
  const editInputs = rootElement.querySelectorAll<HTMLInputElement>(".remote-host-editor input");
  inputValue(editInputs[1], "GPU Host renamed");
  await act(async () => {
    button(rootElement, "Save").click();
    await flush();
  });
  await waitFor("edited Host", () => rootElement.textContent?.includes("GPU Host renamed") === true);
  ok(savedInputs[1]?.id === "host-2" && savedInputs[1]?.label === "GPU Host renamed", "Host edit preserves the stable Host id");

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
  const failedInputs = rootElement.querySelectorAll<HTMLInputElement>(".remote-host-editor input");
  inputValue(failedInputs[0], "bad-host");
  inputValue(failedInputs[1], "Bad Host");
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
// directory workflow. It supports paging/navigation, a primary root, repeatable
// additional roots, backend errors, and closes as soon as the workbench attaches.
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
        ReconnectRemoteTarget: async () => {},
        SwitchToLocalTarget: async () => {},
        RespondRemoteAskPass: async () => {},
      } as Partial<AppBindings> as AppBindings,
    },
  };

  const { root, rootElement } = render(React.createElement(RemoteTargetSurfaces));
  await waitFor("Remote workspace setup", () => rootElement.textContent?.includes("Open a Remote workspace") === true);
  ok(rootElement.querySelector(".remote-workspace-modal")?.getAttribute("role") === "dialog", "unattached Remote target opens the workspace setup dialog");
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
  ok(!rootElement.textContent?.includes("Open a Remote workspace"), "workbench-state attachment removes setup instead of rendering a second chat surface");

  await act(async () => root.unmount());
  dom.close();
}

console.log(`Remote target UI: ${passed} checks passed`);
