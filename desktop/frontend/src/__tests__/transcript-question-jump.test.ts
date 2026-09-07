import { TranscriptKernel, type TranscriptKernelClock } from "../lib/transcriptKernel";
import { TranscriptNavigation } from "../lib/transcriptNavigation";
import { TranscriptHistoryRequest } from "../lib/transcriptHistoryRequest";

let passed = 0;
let failed = 0;
function ok(value: unknown, label: string) {
  if (value) { process.stdout.write(`  PASS  ${label}\n`); passed += 1; }
  else { process.stdout.write(`  FAIL  ${label}\n`); failed += 1; }
}
const frames = new Map<number, FrameRequestCallback>();
let sequence = 0;
const clock: TranscriptKernelClock = {
  now: () => 0,
  requestAnimationFrame: (callback) => { const id = ++sequence; frames.set(id, callback); return id; },
  cancelAnimationFrame: (id) => { frames.delete(id); },
  setTimeout: () => ++sequence as unknown as ReturnType<typeof setTimeout>,
  clearTimeout: () => {},
};

console.log("\nTranscript question jump transaction");
const writes: number[] = [];
const kernel = new TranscriptKernel({ clock });
kernel.connectWriter((request) => { writes.push(request.offset); return { accepted: true, offset: request.offset, changed: true }; });
kernel.replaceSurface("one");
const jump = kernel.stageJumpToBlock("turn:500");
ok(jump?.status === "active", "an unmounted question starts a transaction while its block is pinned");
ok(writes.length === 0, "window mounting does not perform an estimated physical write");
kernel.advanceGeometry();
ok(Boolean(jump && kernel.correctAnchor(jump, () => 12_120)), "painted target receives its single exact logical-anchor write");
ok(jump?.status === "committed", "the question jump reaches a terminal state after paint");
const writesBeforeSwitch = writes.length;
const stale = kernel.stageJumpToBlock("turn:old");
kernel.replaceSurface("two");
kernel.advanceGeometry();
ok(stale?.status === "cancelled", "session switch cancels an old question jump");
ok(writes.length === writesBeforeSwitch, "the old generation cannot write into the replacement surface");

function deferred() {
  let resolve!: (value: boolean) => void;
  const promise = new Promise<boolean>((done) => { resolve = done; });
  return { promise, resolve };
}
const navigation = new TranscriptNavigation(kernel);
const history = new TranscriptHistoryRequest(kernel);
const question = { id: "unloaded", text: "", turn: 4 };
const positioning = navigation.start(question);
const staged = kernel.stageJumpToBlock("turn:unmounted")!;
navigation.locate(positioning, () => {});
ok(navigation.current?.status === "locating", "navigation remains pending while its target mounts");
kernel.advanceGeometry();
kernel.correctAnchor(staged, () => 250);
ok(navigation.current === null && staged.status === "committed", "only the positioned transaction completes navigation");
const snapshot = { scrollTop: 50, scrollHeight: 1000, clientHeight: 500, visibleBlocks: [] };
for (const gesture of ["wheel", "touch", "thumb", "selection"] as const) {
  const request = navigation.start(question);
  const data = deferred();
  const loading = history.load(() => data.promise);
  kernel.beginUserGesture(snapshot, gesture === "selection" ? "selection" : "native");
  kernel.endUserGesture();
  data.resolve(true);
  ok(await loading, `${gesture}: valid source data may finish loading`);
  ok(!navigation.owns(request), `${gesture}: releasing the gesture cannot revive pending navigation`);
  navigation.fail(request);
  ok(navigation.current === null, `${gesture}: late failure cannot offer a cancelled retry`);
}
kernel.replaceSurface("A");
const a = navigation.start(question);
const aData = deferred();
const aLoad = history.load(() => aData.promise);
await Promise.resolve();
kernel.replaceSurface("B");
const b = navigation.start({ ...question, id: "B" });
const bData = deferred();
let bCalls = 0;
const bLoad = history.load(() => { bCalls += 1; return bData.promise; });
aData.resolve(false);
await aLoad;
navigation.fail(a);
ok(navigation.current === b && b.status === "pending", "A failure does not alter B UI");
const sameBLoad = history.load(() => { bCalls += 1; return false; });
ok(sameBLoad === bLoad && bCalls === 1, "A finally cannot release B's request");
kernel.replaceSurface("A");
ok(!navigation.owns(a) && !navigation.owns(b), "A→B→A rejects both old owners");
const newest = navigation.start(question);
const replaced = navigation.start({ ...question, id: "newer" });
navigation.fail(newest);
ok(navigation.current === replaced && replaced.status === "pending", "new jump supersedes old failure even at the same turn");
kernel.detachSurface();
ok(!navigation.owns(replaced), "unmount revokes navigation ownership");
bData.resolve(true);
ok(!await bLoad, "B data completion cannot claim the detached surface");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed) process.exit(1);
