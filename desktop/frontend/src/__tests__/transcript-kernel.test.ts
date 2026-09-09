import { commitTranscriptWindowGeometry } from "../lib/transcriptWindowGeometry";
import { TranscriptMeasurementLedger } from "../lib/transcriptMeasurementLedger";
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

{
  const clock = new FakeClock();
  const kernel = new TranscriptKernel({ clock });
  const transactions: number[] = [];
  kernel.connectWriter(request => {
    transactions.push(request.transactionId);
    return { accepted: true, offset: request.offset, changed: true };
  });
  kernel.replaceSurface("prepend");
  const transaction = kernel.begin("prepend", { kind: "block", blockKey: "old-first", offsetPx: 7 })!;
  for (const top of [100, 180, 238]) {
    kernel.advanceGeometry();
    kernel.correctAnchor(transaction, () => top);
    kernel.correctAnchor(transaction, () => top);
  }
  ok(transactions.length === 3 && new Set(transactions).size === 1, "prepend batches share ownership and deduplicate each geometry revision");
  ok(kernel.activeTransaction === transaction, "prepend remains active until the layout batch settles");
  clock.flushFrames();
  ok(transaction.status === "committed" && kernel.activeTransaction === null, "prepend commits after its final layout batch");
  const replaced = kernel.begin("prepend", { kind: "block", blockKey: "old-first", offsetPx: 7 })!;
  kernel.advanceGeometry();
  kernel.correctAnchor(replaced, () => 300);
  kernel.replaceSurface("next");
  clock.flushFrames();
  ok(replaced.status === "cancelled", "surface replacement cancels pending prepend settlement");
  const interrupted = kernel.begin("prepend", { kind: "block", blockKey: "new-first", offsetPx: 0 })!;
  kernel.advanceGeometry();
  kernel.correctAnchor(interrupted, () => 300);
  const beforeInput = transactions.length;
  kernel.beginUserGesture({ scrollTop: 300, scrollHeight: 2000, clientHeight: 600, visibleBlocks: [{ key: "new-first", top: 300, bottom: 500 }] });
  clock.flushFrames();
  kernel.advanceGeometry();
  kernel.correctAnchor(interrupted, () => 400);
  ok(interrupted.status === "cancelled" && transactions.length === beforeInput, "user input cancels prepend settlement without another scroll write");
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

// Deferred DOM growth is reconciled after native release, preserving the
// original logical anchor while allowing the following block to move.
const measured = new TranscriptMeasurementLedger();
measured.commit([{ key: "before", size: 100 }, { key: "turn:4", size: 100 }]);
kernel.beginUserGesture(snapshot);
measured.stage([{ key: "before", size: 180 }, { key: "turn:4", size: 340 }]);
const heldWrites = writes.length;
measured.publishStaged(() => !kernel.userGestureActive);
ok(measured.sizeFor("turn:4", 0) === 100 && writes.length === heldWrites, "held growth remains staged with zero correction writes");
kernel.endUserGesture();
const reconciliation = kernel.begin("restore", kernel.anchor);
measured.publishStaged();
kernel.advanceGeometry();
const newAnchorTop = snapshot.visibleBlocks[0].top + measured.sizeFor("before", 0) - 100;
if (reconciliation) kernel.correctAnchor(reconciliation, () => newAnchorTop);
ok(writes[writes.length - 1]?.offset === 280, "release corrects only the changed prefix and retains the reader's 20px in-block offset");
ok(newAnchorTop + measured.sizeFor("turn:4", 0) === 600, "the following block advances past all expanded content");
const settledWrites = writes.length;
if (reconciliation) kernel.correctAnchor(reconciliation, () => newAnchorTop);
ok(writes.length === settledWrites, "one geometry reconciliation cannot emit duplicate corrections");

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

// The adapter closes a safe size batch while the kernel still owns native input.
const nativeBatchWrites = writes.length;
kernel.renewNativeGesture(snapshot, 320, () => {});
const batchItems = Array.from({ length: 50 }, (_, index) => ({
  index, key: `batch:${index}`, start: index * 100, end: (index + 1) * 100, size: 100,
}));
const batchInput = { candidate: batchItems.slice(0, 38), measurements: batchItems,
  retainedIndexes: new Set<number>(), structureRevision: "batch", scrollTop: 200, clientHeight: 500,
  scrollMargin: 0, totalSize: 5000, maxItems: 38, direction: "forward" as const,
  gestureActive: kernel.userGestureActive, residentCount: 2, forceFull: false };
const batchBefore = commitTranscriptWindowGeometry(batchInput);
const measuredBatch = batchItems.map(item => ({ ...item, size: item.size + (item.index === 20 ? 24 : 0),
  start: item.start + (item.index > 20 ? 24 : 0), end: item.end + (item.index >= 20 ? 24 : 0) }));
const batchAfter = commitTranscriptWindowGeometry({ ...batchInput, candidate: measuredBatch.slice(0, 38),
  measurements: measuredBatch, totalSize: 5024, previous: batchBefore, measurementCommit: true });
kernel.advanceGeometry();
clock.flushFrames();
ok(batchAfter.prefix.items[21].start === 2124 && batchAfter.prefix.items[2].start === 200,
  "a published future batch is painted without moving the kernel reader anchor");
ok(kernel.userGestureActive && writes.length === nativeBatchWrites,
  "before-paint measurement acknowledgement neither releases native ownership nor writes scroll");
kernel.replaceSurface("batch-replaced");
clock.advance(320);
ok(writes.length === nativeBatchWrites, "batch completion cannot restore a replaced surface");

// Browser geometry notifications are not new user input.
kernel.scrollToTail();
const noInputChangedIntent = kernel.observeNativeScroll({ ...snapshot, scrollTop: 905, scrollHeight: 1460 });
ok(!noInputChangedIntent && kernel.intent === "tail", "a delayed layout scroll cannot revoke tail intent without an input owner");
const renewalSnapshot = { ...snapshot, scrollTop: 600, visibleBlocks: [{ key: "renewal", top: 580, bottom: 900 }] };
kernel.beginUserGesture(renewalSnapshot);
kernel.renewNativeGesture({ ...renewalSnapshot, scrollTop: 640 }, 320, () => {});
ok(kernel.anchor.kind === "block" && kernel.anchor.offsetPx === 20,
  "renewing input ownership does not invent a native scroll observation");
kernel.observeNativeScroll({ ...renewalSnapshot, scrollTop: 640 });
ok(kernel.anchor.kind === "block" && kernel.anchor.offsetPx === 60,
  "the subsequent native event records actual user movement");
kernel.endUserGesture();

let firstTailWrite = true;
kernel.connectWriter(() => {
  const changed = firstTailWrite; firstTailWrite = false;
  return { accepted: true, offset: 900, changed };
});
kernel.scrollToTail();
kernel.scrollToTail(); // Geometry may request an idempotent sync before scroll delivery.
kernel.beginUserGesture({ ...snapshot, scrollTop: 900 });
kernel.renewNativeGesture({ ...snapshot, scrollTop: 900 }, 320, () => {});
ok(!kernel.observeNativeScroll({ ...snapshot, scrollTop: 900 }),
  "no-op sync and lease renewal preserve the pending writer event provenance");
kernel.endUserGesture();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed) process.exit(1);
