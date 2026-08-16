import { JSDOM } from "jsdom";

const dom = new JSDOM("<!doctype html><html><body></body></html>");
const previousWindow = globalThis.window;
globalThis.window = dom.window as unknown as Window & typeof globalThis;

type Session = {
  id: string;
  title: string;
  shell: string;
  cwd: string;
  createdAt: number;
  running: boolean;
};
type Workspace = { available: boolean; readOnly: boolean; sessions: Session[]; shells: { id: string; label: string }[] };
const pending = new Map<string, (value: Workspace) => void>();
const pendingReject = new Map<string, (error: Error) => void>();
const createPending: Array<(value: Session) => void> = [];
const createReject: Array<(error: Error) => void> = [];
let calls = 0;
let closeError: Error | null = null;
let writeError: Error | null = null;
const fakeApp = {
  TerminalWorkspaceForTab(tabId: string) {
    calls += 1;
    return new Promise<Workspace>((resolve, reject) => {
      pending.set(tabId, resolve);
      pendingReject.set(tabId, reject);
    });
  },
  CreateTerminalForTab() {
    return new Promise<Session>((resolve, reject) => {
      createPending.push(resolve);
      createReject.push(reject);
    });
  },
  async CloseTerminalForTab() {
    if (closeError) throw closeError;
  },
  async WriteTerminalForTab() {
    if (writeError) throw writeError;
  },
};
(globalThis.window as unknown as { go: unknown }).go = { main: { App: fakeApp } };

const { resetTerminalStoreForTests, useTerminalStore } = await import("../store/terminal");
resetTerminalStoreForTests();

const oldRequest = useTerminalStore.getState().syncWorkspace("old-tab");
const newRequest = useTerminalStore.getState().syncWorkspace("new-tab");
pending.get("new-tab")?.({ available: true, readOnly: false, sessions: [], shells: [] });
await newRequest;
pending.get("old-tab")?.({ available: true, readOnly: false, sessions: [], shells: [] });
await oldRequest;
if (useTerminalStore.getState().tabId !== "new-tab") throw new Error("stale workspace response replaced the selected tab");
if (calls !== 2) throw new Error(`expected two workspace calls, got ${calls}`);

resetTerminalStoreForTests();
calls = 0;
const shared = useTerminalStore.getState().ensureReady("same-tab");
const sharedAgain = useTerminalStore.getState().ensureReady("same-tab");
pending.get("same-tab")?.({ available: true, readOnly: false, sessions: [], shells: [] });
await Promise.all([shared, sharedAgain]);
if (calls !== 1) throw new Error(`ensureReady duplicated the first request: ${calls}`);
process.stdout.write("PASS stale responses and first-ready deduplication\n");

resetTerminalStoreForTests();
calls = 0;
createPending.length = 0;
const firstOpenCreate = useTerminalStore.getState().createSession("first-open");
await Promise.resolve();
const panelReady = useTerminalStore.getState().syncWorkspace("first-open");
await Promise.resolve();
if (calls !== 1) throw new Error(`panel readiness duplicated the first-open workspace request: ${calls}`);
pending.get("first-open")?.({
  available: true,
  readOnly: false,
  sessions: [],
  shells: [{ id: "default", label: "Default shell" }],
});
await panelReady;
await Promise.resolve();
if (createPending.length !== 1) throw new Error(`first-open create did not reach the backend: ${createPending.length}`);
const firstOpenSession: Session = {
  id: "first-open-session",
  title: "default",
  shell: "default",
  cwd: ".",
  createdAt: 1,
  running: true,
};
createPending[0]?.(firstOpenSession);
const createdOnFirstOpen = await firstOpenCreate;
if (createdOnFirstOpen?.id !== firstOpenSession.id) throw new Error("first-open create was discarded");
process.stdout.write("PASS first-open panel readiness shares the pending workspace request\n");

resetTerminalStoreForTests();
calls = 0;
const staleCapability = useTerminalStore.getState().syncWorkspace("capability-refresh");
await Promise.resolve();
const resolveStaleCapability = pending.get("capability-refresh");
const latestCapability = useTerminalStore.getState().syncWorkspace("capability-refresh", true);
await Promise.resolve();
if (calls !== 2) throw new Error(`forced capability refresh did not start a new request: ${calls}`);
pending.get("capability-refresh")?.({
  available: true,
  readOnly: false,
  sessions: [],
  shells: [{ id: "default", label: "Default shell" }],
});
await latestCapability;
resolveStaleCapability?.({
  available: true,
  readOnly: true,
  sessions: [],
  shells: [{ id: "default", label: "Default shell" }],
});
await staleCapability;
if (useTerminalStore.getState().workspace?.readOnly) {
  throw new Error("stale read-only capability replaced the forced writable refresh");
}
process.stdout.write("PASS forced capability refresh supersedes an in-flight same-tab request\n");

resetTerminalStoreForTests();
calls = 0;
const paintedWorkspace: Workspace = {
  available: true,
  readOnly: false,
  sessions: [{
    id: "painted",
    title: "zsh",
    shell: "default",
    cwd: ".",
    createdAt: 1,
    running: true,
  }],
  shells: [{ id: "default", label: "Default shell" }],
};
useTerminalStore.setState({
  tabId: "warm-tab",
  workspace: paintedWorkspace,
  loading: false,
  activeSessionId: "painted",
});
const warmRefresh = useTerminalStore.getState().syncWorkspace("warm-tab");
await Promise.resolve();
if (useTerminalStore.getState().workspace?.sessions[0]?.id !== "painted") {
  throw new Error("same-tab refresh cleared the painted workspace before the response landed");
}
if (useTerminalStore.getState().loading !== true) {
  throw new Error("same-tab refresh should still mark loading while the request is in flight");
}
pending.get("warm-tab")?.(paintedWorkspace);
await warmRefresh;
if (calls !== 1) throw new Error(`same-tab refresh expected one workspace call, got ${calls}`);
process.stdout.write("PASS same-tab refresh keeps the painted workspace warm\n");

resetTerminalStoreForTests();
createPending.length = 0;
const readyWorkspace: Workspace = {
  available: true,
  readOnly: false,
  sessions: [],
  shells: [{ id: "default", label: "Default shell" }],
};
useTerminalStore.setState({
  tabId: "create-tab",
  workspace: readyWorkspace,
  loading: false,
  activeSessionId: null,
});
const firstCreate = useTerminalStore.getState().createSession("create-tab");
const secondCreate = useTerminalStore.getState().createSession("create-tab");
await Promise.resolve();
await Promise.resolve();
if (createPending.length !== 2) throw new Error(`expected two create calls, got ${createPending.length}`);
const firstSession: Session = {
  id: "first",
  title: "first",
  shell: "default",
  cwd: ".",
  createdAt: 1,
  running: true,
};
const secondSession: Session = {
  id: "second",
  title: "second",
  shell: "default",
  cwd: ".",
  createdAt: 2,
  running: true,
};
createPending[1]?.(secondSession);
await secondCreate;
createPending[0]?.(firstSession);
await firstCreate;
const sessionIDs = useTerminalStore.getState().workspace?.sessions.map((session) => session.id).sort();
if (JSON.stringify(sessionIDs) !== JSON.stringify(["first", "second"])) {
  throw new Error(`concurrent creates lost a session: ${JSON.stringify(sessionIDs)}`);
}
process.stdout.write("PASS concurrent terminal creates merge into the latest workspace state\n");

resetTerminalStoreForTests();
const failedSync = useTerminalStore.getState().syncWorkspace("sync-error");
pendingReject.get("sync-error")?.(new Error("workspace unavailable"));
await failedSync.catch(() => {});
if (useTerminalStore.getState().error !== "workspace unavailable") {
  throw new Error(`workspace sync error was not exposed: ${useTerminalStore.getState().error}`);
}
if (useTerminalStore.getState().loading || useTerminalStore.getState().workspace !== null) {
  throw new Error("failed workspace sync left a loading or partial workspace state");
}
process.stdout.write("PASS workspace sync failures remain visible and retryable\n");

resetTerminalStoreForTests();
createPending.length = 0;
createReject.length = 0;
useTerminalStore.setState({
  tabId: "create-error",
  workspace: readyWorkspace,
  loading: false,
  activeSessionId: null,
});
const failedCreate = useTerminalStore.getState().createSession("create-error");
await Promise.resolve();
createReject[0]?.(new Error("shell failed to start"));
await failedCreate.catch(() => {});
if (useTerminalStore.getState().error !== "shell failed to start") {
  throw new Error(`terminal create error was not exposed: ${useTerminalStore.getState().error}`);
}
if (useTerminalStore.getState().workspace?.sessions.length !== 0) {
  throw new Error("failed terminal create inserted a phantom session");
}
process.stdout.write("PASS terminal create failures do not insert phantom sessions\n");

resetTerminalStoreForTests();
closeError = new Error("terminal did not close");
useTerminalStore.setState({
  tabId: "close-error",
  workspace: { ...readyWorkspace, sessions: [firstSession] },
  loading: false,
  activeSessionId: firstSession.id,
});
await useTerminalStore.getState().closeSession("close-error", firstSession.id).catch(() => {});
if (useTerminalStore.getState().error !== "terminal did not close") {
  throw new Error(`terminal close error was not exposed: ${useTerminalStore.getState().error}`);
}
if (useTerminalStore.getState().workspace?.sessions[0]?.id !== firstSession.id) {
  throw new Error("failed terminal close removed the live session from the workspace");
}
useTerminalStore.getState().clearError();
if (useTerminalStore.getState().error !== null) throw new Error("terminal error dismissal did not clear the error");
closeError = null;
process.stdout.write("PASS terminal close failures preserve the session and can be dismissed\n");

writeError = new Error("terminal input failed");
await useTerminalStore.getState().write("close-error", firstSession.id, "echo test").catch(() => {});
if (useTerminalStore.getState().error !== "terminal input failed") {
  throw new Error(`terminal write error was not exposed: ${useTerminalStore.getState().error}`);
}
writeError = null;
process.stdout.write("PASS terminal write failures remain visible to the user\n");

if (previousWindow) globalThis.window = previousWindow;
else delete (globalThis as { window?: unknown }).window;
