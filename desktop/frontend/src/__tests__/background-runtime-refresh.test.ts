import assert from "node:assert/strict";
import {
  BACKGROUND_RUNTIME_ACTIVE_REFRESH_MS,
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
assert.equal(sameBackgroundRuntimeLists([runtime()], [runtime()]), true);
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

console.log("background runtime refresh tests passed");
