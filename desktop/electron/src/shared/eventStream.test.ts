import assert from "node:assert/strict";
import { test } from "node:test";
import { DesktopEventStream, MissedEventSubscriptions, type EventRecovery } from "./eventStream.js";
import type { EventFrame } from "./ipc.js";

test("missed dynamic subscriptions have bounded retention and overflow still requests recovery", () => {
  const missed = new MissedEventSubscriptions();
  missed.add("agent:event");
  assert.equal(missed.consume("agent:event"), true);
  assert.equal(missed.consume("agent:event"), false, "normal recovery consumes the pending name");
  for (let index = 0; index < 10000; index++) {
    missed.add(`remote-tab:closed-${index}:event`);
    assert.ok(missed.size <= 64);
  }
  assert.equal(missed.size, 0, "overflow retains no closed-tab event names");
  assert.equal(missed.consume("remote-tab:closed-0:event"), true, "an evicted name still triggers read-side repair");
  assert.equal(missed.consume("remote-tab:closed-9999:event"), true);
  missed.clear();
  assert.equal(missed.consume("remote-tab:closed-0:event"), false, "a new generation drops old pending state");
  missed.add("agent:event");
  assert.equal(missed.size, 1);
});

test("transport rejects stale generations and duplicate/out-of-order frames and reports sequence gaps", () => {
  const accepted: EventFrame[] = [], repairs: EventRecovery[] = [];
  const stream = new DesktopEventStream(frame => accepted.push(frame), event => repairs.push(event));
  const frame = (seq: number, generation = "g1", name = "agent:event") => ({ seq, generation, name, args: [seq] });
  assert.equal(stream.accept(frame(1)), false, "unnegotiated generations cannot enter the renderer");
  stream.observeState({ phase: "ready", generation: "g1" });
  assert.equal(stream.accept(frame(1)), true);
  assert.equal(stream.accept(frame(1)), false);
  assert.equal(stream.accept(frame(3)), true);
  assert.equal(stream.accept(frame(2)), false);
  assert.equal(stream.accept(frame(4, "old")), false);
  assert.equal(stream.accept(frame(Number.NaN)), false);
  assert.equal(stream.accept(frame(0)), false);
  assert.equal(stream.accept(frame(0, "g1", "app:open-settings")), true, "shell events have their own sequence-free namespace");
  assert.deepEqual(repairs.map(({ reason, expectedSeq, actualSeq }) => ({ reason, expectedSeq, actualSeq })), [
    { reason: "generation", expectedSeq: 1, actualSeq: 0 },
    { reason: "gap", expectedSeq: 2, actualSeq: 3 },
  ]);
  stream.observeState({ phase: "restarting", generation: "" });
  assert.equal(stream.accept(frame(4)), false);
  stream.observeState({ phase: "ready", generation: "g2" });
  assert.equal(stream.accept(frame(5)), false, "queued frames from the previous service stay rejected");
  assert.equal(stream.accept(frame(1, "g2")), true);
  assert.deepEqual(accepted.map(value => `${value.generation}:${value.seq}`), ["g1:1", "g1:3", "g1:0", "g2:1"]);
  stream.requestRecovery("subscription");
  assert.equal(stream.recovery?.reason, "subscription");
  assert.equal(stream.recovery?.generation, "g2");
});
