// Run: tsx src/__tests__/heartbeat-mutation-queue.test.ts

import { JSDOM } from "jsdom";
import type { HeartbeatTask } from "../custom/features/heartbeat/heartbeat.types";

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function flush(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

const dom = new JSDOM("<!doctype html><html><body></body></html>", { url: "http://localhost/" });
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;

type ConfigUpdate = { revision: number; etag: string; tasks: HeartbeatTask[] };
type SaveCall = ConfigUpdate & { resolve: (view: ConfigUpdate) => void };

const initial: HeartbeatTask[] = [
  { id: "a", title: "A", prompt: "a", interval: "1h", enabled: true },
  { id: "b", title: "B", prompt: "b", interval: "1h", enabled: true },
];
const saveCalls: SaveCall[] = [];

Object.assign(window, {
  go: {
    main: {
      App: {
        async HeartbeatReloadConfig() {
          return { revision: 1, etag: "etag-1", tasks: initial };
        },
        HeartbeatSaveConfig(update: ConfigUpdate) {
          return new Promise<ConfigUpdate>((resolve) => saveCalls.push({ ...update, resolve }));
        },
      },
    },
  },
});

const { heartbeatListTasks, heartbeatMutateTasks } = await import("../custom/features/heartbeat/heartbeat.bridge");

console.log("\nheartbeat mutation queue");

await heartbeatListTasks();
const toggle = heartbeatMutateTasks((tasks) => tasks.map((task) => (
  task.id === "a" ? { ...task, enabled: !task.enabled } : task
)));
const remove = heartbeatMutateTasks((tasks) => tasks.filter((task) => task.id !== "b"));
await flush();

ok(saveCalls.length === 1, "only one full-table mutation is in flight");
ok(saveCalls[0]?.revision === 1 && saveCalls[0]?.etag === "etag-1", "first mutation uses the loaded config token");
const firstTasks = saveCalls[0]?.tasks ?? [];
saveCalls[0]?.resolve({ revision: 2, etag: "etag-2", tasks: firstTasks });
await flush();

ok(saveCalls.length === 2, "second mutation starts after the first save settles");
ok(saveCalls[1]?.revision === 2 && saveCalls[1]?.etag === "etag-2", "second mutation uses the new config token");
ok(saveCalls[1]?.tasks.length === 1 && saveCalls[1]?.tasks[0]?.id === "a", "second mutation rebases deletion onto the saved task list");
ok(saveCalls[1]?.tasks[0]?.enabled === false, "rebased deletion preserves the earlier toggle");
const secondTasks = saveCalls[1]?.tasks ?? [];
saveCalls[1]?.resolve({ revision: 3, etag: "etag-3", tasks: secondTasks });

const [afterToggle, afterDelete] = await Promise.all([toggle, remove]);
ok(afterToggle.find((task) => task.id === "a")?.enabled === false, "first caller receives its authoritative save");
ok(afterDelete.length === 1 && afterDelete[0]?.id === "a", "second caller receives the combined authoritative state");

dom.window.close();
console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
