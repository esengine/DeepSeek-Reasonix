import {
  TranscriptKernel,
  type TranscriptKernelClock,
  type TranscriptKernelEvent,
  type TranscriptViewportSnapshot,
  type TranscriptWriteRequest,
} from "../lib/transcriptKernel";
import { observeTranscriptGeometry } from "../lib/transcriptGeometryObserver";

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
  requestAnimationFrame = (callback: FrameRequestCallback) => {
    const id = ++this.sequence;
    this.frames.set(id, callback);
    return id;
  };
  cancelAnimationFrame = (id: number) => { this.frames.delete(id); };
  setTimeout = (callback: () => void, delay: number) => {
    const id = ++this.sequence;
    this.timers.set(id, { at: this.time + delay, callback });
    return id as unknown as ReturnType<typeof setTimeout>;
  };
  clearTimeout = (id: ReturnType<typeof setTimeout>) => { this.timers.delete(id as unknown as number); };
  flushFrames() {
    const frames = [...this.frames.values()];
    this.frames.clear();
    frames.forEach((callback) => callback(this.time));
  }
  advance(ms: number) {
    this.time += ms;
    const ready = [...this.timers].filter(([, timer]) => timer.at <= this.time);
    ready.forEach(([id, timer]) => {
      this.timers.delete(id);
      timer.callback();
    });
  }
}

const readerSnapshot = (key = "turn:visible", scrollTop = 500): TranscriptViewportSnapshot => ({
  scrollTop,
  scrollHeight: 4_000,
  clientHeight: 800,
  visibleBlocks: [{ key, top: scrollTop - 12, bottom: scrollTop + 120 }],
});

function setup(session = "race") {
  const clock = new FakeClock();
  const writes: TranscriptWriteRequest[] = [];
  const events: TranscriptKernelEvent[] = [];
  const kernel = new TranscriptKernel({ clock, emit: (event) => events.push(event) });
  kernel.connectWriter((request) => {
    writes.push(request);
    return { accepted: true, offset: Number.isFinite(request.offset) ? request.offset : 3_200, changed: true };
  });
  kernel.replaceSurface(session);
  return { clock, events, kernel, writes };
}

console.log("\nTranscriptKernel deterministic race matrix");

{
  const { clock, kernel, writes } = setup("observer-A");
  let notify!: () => void;
  let before = 0, commits = 0;
  const disconnect = observeTranscriptGeometry(kernel, {} as Element,
    () => { before += 1; }, () => { commits += 1; kernel.advanceGeometry(); },
    (callback) => { notify = callback; return { observe() {}, disconnect() {} }; });
  notify();
  const queued = [...clock.frames.values()];
  disconnect();
  kernel.replaceSurface("observer-B");
  notify(); // A platform can deliver notifications even after disconnect.
  queued.forEach((callback) => callback(0));
  clock.flushFrames();
  ok(before === 1 && commits === 0 && kernel.geometryRevision === 0 && writes.length === 0,
    "detached observer and already queued frame cannot mutate replacement geometry");
  let painted = false;
  const cancel = kernel.afterCurrentGenerationPaint(() => { painted = true; });
  const cancelledFrame = [...clock.frames.values()][0];
  cancel();
  cancelledFrame(0);
  ok(!painted, "a cancelled surface-paint callback is inert even when delivered in the same generation");
  kernel.renewNativeGesture(readerSnapshot(), 320, () => {});
  const staleTimer = [...clock.timers.values()][0].callback;
  kernel.replaceSurface("observer-C");
  kernel.renewNativeGesture(readerSnapshot(), 320, () => {});
  staleTimer();
  ok(kernel.nativeGestureLeaseActive && kernel.userGestureActive,
    "an old native timer cannot release the new generation's lease");
}

{
  const { clock, kernel, writes } = setup("stream-display");
  kernel.scheduleTailSync();
  const display = kernel.begin("display-change", { kind: "block", blockKey: "turn:4", offsetPx: 3 });
  kernel.advanceGeometry();
  ok(Boolean(display && kernel.correctAnchor(display, () => 700)), "stream growth × display change commits from the newest geometry");
  clock.flushFrames();
  ok(writes.length === 1 && writes[0]?.offset === 703, "display change cancels its queued tail write and performs one correction");
}

{
  const { kernel, writes } = setup("display-gesture");
  const display = kernel.begin("display-change", { kind: "block", blockKey: "turn:4", offsetPx: 0 });
  kernel.beginUserGesture(readerSnapshot());
  kernel.advanceGeometry();
  ok(display?.status === "cancelled" && !kernel.correctAnchor(display!, () => 800), "display change × wheel/touch/thumb gives ownership to the user");
  ok(writes.length === 0, "a held native gesture accepts zero programmatic writes");
}

{
  const { kernel, writes } = setup("prepend-selection");
  kernel.observeNativeScroll(readerSnapshot());
  const prepend = kernel.begin("prepend", kernel.anchor);
  kernel.beginUserGesture(readerSnapshot(), "selection");
  kernel.advanceGeometry();
  ok(prepend?.status === "cancelled" && !kernel.correctAnchor(prepend!, () => 900), "prepend × selection cancels the structural correction");
  kernel.writeUserControlled("selection-edge-scroll", 520);
  ok(writes.length === 1 && writes[0]?.owner === "selection-edge-scroll", "selection keeps only its explicit edge-scroll write");
  kernel.endUserGesture();
  ok(kernel.activeTransaction === null, "selection reaches a terminal state when the gesture ends");
}

{
  const { kernel, writes } = setup("prepend-during-gesture");
  const snapshot = readerSnapshot("turn:prepend-anchor");
  kernel.beginUserGesture(snapshot, "selection");
  const deferred = kernel.begin("prepend", kernel.anchor);
  ok(deferred === null && writes.length === 0, "prepend requested during a gesture captures intent without writing");
  const resumed = kernel.endUserGesture();
  kernel.advanceGeometry();
  kernel.correctAnchor(resumed!, () => 1_400);
  ok(resumed?.status === "committed" && writes[0]?.offset === 1_412, "gesture release resumes prepend from the pre-mutation logical anchor");
}

{
  const { kernel, writes } = setup("prepend-display");
  kernel.observeNativeScroll(readerSnapshot("turn:stable"));
  const prepend = kernel.begin("prepend", kernel.anchor);
  const display = kernel.begin("display-change", kernel.anchor);
  kernel.advanceGeometry();
  kernel.correctAnchor(display!, () => 880);
  ok(prepend?.status === "cancelled" && display?.status === "committed", "prepend × display change deterministically selects the latest equal-priority transaction");
  ok(writes.length === 1 && writes[0]?.offset === 892, "the surviving transaction preserves the reader's in-block offset");
}

{
  const { clock, kernel, writes } = setup("composer-tail");
  kernel.scheduleTailSync();
  const composer = kernel.begin("composer-resize", { kind: "tail" });
  kernel.advanceGeometry();
  kernel.correctAnchor(composer!, () => undefined);
  clock.flushFrames();
  ok(composer?.status === "committed" && writes.length === 1, "Composer resize × tail follow submits one tail correction");
}

{
  const { kernel, writes } = setup("jump-switch");
  const jump = kernel.stageJumpToBlock("turn:900");
  kernel.replaceSurface("jump-destination");
  kernel.advanceGeometry();
  ok(jump?.status === "cancelled" && !kernel.correctAnchor(jump!, () => 12_000), "question jump × session switch fences the old generation");
  ok(writes.length === 0, "a stale question jump performs zero writes");
}

{
  const { clock, kernel, writes } = setup("turn-completion");
  kernel.scheduleTailSync();
  kernel.scheduleTailSync();
  clock.flushFrames();
  ok(writes.length === 1 && writes[0]?.generation === kernel.generation, "active completion × next-round start coalesces to one current-generation tail write");
}

{
  const { kernel, writes } = setup("lazy-measure");
  kernel.observeNativeScroll(readerSnapshot("turn:markdown"));
  const restore = kernel.begin("restore", kernel.anchor);
  kernel.advanceGeometry();
  ok(!kernel.correctAnchor(restore!, () => undefined), "lazy Markdown/image/table measurement defers when the anchor is unmeasured");
  ok(!kernel.correctAnchor(restore!, () => 910), "one geometry revision accepts at most one structural correction attempt");
  kernel.advanceGeometry();
  ok(kernel.correctAnchor(restore!, () => 910), "the latest measured geometry retries the logical reader anchor once");
  ok(writes[0]?.offset === 922, "lazy content restores the exact block-local reader offset");
  kernel.observeNativeScroll(readerSnapshot("turn:wrong", 922));
  ok(kernel.anchor.kind === "block" && kernel.anchor.blockKey === "turn:markdown", "writer scroll events cannot replace the structural logical anchor");
  kernel.beginUserGesture(readerSnapshot("turn:user", 940));
  kernel.endUserGesture();
  ok(kernel.anchor.kind === "block" && kernel.anchor.blockKey === "turn:user", "the next native scroll records the user's actual reader anchor");
}

{
  const { kernel } = setup("gesture-anchor-ownership");
  kernel.observeNativeScroll(readerSnapshot("turn:reader", 500));
  kernel.beginUserGesture(readerSnapshot("turn:reader", 500));
  kernel.endUserGesture();
  ok(kernel.anchor.kind === "block" && kernel.anchor.blockKey === "turn:reader", "measurement-only gesture completion preserves the pre-measurement logical anchor");
  kernel.beginUserGesture(readerSnapshot("turn:reader", 500));
  kernel.observeNativeScroll(readerSnapshot("turn:moved", 620));
  kernel.endUserGesture();
  ok(kernel.anchor.kind === "block" && kernel.anchor.blockKey === "turn:moved", "a changed native position commits the gesture's final logical anchor");
}

{
  const { kernel, writes } = setup("reduced-motion");
  const restore = kernel.begin("restore", { kind: "block", blockKey: "turn:old", offsetPx: 0 });
  kernel.replaceSurface("reduced-motion-replacement");
  kernel.advanceGeometry();
  ok(restore?.status === "cancelled" && writes.length === 0, "reduced-motion × surface replacement keeps the same generation fence and zero-write path");
}

{
  const { kernel, events } = setup("safe-mode");
  kernel.reportAnomaly("blank-viewport");
  kernel.reportAnomaly("invalid-geometry");
  ok(kernel.safeMode, "two consecutive geometry anomalies activate full-DOM safe mode");
  ok(events.filter((event) => event.outcome === "blank-viewport" || event.outcome === "invalid-geometry").length === 2, "safe-mode anomalies remain numeric/enumerated diagnostics");
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed) process.exit(1);
