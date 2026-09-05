import assert from "node:assert/strict";
import { createSubscriptionScope } from "../lib/subscriptionScope";

const effects: string[] = [];
const queued: Array<() => void> = [];
let subscriptions = 0;
const scope = createSubscriptionScope((delta) => { subscriptions += delta; });
scope.listen((listener: () => void) => {
  queued.push(listener);
  return () => { queued[1](); throw new Error("first unsubscribe failed"); };
}, () => { effects.push("old-first"); });
scope.listen((listener: () => void) => {
  queued.push(listener);
  return () => { effects.push("second-released"); };
}, () => { effects.push("old-second"); });
assert.equal(subscriptions, 2);
assert.throws(() => scope.dispose(), /first unsubscribe failed/);
assert.equal(scope.size, 0);
assert.equal(subscriptions, 0, "all subscriptions release even if one source cleanup fails");
assert.deepEqual(effects, ["second-released"], "cleanup revokes all listeners before touching the source");
const replacement = createSubscriptionScope();
replacement.listen((listener: () => void) => { queued.push(listener); return () => {}; }, () => { effects.push("new"); });
for (const listener of queued) listener();
assert.deepEqual(effects, ["second-released", "new"], "queued old notifications cannot acquire replacement rights");
scope.dispose();
assert.equal(subscriptions, 0, "repeated disposal cannot decrement accounting twice");
replacement.dispose();
const synchronous = createSubscriptionScope((delta) => { subscriptions += delta; });
synchronous.listen((listener: () => void) => { listener(); return () => {}; }, () => synchronous.dispose());
assert.equal(synchronous.size, 0);
assert.equal(subscriptions, 0, "disposal during synchronous registration cannot leak a lease");
console.log("PASS subscription scopes fence queued notifications and release all resources");
