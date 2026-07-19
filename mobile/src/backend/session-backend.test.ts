import assert from "node:assert/strict";
import { LocalBackend } from "./session-backend.ts";
import { LOCAL_CAPABILITIES } from "../protocol/types.ts";

async function main() {
  const backend = new LocalBackend();
  assert.equal(backend.runtime, "local");

  await assert.rejects(
    () => backend.createSession({ runtime: "remote" }),
    /only creates local/,
  );

  const d = await backend.createSession({ runtime: "local", title: "t1" });
  assert.equal(d.runtime, "local");
  assert.deepEqual(d.capabilities, [...LOCAL_CAPABILITIES]);

  const events: unknown[] = [];
  const unsub = backend.subscribe(d.id, (e, seq) => {
    events.push({ e, seq });
  });
  await backend.submit(d.id, { text: "hello" }, "req-1");
  assert.ok(events.length >= 2);
  assert.equal((events[0] as { seq: number }).seq, 1);
  unsub();

  const snap = await backend.snapshot(d.id);
  assert.equal(snap.descriptor.id, d.id);
  assert.ok((snap.lastEventSeq ?? 0) >= 1);

  // Approval path: dangerous text pauses until approve/deny.
  const d2 = await backend.createSession({ runtime: "local", title: "t2" });
  let approvalId = "";
  const unsub2 = backend.subscribe(d2.id, (e) => {
    const ev = e as { kind?: string; approval?: { id?: string } };
    if (ev.kind === "approval_request" && ev.approval?.id) {
      approvalId = ev.approval.id;
      void backend.approve(d2.id, { id: approvalId, allow: true }, "req-appr");
    }
  });
  await backend.submit(d2.id, { text: "delete tmp/scratch.log" }, "req-2");
  unsub2();
  assert.ok(approvalId, "expected approval_request");
  const snap2 = await backend.snapshot(d2.id);
  assert.equal(snap2.descriptor.status, "idle");

  console.log("session-backend.test.ts: ok");
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
