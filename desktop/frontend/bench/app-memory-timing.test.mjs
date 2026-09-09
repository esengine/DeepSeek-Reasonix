import assert from "node:assert/strict";
import test from "node:test";
import { createTimings } from "./app-memory-timing.mjs";

test("timings count successful and failed waits without changing their results", async () => {
  let clock = 0;
  const timings = createTimings(() => clock);
  assert.equal(await timings.measure("click", () => { clock += 3; return 42; }), 42);
  const failure = new Error("navigation failed");
  await assert.rejects(timings.measure("click", () => { clock += 7; throw failure; }), error => error === failure);
  assert.deepEqual(timings.snapshot(), { click: { count: 2, totalMs: 10, maxMs: 7 } });
});
