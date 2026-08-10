import assert from "node:assert/strict";
import { mkdtemp, rm, stat } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { createPlatformStore } from "./platform-store.mjs";

async function fixture() {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-platform-store-"));
  const store = createPlatformStore({ dbPath: path.join(directory, "platform.sqlite") });
  return {
    directory,
    store,
    async close() {
      store.close();
      await rm(directory, { recursive: true, force: true });
    },
  };
}

function publication(sourceJobId = "JOB-1", assetId = "IP-REAL-A") {
  return {
    publicationId: `PUB-${sourceJobId}`,
    sourceJobId,
    status: "published",
    version: "V1.0",
    publishedAt: "2026-08-10T08:00:00.000Z",
    document: { title: "技术报告", sourceName: "report.pdf" },
    assets: [{
      id: assetId,
      title: "推理路由方法",
      version: "V1.0",
      wiki: {
        title: "推理路由方法",
        executiveSummary: "初始摘要",
        keyMechanism: "初始机制",
        metrics: [],
        relationships: [],
      },
      evidence: [],
    }],
  };
}

test("persists workspace users and opaque sessions without exposing another workspace", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.ensureWorkspace({ id: "WS-B", name: "乙公司" });
    fx.store.createUser({ id: "USR-A", workspaceId: "WS-A", email: "owner-a@example.com", name: "甲方", role: "owner", passwordHash: "scrypt-a" });
    fx.store.createUser({ id: "USR-B", workspaceId: "WS-B", email: "owner-b@example.com", name: "乙方", role: "viewer", passwordHash: "scrypt-b" });
    fx.store.createSession({ id: "SES-A", userId: "USR-A", tokenHash: "opaque-hash", expiresAt: "2099-01-01T00:00:00.000Z" });

    const session = fx.store.getSession("opaque-hash");
    assert.deepEqual({ userId: session.user.id, workspaceId: session.workspace.id, role: session.user.role }, { userId: "USR-A", workspaceId: "WS-A", role: "owner" });
    assert.equal(session.user.passwordHash, undefined);
    assert.equal(fx.store.getUserByEmail("owner-b@example.com").workspaceId, "WS-B");
  } finally {
    await fx.close();
  }
});

test("persists jobs and marks unfinished work as safely retryable after restart", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    const job = { id: "JOB-REAL-1", state: "deepseek", progress: 68, stageLabel: "分析中", createdAt: "2026-08-10T08:00:00.000Z", updatedAt: "2026-08-10T08:01:00.000Z", document: { name: "report.pdf" } };
    fx.store.saveJob("WS-A", job, { uploadPath: "C:/runtime/upload.bin" });
    fx.store.markInterruptedJobs();

    const recovered = fx.store.getJob("WS-A", job.id);
    assert.equal(recovered.job.state, "interrupted");
    assert.equal(recovered.job.retryable, true);
    assert.equal(recovered.uploadPath, "C:/runtime/upload.bin");
    assert.equal(fx.store.getJob("WS-B", job.id), null);
  } finally {
    await fx.close();
  }
});

test("publishes idempotently per workspace and isolates identical asset ids", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.ensureWorkspace({ id: "WS-B", name: "乙公司" });
    const first = fx.store.savePublication("WS-A", publication());
    const second = fx.store.savePublication("WS-A", { ...publication(), publishedAt: "2099-01-01T00:00:00.000Z" });
    fx.store.savePublication("WS-B", publication("JOB-2", "IP-REAL-A"));

    assert.equal(first.publishedAt, second.publishedAt);
    assert.equal(fx.store.listPublications("WS-A").length, 1);
    assert.equal(fx.store.findAsset("WS-A", "IP-REAL-A").title, "推理路由方法");
    assert.equal(fx.store.findAsset("WS-C", "IP-REAL-A"), null);
  } finally {
    await fx.close();
  }
});

test("creates append-only Wiki versions and rejects a stale base version", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.savePublication("WS-A", publication());
    const updated = fx.store.saveWikiVersion("WS-A", "IP-REAL-A", {
      baseVersion: "V1.0",
      title: "推理路由方法（复核版）",
      executiveSummary: "人工复核摘要",
      keyMechanism: "人工复核机制",
      changeNote: "修正文案",
      editorUserId: null,
    });

    assert.equal(updated.version, "V1.1");
    assert.equal(fx.store.getWiki("WS-A", "IP-REAL-A").executiveSummary, "人工复核摘要");
    assert.equal(fx.store.listWikiVersions("WS-A", "IP-REAL-A").length, 2);
    assert.throws(() => fx.store.saveWikiVersion("WS-A", "IP-REAL-A", {
      baseVersion: "V1.0",
      title: "冲突编辑",
      executiveSummary: "冲突",
      keyMechanism: "冲突",
    }), (error) => error.code === "VERSION_CONFLICT");
  } finally {
    await fx.close();
  }
});

test("chains audit events per workspace and detects stored tampering", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.appendAudit("WS-A", { actorUserId: null, action: "login", objectType: "session", objectId: "SES-1", detail: { result: "success" } });
    fx.store.appendAudit("WS-A", { actorUserId: null, action: "wiki.update", objectType: "wiki", objectId: "IP-REAL-A", detail: { version: "V1.1" } });
    assert.deepEqual(fx.store.verifyAuditChain("WS-A"), { valid: true, count: 2 });

    fx.store.unsafeDatabaseForTests.prepare("UPDATE audit_events SET detail_json = ? WHERE sequence = 1").run('{"result":"changed"}');
    assert.equal(fx.store.verifyAuditChain("WS-A").valid, false);
  } finally {
    await fx.close();
  }
});

test("creates a consistent online SQLite backup", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    const destination = path.join(fx.directory, "backups", "snapshot.sqlite");
    await fx.store.backupTo(destination);
    assert.ok((await stat(destination)).size > 0);
    const snapshot = createPlatformStore({ dbPath: destination });
    try {
      assert.equal(snapshot.unsafeDatabaseForTests.prepare("SELECT name FROM workspaces WHERE id = ?").get("WS-A").name, "甲公司");
      assert.equal(snapshot.integrityCheck().valid, true);
    } finally {
      snapshot.close();
    }
  } finally {
    await fx.close();
  }
});

test("persists one-time invitations and protects owner membership", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.createUser({ id: "USR-OWNER", workspaceId: "WS-A", email: "owner@example.com", name: "所有者", role: "owner", passwordHash: "scrypt-owner" });
    const first = fx.store.createInvitation("WS-A", { id: "INV-1", email: "member@example.com", name: "新成员", role: "editor", tokenHash: "hash-1", invitedBy: "USR-OWNER", expiresAt: "2099-01-01T00:00:00.000Z" });
    assert.equal(first.status, "pending");
    fx.store.createInvitation("WS-A", { id: "INV-2", email: "member@example.com", name: "新成员", role: "viewer", tokenHash: "hash-2", invitedBy: "USR-OWNER", expiresAt: "2099-01-01T00:00:00.000Z" });
    assert.equal(fx.store.getInvitationByTokenHash("hash-1"), null);
    const accepted = fx.store.acceptInvitation("hash-2", { userId: "USR-MEMBER", passwordHash: "scrypt-member" });
    assert.equal(accepted.user.role, "viewer");
    assert.equal(accepted.user.passwordHash, undefined);
    assert.equal(fx.store.getInvitationByTokenHash("hash-2"), null);
    assert.throws(() => fx.store.acceptInvitation("hash-2", { userId: "USR-SECOND", passwordHash: "x" }), (error) => error.code === "INVITATION_UNAVAILABLE");
    assert.throws(() => fx.store.updateMember("WS-A", "USR-OWNER", { role: "viewer", disabled: true }), (error) => error.code === "OWNER_PROTECTED");
    const disabled = fx.store.updateMember("WS-A", "USR-MEMBER", { role: "editor", disabled: true });
    assert.equal(disabled.status, "disabled");
    assert.equal(fx.store.listMembers("WS-A").length, 2);
    assert.equal(fx.store.listMembers("WS-B").length, 0);
  } finally {
    await fx.close();
  }
});
