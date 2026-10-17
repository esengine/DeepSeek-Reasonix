import assert from "node:assert/strict";
import { attachOrdinaryFile, attachmentUploadFailure } from "../lib/attachmentSubmit";
import type { AppBindings } from "../lib/bridge";

const limit = { code: "attachment.too_large", message: "attachment exceeds 4 byte stream limit (user)", limit_bytes: 4, limit_source: "user" };
await assert.rejects(
  attachOrdinaryFile({} as AppBindings, "captured", new File(["12345"], "app.apk"), undefined,
    (async () => new Response(JSON.stringify(limit), { status: 413, headers: { "content-type": "application/json" } })) as typeof fetch),
  (error: unknown) => {
    assert.ok(error instanceof Error);
    assert.equal(error.message, limit.message);
    assert.equal((error as Error & typeof limit).code, limit.code);
    assert.equal((error as Error & typeof limit).limit_bytes, 4);
    assert.equal((error as Error & typeof limit).limit_source, "user");
    assert.equal(attachmentUploadFailure(error, "generic failure"), limit.message);
    assert.equal(attachmentUploadFailure(error, "generic failure", bytes => `limit ${bytes}`), "limit 4");
    return true;
  },
);
assert.equal(attachmentUploadFailure(new Error("unclassified server error"), "generic failure"), "generic failure");
await assert.rejects(
  attachOrdinaryFile({} as AppBindings, "captured", new File(["12345"], "app.apk"), undefined,
    (async () => new Response("upstream unavailable", { status: 502 })) as typeof fetch),
  /File upload failed \(502\)/,
);
console.log("attachment stream limit: typed server errors retain ceiling and source");
