import assert from "node:assert/strict";
import { imageSubmissionIdentity, settleImageSubmission, captureImageTarget } from "../lib/attachmentSubmit";
import { app, type AppBindings } from "../lib/bridge";

const first = imageSubmissionIdentity("session-a", "draft-version-1");
assert.equal(imageSubmissionIdentity("session-a", "draft-version-1"), first);
assert.notEqual(imageSubmissionIdentity("session-b", "draft-version-1"), first);
const edited = imageSubmissionIdentity("session-a", "draft-version-2");
assert.notEqual(edited, first);
settleImageSubmission("session-a", first);
assert.equal(imageSubmissionIdentity("session-a", "draft-version-2"), edited);
settleImageSubmission("session-a", edited);
assert.notEqual(imageSubmissionIdentity("session-a", "draft-version-2"), edited);
await assert.rejects(captureImageTarget({} as AppBindings, { kind: "session", tabId: "a" }), /unsupported/);
const captured = await app.CaptureAttachmentTarget!({ kind: "session", tabId: "mock-tab" });
assert.deepEqual(captured.capabilities, ["attachments-v2"]);
const staged = await app.StageImageForTarget!(captured.token, "mock-operation", "photo.png", "image/png", "data:image/png;base64,aW1hZ2U=");
assert.equal(await app.ReadDraftImageForTarget!(captured.token, staged.draftId), "data:image/png;base64,aW1hZ2U=");
console.log("attachment submission identity: retry, edit, session isolation, capability rejection, and lazy mock loading passed");
