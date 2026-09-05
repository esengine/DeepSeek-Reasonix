import assert from "node:assert/strict";
import {
  BACKGROUND_RUNTIME_ACTIVE_REFRESH_MS,
  BACKGROUND_RUNTIME_ERROR_RETRY_BASE_MS,
  createBackgroundRuntimeRefreshCoordinator,
  sameBackgroundRuntimeLists,
} from "../lib/backgroundRuntimeRefresh";
import type { BackgroundRuntimeView } from "../lib/types";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

const flushPromises = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

const runtime = (overrides: Partial<BackgroundRuntimeView> = {}): BackgroundRuntimeView => ({
  tabId: "tab-1",
  title: "Run",
  detached: false,
  running: true,
  pendingPrompt: false,
  jobs: [],
  ...overrides,
});

assert.equal(BACKGROUND_RUNTIME_ACTIVE_REFRESH_MS, 5_000);
assert.equal(BACKGROUND_RUNTIME_ERROR_RETRY_BASE_MS, 5_000);
assert.equal(sameBackgroundRuntimeLists([runtime()], [runtime()]), true);
assert.equal(
  sameBackgroundRuntimeLists(
    [runtime({ tabId: "tab-a" }), runtime({ tabId: "tab-b" })],
    [runtime({ tabId: "tab-b" }), runtime({ tabId: "tab-a" })],
  ),
  true,
);
assert.equal(sameBackgroundRuntimeLists([runtime()], [runtime({ running: false })]), false);

{
  let loads = 0;
  const scheduled: Array<() => void> = [];
  const coordinator = createBackgroundRuntimeRefreshCoordinator(
    async () => { loads += 1; return []; },
    () => undefined,
    (callback) => { scheduled.push(callback); return scheduled.length; },
    () => undefined,
  );
  await coordinator.refresh();
  assert.equal(loads, 1);
  assert.equal(scheduled.length, 0, "an idle runtime list must not keep polling");
}

{
  let loads = 0;
  const scheduled: Array<() => void> = [];
  const coordinator = createBackgroundRuntimeRefreshCoordinator(
    async () => { loads += 1; return loads === 1 ? [runtime()] : []; },
    () => undefined,
    (callback) => { scheduled.push(callback); return scheduled.length; },
    () => undefined,
  );
  await coordinator.refresh();
  assert.equal(scheduled.length, 1, "active work schedules a bounded fallback refresh");
  scheduled[0]();
  await flushPromises();
  assert.equal(loads, 2);
}

{
  const first = deferred<BackgroundRuntimeView[]>();
  let loads = 0;
  const coordinator = createBackgroundRuntimeRefreshCoordinator(
    () => { loads += 1; return loads === 1 ? first.promise : Promise.resolve([]); },
    () => undefined,
    () => 1,
    () => undefined,
  );
  const initial = coordinator.refresh();
  const joined = coordinator.refresh();
  await Promise.resolve();
  assert.equal(loads, 1, "concurrent observations share one bridge request");
  first.resolve([runtime()]);
  await Promise.all([initial, joined]);
}

{
  const first = deferred<BackgroundRuntimeView[]>();
  let loads = 0;
  const coordinator = createBackgroundRuntimeRefreshCoordinator(
    () => { loads += 1; return loads === 1 ? first.promise : Promise.resolve([]); },
    () => undefined,
    () => 1,
    () => undefined,
  );
  const initial = coordinator.refresh();
  void coordinator.refresh({ afterMutation: true });
  await Promise.resolve();
  assert.equal(loads, 1);
  first.resolve([runtime()]);
  await initial;
  await flushPromises();
  assert.equal(loads, 2, "a mutation queues one authoritative trailing refresh");
}

{
  let calls = 0;
  const scheduled: Array<{ callback: () => void; delayMs: number }> = [];
  const coordinator = createBackgroundRuntimeRefreshCoordinator(
    async () => {
      calls += 1;
      if (calls === 1) throw new Error("temporary bridge failure");
      return [];
    },
    () => undefined,
    (callback, delayMs) => { scheduled.push({ callback, delayMs }); return scheduled.length; },
    () => undefined,
  );
  await assert.rejects(coordinator.refresh());
  assert.equal(scheduled.length, 1, "a failed active refresh must schedule a retry");
  assert.equal(scheduled[0].delayMs, BACKGROUND_RUNTIME_ERROR_RETRY_BASE_MS);
  scheduled[0].callback();
  await flushPromises();
  assert.equal(calls, 2);

  const pending = deferred<BackgroundRuntimeView[]>();
  let applied = 0;
  let disposedSchedules = 0;
  const disposedCoordinator = createBackgroundRuntimeRefreshCoordinator(
    () => pending.promise,
    () => { applied += 1; },
    () => { disposedSchedules += 1; return disposedSchedules; },
    () => undefined,
  );
  const request = disposedCoordinator.refresh();
  disposedCoordinator.dispose();
  pending.resolve([runtime()]);
  await request;
  await flushPromises();
  assert.equal(applied, 0, "disposed coordinators must ignore late bridge results");
  assert.equal(disposedSchedules, 0, "disposed coordinators must not schedule fallback polling");
}

console.log("background runtime refresh tests passed");
