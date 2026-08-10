// Run: tsx src/__tests__/inbox-recovery-preview.test.ts

import assert from "node:assert/strict";
import { inboxRecoveryPreviewSnapshot, setInboxRecoveryPreviewPaused } from "../lib/inboxRecoveryPreview";

const paused = inboxRecoveryPreviewSnapshot(true);
assert.equal(paused.paused, true);
assert.equal(paused.recovered, true);
assert.equal(paused.recoveredCount, 30);
assert.equal(paused.itemsCount, 30);
assert.equal(paused.items.length, 30);
assert.equal(paused.bytes, 30 * 1024 * 1024);
assert.equal(new Set(paused.items.map((item) => item.id)).size, paused.items.length);
assert.ok(paused.items.every((item) => item.state === "uncertain" && item.intent === "followup"));

const resumed = inboxRecoveryPreviewSnapshot(false);
assert.equal(resumed.paused, false);
assert.equal(resumed.recovered, true);

setInboxRecoveryPreviewPaused(false);
assert.equal(inboxRecoveryPreviewSnapshot().paused, false);
setInboxRecoveryPreviewPaused(true);
assert.equal(inboxRecoveryPreviewSnapshot().paused, true);

process.stdout.write("inbox recovery preview: PASS\n");
