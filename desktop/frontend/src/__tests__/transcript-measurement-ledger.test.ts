import { TranscriptMeasurementLedger } from "../lib/transcriptMeasurementLedger";

let passed = 0;
let failed = 0;
function ok(condition: unknown, label: string) {
  if (condition) { process.stdout.write(`  PASS  ${label}\n`); passed += 1; }
  else { process.stdout.write(`  FAIL  ${label}\n`); failed += 1; }
}

console.log("\nTranscript immutable measurement ledger");

const ledger = new TranscriptMeasurementLedger();
ok(ledger.publicationLead(false) === 0, "an idle adapter has no measurement publication lead");
ok(ledger.publicationLead(true) === Number.POSITIVE_INFINITY, "an unclassified native gesture freezes every cold measurement");
ledger.observeWheel(2_880, 0, 596);
ok(ledger.publicationLead(true) === 3_476, "pixel wheel input reserves one native step plus one viewport");
ledger.observeWheel(120, 0, 596);
ok(ledger.publicationLead(true) === 3_596, "a wheel lease accumulates every unsettled native compositor step");
ledger.beginUnboundedGesture();
ok(ledger.publicationLead(true) === Number.POSITIVE_INFINITY, "touch, selection, thumb, or keyboard takeover upgrades a bounded lease to unbounded");
ok(ledger.publicationLead(false) === Number.POSITIVE_INFINITY, "native ownership freezes publication before React commits the kernel snapshot");
ledger.endGesture();
ledger.observeWheel(80, 0, 596);
ok(ledger.publicationLead(true) === 676, "gesture completion resets the prior publication lead");
ledger.endGesture();
ledger.observeWheel(18, 1, 596);
ok(ledger.publicationLead(true) === Number.POSITIVE_INFINITY, "non-pixel wheel input remains unbounded");
ledger.endGesture();
ok(!ledger.commit([]), "an empty measurement batch is a no-op");
ledger.stage([{ key: "post-viewport", size: 144 }]);
const published = ledger.publishStaged((key) => key === "post-viewport");
ok(published.length === 1 && published[0]?.key === "post-viewport" && published[0]?.size === 144,
  "publication returns the exact immutable suffix snapshot for the range adapter");
ok(ledger.publishStaged().length === 0, "an already published snapshot is not replayed into TanStack");

ok(ledger.commit([
  { key: "turn:1", size: 120 },
  { key: "turn:2", size: 240 },
]), "a valid measurement batch commits");
ok(ledger.sizeFor("turn:1", 64) === 120 && ledger.sizeFor("turn:2", 64) === 240, "all measurements become visible in the same snapshot");

ok(!ledger.commit([
  { key: "turn:1", size: 120.2 },
  { key: "turn:invalid", size: Number.NaN },
]), "sub-pixel noise and invalid measurements do not publish a partial snapshot");
ok(ledger.sizeFor("turn:invalid", 64) === 64, "ignored measurements leave the prior snapshot authoritative");

ok(ledger.commit([
  { key: "turn:1", size: 140 },
  { key: "turn:3", size: 360 },
]), "a later atomic batch replaces every changed key together");
ok(ledger.sizeFor("turn:1", 64) === 140 && ledger.sizeFor("turn:3", 64) === 360, "the second batch publishes complete contents");

ok(ledger.retain(new Set(["turn:1", "turn:3"])), "retaining live block identities removes obsolete measurements");
ok(ledger.sizeFor("turn:2", 64) === 64, "retention publishes one pruned snapshot");
ok(!ledger.retain(new Set(["turn:1", "turn:3"])), "retaining an unchanged identity set is a no-op");

ok(ledger.stage([
  { key: "turn:before-anchor", size: 180 },
  { key: "turn:after-anchor", size: 220 },
]), "DOM measurements can be staged before publication");
ok(ledger.publishStaged((key) => key === "turn:after-anchor").length === 1, "an anchor-safe subset publishes atomically");
ok(ledger.sizeFor("turn:before-anchor", 64) === 64, "a measurement before the reader anchor remains deferred");
ok(ledger.sizeFor("turn:after-anchor", 64) === 220, "a measurement after the reader anchor becomes authoritative");
ok(ledger.publishStaged().length === 1, "an explicit safe boundary publishes the deferred prefix measurement");
ok(ledger.sizeFor("turn:before-anchor", 64) === 180, "the deferred prefix survives window recycling until publication");

console.log(`\n${passed} passed, ${failed} failed`);
if (failed) process.exit(1);
