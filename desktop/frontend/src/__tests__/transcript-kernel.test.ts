import { TranscriptKernel, type TranscriptKernelClock, type TranscriptKernelEvent } from "../lib/transcriptKernel";

let passed = 0;
let failed = 0;
function ok(condition: unknown, label: string) {
  if (condition) { process.stdout.write(`  PASS  ${label}\n`); passed += 1; }
  else { process.stdout.write(`  FAIL  ${label}\n`); failed += 1; }
}

class FakeClock implements TranscriptKernelClock {
  time = 0;
  sequence = 0;
  frames = new Map<number, FrameRequestCallback>();
  timers = new Map<number, { at: number; callback: () => void }>();
  now = () => this.time;
  requestAnimationFrame = (callback: FrameRequestCallback) => { const id = ++this.sequence; this.frames.set(id, callback); return id; };
  cancelAnimationFrame = (id: number) => { this.frames.delete(id); };
  setTimeout = (callback: () => void, delay: number) => { const id = ++this.sequence; this.timers.set(id, { at: this.time + delay, callback }); return id as unknown as ReturnType<typeof setTimeout>; };
  clearTimeout = (id: ReturnType<typeof setTimeout>) => { this.timers.delete(id as unknown as number); };
  flushFrames() { const frames = [...this.frames.values()]; this.frames.clear(); frames.forEach((callback) => callback(this.time)); }
  advance(ms: number) {
    this.time += ms;
    const ready = [...this.timers].filter(([, timer]) => timer.at <= this.time);
    ready.forEach(([id, timer]) => { this.timers.delete(id); timer.callback(); });
  }
}

console.log("\nTranscriptKernel deterministic transactions");
const clock = new FakeClock();
const events: TranscriptKernelEvent[] = [];
const writes: Array<{ generation: number; transactionId: number; offset: number; owner: string }> = [];
const kernel = new TranscriptKernel({ clock, emit: (event) => events.push(event) });
kernel.connectWriter((request) => {
  writes.push(request);
  return { accepted: true, offset: Number.isFinite(request.offset) ? request.offset : 900, changed: true };
});

kernel.replaceSurface("one");
const restore = kernel.begin("restore", { kind: "block", blockKey: "turn:2", offsetPx: 7 });
ok(Boolean(restore), "restore transaction begins in the current generation");
kernel.advanceGeometry();
ok(Boolean(restore && kernel.correctAnchor(restore, () => 120)), "logical block anchor commits one correction");
ok(writes[writes.length - 1]?.offset === 127, "block correction preserves its in-block offset");
ok(restore?.status === "committed", "accepted correction reaches a terminal committed state");

const display = kernel.begin("display-change", { kind: "block", blockKey: "turn:2", offsetPx: 7 });
const lowerTail = kernel.begin("tail-sync");
ok(lowerTail === null, "tail follow cannot supersede display change");
const jump = kernel.begin("jump", { kind: "block", blockKey: "turn:9", offsetPx: 0 });
ok(display?.status === "cancelled" && jump?.status === "active", "question jump supersedes lower-priority display work");

const snapshot = {
  scrollTop: 200, scrollHeight: 2_000, clientHeight: 500,
  visibleBlocks: [{ key: "turn:4", top: 180, bottom: 280 }],
};
kernel.beginUserGesture(snapshot);
ok(jump?.status === "cancelled" && kernel.intent === "reader", "native user intent cancels an active jump and owns reader intent");
const countBeforeGesture = writes.length;
kernel.scheduleTailSync();
clock.flushFrames();
ok(writes.length === countBeforeGesture, "reader gesture accepts zero tail writes");
kernel.endUserGesture();

kernel.scrollToTail();
const writesBeforeStaleFrame = writes.length;
kernel.scheduleTailSync();
kernel.replaceSurface("two");
clock.flushFrames();
ok(writes.length === writesBeforeStaleFrame, "a queued callback from an expired generation performs zero writes");

kernel.scheduleTailSync();
const writesBeforeDetach = writes.length;
kernel.detachSurface();
clock.flushFrames();
ok(writes.length === writesBeforeDetach, "a queued callback from an unmounted surface performs zero writes");

const expiring = kernel.begin("prepend", { kind: "block", blockKey: "missing", offsetPx: 0 });
clock.advance(1_000);
ok(expiring?.status === "expired", "a transaction that cannot settle expires deterministically at 1000ms");
ok(events.some((event) => event.transaction === expiring?.id && event.outcome === "deadline"), "expiry emits an explicit terminal outcome");

kernel.reportAnomaly("blank-viewport");
ok(!kernel.safeMode, "one anomalous frame does not downgrade the session");
kernel.reportHealthyGeometry();
kernel.reportAnomaly("invalid-geometry");
ok(!kernel.safeMode, "a healthy frame resets the consecutive anomaly streak");
kernel.reportAnomaly("blank-viewport");
ok(kernel.safeMode, "two consecutive anomalies downgrade only the current generation");
kernel.replaceSurface("three");
ok(!kernel.safeMode, "surface generation replacement clears safe mode");

let leaseEnded = 0;
kernel.renewNativeGesture(snapshot, 320, () => { leaseEnded += 1; });
ok(kernel.userGestureActive && kernel.nativeGestureLeaseActive, "native input starts one kernel-owned gesture lease");
clock.advance(319);
ok(leaseEnded === 0 && kernel.userGestureActive, "the injected clock keeps native ownership until the lease expires");
kernel.renewNativeGesture({ ...snapshot, scrollTop: 260 }, 320, () => { leaseEnded += 1; });
clock.advance(319);
ok(leaseEnded === 0, "renewing native input replaces rather than stacks lease timers");
clock.advance(1);
ok(leaseEnded === 1 && !kernel.userGestureActive && !kernel.nativeGestureLeaseActive, "the current generation ends the gesture exactly once");

kernel.renewNativeGesture(snapshot, 320, () => { leaseEnded += 1; });
kernel.replaceSurface("four");
clock.advance(320);
ok(leaseEnded === 1 && !kernel.userGestureActive, "surface replacement cancels stale gesture callbacks");

let painted = 0;
kernel.afterCurrentGenerationPaint(() => { painted += 1; });
kernel.replaceSurface("five");
clock.flushFrames();
ok(painted === 0, "surface replacement cancels stale paint callbacks");
kernel.afterCurrentGenerationPaint(() => { painted += 1; });
clock.flushFrames();
ok(painted === 1, "the current generation accepts its paint callback");

kernel.scrollToTail();
const delayedWriterOffset = 900;
kernel.beginUserGesture({
  ...snapshot,
  scrollTop: delayedWriterOffset,
  visibleBlocks: [{ key: "turn:writer-target", top: delayedWriterOffset, bottom: delayedWriterOffset + 120 }],
});
const delayedWriterIsNative = kernel.observeNativeScroll({
  ...snapshot,
  scrollTop: delayedWriterOffset,
  visibleBlocks: [{ key: "turn:writer-target", top: delayedWriterOffset, bottom: delayedWriterOffset + 120 }],
});
ok(!delayedWriterIsNative, "a delayed writer scroll keeps its provenance after user ownership begins");
const movedNativeIsNative = kernel.observeNativeScroll({
  ...snapshot,
  scrollTop: delayedWriterOffset - 80,
  visibleBlocks: [{ key: "turn:user-position", top: delayedWriterOffset - 90, bottom: delayedWriterOffset + 30 }],
});
ok(movedNativeIsNative, "a physical offset that diverges from the writer target belongs to the user");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed) process.exit(1);
