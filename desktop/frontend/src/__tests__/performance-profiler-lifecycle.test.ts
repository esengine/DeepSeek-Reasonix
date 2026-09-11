import assert from "node:assert/strict";
import { installPerformancePressureMonitor } from "../lib/crash";
import { installDesktopHostStub } from "./desktopHostStub";

const events = new Map<string, () => void>();
let focused = true;
let starts = 0;
let cancellations = 0;
Object.assign(globalThis, {
  window: { addEventListener: (name: string, cb: () => void) => events.set(name, cb), setInterval: () => 1 },
  document: { visibilityState: "visible", hasFocus: () => focused, addEventListener: (name: string, cb: () => void) => events.set(name, cb) },
  Profiler: class {
    constructor() { starts++; }
  },
});
installDesktopHostStub({}, { performance: { cancelRendererProfile: async () => { cancellations++; } } });
installPerformancePressureMonitor();
assert.equal(starts, 0, "lightweight monitoring never starts a JS profiler");
focused = false;
events.get("blur")!();
assert.equal(cancellations, 0, "no cancellation is sent without an owned capture");
focused = true;
events.get("focus")!();
events.get("focus")!();
assert.equal(starts, 0, "refocusing does not start a profiler");
Object.assign(document, { visibilityState: "hidden" });
events.get("visibilitychange")!();
assert.equal(cancellations, 0);
console.log("profiler lifecycle tests passed");
