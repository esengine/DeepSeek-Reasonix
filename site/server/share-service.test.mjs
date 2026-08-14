import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { createPlatformStore } from "./platform-store.mjs";
import { createShareService } from "./share-service.mjs";

function publication() {
  return { publicationId: "PUB-1", sourceJobId: "JOB-1", status: "published", version: "V1.0", publishedAt: "2026-08-10T08:00:00.000Z", document: { title: "内部技术报告", sourceName: "secret.pdf", markdownPreview: "绝密原文" }, assets: [{ id: "IP-REAL-SHARE", title: "路由技术", version: "V1.0", summary: "可分享摘要", wiki: { title: "路由技术 Wiki", executiveSummary: "脱敏执行摘要", keyMechanism: "脱敏关键机制", metrics: [{ label: "效率", value: "31%" }], relationships: [] }, evidence: [{ quote: "不应公开的逐字证据" }] }] };
}

async function fixture() {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-share-"));
  const store = createPlatformStore({ dbPath: path.join(directory, "share.sqlite") });
  store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
  store.ensureWorkspace({ id: "WS-B", name: "乙公司" });
  store.savePublication("WS-A", publication());
  let clock = Date.parse("2026-08-10T09:00:00.000Z");
  return { directory, store, service: createShareService({ store, now: () => new Date(clock) }), advance(ms) { clock += ms; }, async close() { store.close(); await rm(directory, { recursive: true, force: true }); } };
}

test("stores only share hashes and returns a redacted allowlist after double-secret access", async () => {
  const fx = await fixture();
  try {
    const created = fx.service.create({ workspaceId: "WS-A", assetId: "IP-REAL-SHARE", recipientEmail: "partner@example.com", expires: "7d" });
    const stored = fx.store.unsafeDatabaseForTests.prepare("SELECT token_hash, access_code_hash FROM secure_shares WHERE id = ?").get(created.share.id);
    assert.notEqual(stored.token_hash, created.token);
    assert.notEqual(stored.access_code_hash, created.accessCode);
    assert.doesNotMatch(JSON.stringify(fx.service.list("WS-A")), new RegExp(`${created.token}|${created.accessCode}`));
    assert.equal(fx.service.inspect(created.token).recipient, "pa*****@example.com");
    assert.throws(() => fx.service.access({ token: created.token, accessCode: "wrong-code" }), (error) => error.code === "SHARE_UNAVAILABLE");
    const unlocked = fx.service.access({ token: created.token, accessCode: created.accessCode });
    assert.equal(unlocked.wiki.executiveSummary, "脱敏执行摘要");
    assert.equal(unlocked.wiki.evidence, undefined);
    assert.equal(unlocked.wiki.document, undefined);
    assert.doesNotMatch(JSON.stringify(unlocked), /绝密原文|不应公开的逐字证据|secret\.pdf/);
    assert.equal(fx.service.list("WS-A")[0].accessCount, 1);
    assert.equal(fx.store.verifyAuditChain("WS-A").valid, true);
  } finally {
    await fx.close();
  }
});

test("isolates, expires, and revokes secure shares", async () => {
  const fx = await fixture();
  try {
    const created = fx.service.create({ workspaceId: "WS-A", assetId: "IP-REAL-SHARE", recipientEmail: "partner@example.com", expires: "24h" });
    assert.equal(fx.service.list("WS-B").length, 0);
    assert.equal(fx.service.revoke("WS-B", created.share.id), null);
    assert.equal(fx.service.revoke("WS-A", created.share.id).status, "revoked");
    assert.equal(fx.service.inspect(created.token), null);
    const expiring = fx.service.create({ workspaceId: "WS-A", assetId: "IP-REAL-SHARE", recipientEmail: "later@example.com", expires: "24h" });
    fx.advance(24 * 60 * 60_000 + 1);
    assert.equal(fx.service.inspect(expiring.token), null);
    assert.throws(() => fx.service.access({ token: expiring.token, accessCode: expiring.accessCode }), (error) => error.code === "SHARE_UNAVAILABLE");
  } finally {
    await fx.close();
  }
});
