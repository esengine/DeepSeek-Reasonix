import assert from "node:assert/strict";
import type { ControlResult, SessionMeta } from "../lib/types";
import { resolveTaskMonitorSession } from "../lib/taskMonitorNavigation";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

const openResult: ControlResult = {
  schema_version: 1,
  command: "open_session",
  task_id: "session-a--task-1",
  session_id: "session-a",
  accepted: true,
  idempotent: false,
};
const session: SessionMeta = {
  path: "/project-a/session-a.jsonl",
  preview: "project A",
  turns: 1,
  createdAt: 1,
  lastActivityAt: 1,
  modTime: 1,
  current: false,
  open: false,
};
const sessionIDFromPath = (path: string) => path.split("/").pop()?.replace(/\.jsonl$/, "") ?? "";

async function testLateOpenCompletionIsDiscarded() {
  const open = deferred<ControlResult>();
  let currentIntent = 1;
  let listCalls = 0;
  const pending = resolveTaskMonitorSession({
    tabID: "tab-a",
    taskID: openResult.task_id,
    intentSeq: 1,
    isIntentCurrent: (seq) => seq === currentIntent,
    openTaskSessionForTab: () => open.promise,
    listSessionsForTab: async () => {
      listCalls += 1;
      return [session];
    },
    sessionIDFromPath,
  });

  currentIntent = 2;
  open.resolve(openResult);
  assert.equal(await pending, null);
  assert.equal(listCalls, 0);
}

async function testLateSessionListIsDiscarded() {
  const list = deferred<SessionMeta[]>();
  let currentIntent = 1;
  const pending = resolveTaskMonitorSession({
    tabID: "tab-a",
    taskID: openResult.task_id,
    intentSeq: 1,
    isIntentCurrent: (seq) => seq === currentIntent,
    openTaskSessionForTab: async () => openResult,
    listSessionsForTab: () => list.promise,
    sessionIDFromPath,
  });

  await Promise.resolve();
  currentIntent = 2;
  list.resolve([session]);
  assert.equal(await pending, null);
}

async function testSourceTabAndSessionStayBound() {
  const calls: string[][] = [];
  const resolved = await resolveTaskMonitorSession({
    tabID: "tab-a",
    taskID: openResult.task_id,
    intentSeq: 7,
    isIntentCurrent: (seq) => seq === 7,
    openTaskSessionForTab: async (...args) => {
      calls.push(args);
      return openResult;
    },
    listSessionsForTab: async (tabID) => {
      calls.push([tabID]);
      return [session];
    },
    sessionIDFromPath,
  });

  assert.equal(resolved?.path, session.path);
  assert.deepEqual(calls, [["tab-a", openResult.task_id], ["tab-a"]]);
}

await testLateOpenCompletionIsDiscarded();
await testLateSessionListIsDiscarded();
await testSourceTabAndSessionStayBound();
console.log("task monitor navigation: 3 passed");
