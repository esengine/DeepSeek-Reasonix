import assert from "node:assert/strict";
import { startDesktopEventRecovery } from "../lib/desktopEventRecovery";

const tick = () => new Promise(resolve => setImmediate(resolve));
let signal!: () => void;
let selected = "task-a", generation = 1;
const pending: Array<(value: string) => void> = [];
const applied: string[] = [];
const timers = new Map<number, () => void>();
let timerID = 0;
const stop = startDesktopEventRecovery({
  subscribe: callback => { signal = callback; return () => {}; },
  capture: () => ({ selected, generation }),
  isCurrent: scope => scope.selected === selected && scope.generation === generation,
  read: () => new Promise<string>(resolve => pending.push(resolve)),
  apply: (snapshot, scope) => applied.push(`${scope.selected}:${scope.generation}:${snapshot}`),
  timer: callback => { const id = ++timerID; timers.set(id, callback); return id; },
  clearTimer: id => { timers.delete(id as number); },
});
signal(); signal();
assert.equal(pending.length, 1, "gaps share one in-flight snapshot read");
pending.shift()!("obsolete"); await tick();
assert.deepEqual(applied, [], "a new recovery signal fences the previous snapshot");
assert.equal(pending.length, 1);
selected = "task-b";
pending.shift()!("old-selection"); await tick();
assert.deepEqual(applied, [], "switching tasks prevents late snapshot application");
assert.equal(timers.size, 1, "a superseded selection queues fresh recovery");
[...timers.values()][0]();
generation++;
signal();
pending.shift()!("old-service"); await tick();
pending.shift()!("current"); await tick();
assert.deepEqual(applied, ["task-b:2:current"]);
signal(); stop(); pending.shift()!("disposed"); await tick();
assert.equal(applied.length, 1);
assert.equal(timers.size, 0);
console.log("desktop event recovery: coalescing, generation/session fencing, retry and disposal passed");
