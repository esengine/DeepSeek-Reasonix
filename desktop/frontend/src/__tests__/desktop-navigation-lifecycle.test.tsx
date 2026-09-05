import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useDesktopNavigation } from "../app-runtime/useDesktopNavigation";
import type { DesktopNavigationPorts } from "../app-runtime/desktopNavigationOwner";
import type { SessionMeta, TabMeta } from "../lib/types";
import type { SidebarImConnection } from "../app-runtime/sidebarImProjection";
import type { Translator } from "../lib/i18n";
import { __emitMockRemoteTabOpened } from "../lib/remoteTabEvents";
import { useRemoteTabOpened } from "../lib/useRemoteTabOpened";

function deferred<T>() { let resolve!: (value: T) => void; let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }
const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
const tab = (id: string) => ({ id, label: id } as TabMeta);
const pending = new Map<string, ReturnType<typeof deferred<TabMeta>>>();
const calls: string[] = [];
const acceptedTopics: number[] = [];
let intent = 0;
let registration: ReturnType<typeof deferred<string>> | undefined;
let api!: ReturnType<typeof useDesktopNavigation>;
const activate = (id: string) => { calls.push(`open:${id}`); const request = deferred<TabMeta>(); pending.set(id, request); return request.promise; };
const ports: Parameters<typeof useDesktopNavigation>[0]["ports"] = {
  isNavigationIntentCurrent: seq => seq === intent,
  registeredNavigationIntent: async seq => registration ? registration.promise : String(seq),
  openRemoteProject: async (_host, workspace) => activate(`remote:${workspace}`),
  switchRemoteTab: async (meta, seq) => { calls.push(`remote-switch:${meta.id}:${seq}`); },
  activateTopic: async (_scope, _workspace, id) => activate(id),
  openTopicSession: async (_scope, _workspace, id) => { calls.push("classic-session"); return activate(id); },
  openGlobalTab: async id => { calls.push("classic-global"); return activate(id); },
  openProjectTab: async (_workspace, id) => { calls.push("classic-project"); return activate(id); },
  ensureBlankSurface: async (_scope, workspace) => activate(`blank:${workspace}`),
  ensureBlankTab: async (_scope, workspace) => { calls.push("classic-blank"); return activate(`blank:${workspace}`); },
  createIsolatedWorktree: async workspace => ({ tab: await activate(`worktree:${workspace}`), branch: "fixture", sourceDirty: true }) as Awaited<ReturnType<DesktopNavigationPorts["createIsolatedWorktree"]>>,
  openChannelSession: async (path, id) => { calls.push(`channel:${id}:${path}`); },
  resumeSession: async (path, id) => { calls.push(`resume:${id}:${path}`); },
  listTabs: async () => [], applyTabs: () => { calls.push("tabs"); }, seedTab: value => { calls.push(`seed:${value.id}`); },
  listSessions: async () => { calls.push("history-refresh"); return []; },
  topicAccepted: seq => { acceptedTopics.push(seq); },
};
function Probe({ visible = "A", single = true }: { visible?: string; single?: boolean }) {
  useRemoteTabOpened(meta => { calls.push(`resource:${meta.id}`); }, () => {});
  api = useDesktopNavigation({ visible: { tabId: visible, sessionKey: visible }, singleSurface: single, ports,
    setTabRevealSignal: () => { calls.push("reveal-tab"); }, setTranscriptRevealSignal: () => { calls.push("reveal-transcript"); },
    setProjectRevision: () => { calls.push("project"); }, setHistory: () => { calls.push("history-close"); },
    t: ((key: string) => key) as Translator, showToast: message => { calls.push(`notice:${message}`); },
    noteIntent: () => ++intent, beginSurface: seq => { calls.push(`begin:${seq}`); },
    settleSurface: seq => { if (seq === intent) calls.push(`settle:${seq}`); }, showChat: () => {},
  });
  return null;
}
const paint = (visible = "A", single = true) => act(async () => root.render(<Probe visible={visible} single={single} />));
const topic = (id: string) => api.enqueueNavigation({ kind: "topic", scope: "project", workspaceRoot: "fixture", topicId: id });
async function finish(id: string, task: Promise<void>) { pending.get(id)!.resolve(tab(id)); await task; }
try {
  await paint();
  const entry = api.enqueueNavigation;
  const a = topic("A"), b = topic("B"), c = topic("C");
  await b;
  assert.deepEqual(calls.filter(value => value.startsWith("open:")), ["open:A"]);
  await finish("A", a);
  assert.deepEqual(calls.filter(value => value.startsWith("open:")), ["open:A", "open:C"]);
  await finish("C", c);
  assert.deepEqual(calls.filter(value => value.startsWith("seed:")), ["seed:C"]);
  assert.deepEqual(acceptedTopics, [3], "only the accepted queue target can release an automation link");
  assert.deepEqual(calls.filter(value => value.startsWith("settle:")), ["settle:3"], "old finally cannot settle the current surface");
  calls.length = 0;
  const stale = topic("stale"); intent++;
  await paint("B"); await paint("A");
  assert.equal(api.enqueueNavigation, entry);
  await finish("stale", stale);
  assert.deepEqual(calls.filter(value => /^(seed|tabs|notice|reveal|settle)/.test(value)), [], "ABA never restores old UI rights");

  calls.length = 0;
  const connection = { sessionId: "path:channel.jsonl", sessionSource: "auto", scope: "project", workspaceRoot: "im", title: "fixture" } as SidebarImConnection;
  const im = api.enqueueNavigation({ kind: "sidebar-im", connection });
  intent++;
  await finish("blank:im", im);
  assert.ok(!calls.some(value => value.startsWith("channel:")), "cancellation between blank activation and hydrate prevents a second mutation");
  calls.length = 0;
  const validIM = api.enqueueNavigation({ kind: "sidebar-im", connection });
  await finish("blank:im", validIM);
  assert.ok(calls.includes("channel:blank:im:channel.jsonl"));

  calls.length = 0;
  const isolated = api.enqueueNavigation({ kind: "isolated-worktree", workspaceRoot: "dirty" });
  await paint(); // Normal commits do not change the request epoch.
  await finish("worktree:dirty", isolated);
  assert.ok(calls.includes("notice:projectTree.worktreeCreatedDirty"));
  assert.ok(calls.includes("project"));

  calls.length = 0;
  await paint("A", false);
  const history = api.enqueueNavigation({ kind: "resume-session", session: { scope: "global", topicId: "history", path: "history.jsonl" } as SessionMeta });
  await finish("history", history);
  assert.ok(calls.includes("classic-session"));
  assert.ok(calls.includes("history-close"));

  calls.length = 0;
  const failed = topic("failed");
  pending.get("failed")!.reject(new Error("fixture failure")); await failed;
  assert.deepEqual(calls.filter(value => value.startsWith("notice:")), ["notice:history.failedOpenSession"]);

  calls.length = 0;
  registration = deferred<string>();
  const waitingRemote = api.openRemoteProject({ hostId: "fixture", workspace: "waiting" }, { newSession: true });
  const winsRegistration = topic("wins-registration");
  registration.resolve("registered");
  assert.equal((await waitingRemote).status, "cancelled");
  assert.ok(!pending.has("remote:waiting"), "superseded registration cannot issue an OpenRemoteProjectTab request");
  await finish("wins-registration", winsRegistration);
  registration = undefined;

  calls.length = 0;
  const remote = api.openRemoteProject({ hostId: "fixture", workspace: "remote" }, { sessionName: "selected" });
  await act(async () => {});
  const remoteMeta = { ...tab("remote:remote"), remote: { hostId: "fixture", workspace: "remote" } };
  await act(async () => __emitMockRemoteTabOpened(remoteMeta));
  assert.deepEqual(calls.filter(value => /^(seed|remote-switch)/.test(value)), [], "opened event before the response cannot independently navigate");
  const localWins = topic("local-wins");
  pending.get("remote:remote")!.resolve(remoteMeta);
  assert.equal((await remote).status, "cancelled");
  await finish("local-wins", localWins);
  assert.ok(!calls.some(value => value.startsWith("remote-switch:")));
  calls.length = 0;
  const successfulRemote = api.openRemoteProject({ hostId: "fixture", workspace: "success" }, {});
  await act(async () => {});
  pending.get("remote:success")!.resolve({ ...remoteMeta, id: "remote:success" });
  const outcome = await successfulRemote;
  assert.equal(outcome.status, "completed");
  assert.ok(calls.includes(`remote-switch:remote:success:${intent}`), "the request's exact intent reaches dedicated remote activation");
  assert.ok(!calls.includes("classic-session"));

  calls.length = 0;
  const retainedRemote = api.openRemoteProject;
  const disposed = topic("disposed"), queued = topic("never");
  await act(async () => root.unmount());
  await finish("disposed", disposed); await queued;
  entry({ kind: "blank", scope: "global", workspaceRoot: "" });
  assert.deepEqual(calls.filter(value => /^(open|seed|tabs|notice|reveal|settle)/.test(value)), ["open:disposed"], "unmount releases pending input and fences queued and running continuations");
  assert.deepEqual(await retainedRemote({ hostId: "fixture", workspace: "disposed" }, {}), { status: "cancelled", reason: "disposed" });
  console.log("desktop navigation: queue ownership, ABA, IM hydrate, dirty-worktree warning, Classic resume, failure and disposal passed");
} finally { dom.window.close(); }
