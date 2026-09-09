// Run: tsx src/__tests__/controller-notices-heads.test.ts
//
// Head notices from the session log localize by their stable code so the
// backend text, which may carry a head id, never reaches the transcript.

import assert from "node:assert/strict";
import { localizedNoticeText, quietTranscriptNoticeKey } from "../lib/controllerNotices";

const cases: Array<[string, string, string]> = [
  ["session_concurrent_writer", "another writer appended to this session log", "Another Reasonix window or process added to this conversation. Its content is kept as a separate version; use View versions to switch."],
  ["session_head_switched", "opened head 01HEAD (newest activity)", "Opened the newest version of this conversation. Other saved versions are available in View versions."],
  ["session_head_selected", "selected head 01HEAD", "This version is now the current version of the conversation."],
];
for (const [code, raw, want] of cases) {
  assert.equal(localizedNoticeText(raw, code), want, `${code} localizes by code`);
  assert.equal(quietTranscriptNoticeKey(raw, code), "", `${code} is shown, not a quiet lifecycle notice`);
  assert.ok(!localizedNoticeText(raw, code).includes("01HEAD"), `${code} must not leak head ids`);
}
console.log("  PASS  session-log head notices localize by code and stay visible");
