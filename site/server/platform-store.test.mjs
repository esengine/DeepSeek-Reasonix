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

function graphPublication(sourceJobId = "JOB-GRAPH-1") {
  return {
    publicationId: `PUB-${sourceJobId}`,
    sourceJobId,
    status: "published",
    version: "V1.0",
    publishedAt: "2026-08-10T09:00:00.000Z",
    document: { title: "企业知识产权技术报告", sourceName: "ip-report.pdf" },
    assets: [
      {
        id: "IP-GRAPH-CORE",
        title: "多模态知识抽取引擎",
        type: "核心技术",
        owner: "算法团队",
        sensitivity: "内部",
        status: "有效",
        confidence: 0.96,
        tags: ["多模态", "知识抽取"],
        version: "V1.0",
        wiki: {
          title: "多模态知识抽取引擎",
          executiveSummary: "从长文档中提取可追溯的知识资产。",
          keyMechanism: "版面解析与大模型联合抽取",
          metrics: [],
          relationships: [{ source: "多模态知识抽取引擎", relation: "依赖", target: "文档版面解析器" }],
        },
        evidence: [{ id: "EV-CORE-1", label: "第 12 页", quote: "抽取引擎依赖版面解析结果" }],
      },
      {
        id: "IP-GRAPH-PARSER",
        title: "文档版面解析器",
        type: "软件著作权",
        owner: "平台团队",
        sensitivity: "内部",
        status: "有效",
        confidence: 0.91,
        tags: ["MinerU", "OCR"],
        version: "V1.0",
        wiki: {
          title: "文档版面解析器",
          executiveSummary: "解析文档版面、表格与公式。",
          keyMechanism: "版面检测与 OCR",
          metrics: [],
          relationships: [],
        },
        evidence: [{ id: "EV-PARSER-1", label: "第 8 页", quote: "版面解析器输出结构化块" }],
      },
      {
        id: "IP-GRAPH-SECRET",
        title: "未公开商业策略",
        type: "商业秘密",
        owner: "管理层",
        sensitivity: "机密",
        status: "有效",
        confidence: 0.88,
        tags: ["战略"],
        version: "V1.0",
        wiki: {
          title: "未公开商业策略",
          executiveSummary: "仅限授权管理人员访问。",
          keyMechanism: "保密经营策略",
          metrics: [],
          relationships: [],
        },
        evidence: [{ id: "EV-SECRET-1", label: "第 30 页", quote: "保密内容" }],
      },
    ],
  };
}

test("projects publications into an idempotent workspace-scoped asset graph", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.ensureWorkspace({ id: "WS-B", name: "乙公司" });
    fx.store.savePublication("WS-A", graphPublication());
    fx.store.savePublication("WS-A", graphPublication());
    fx.store.savePublication("WS-B", graphPublication("JOB-GRAPH-2"));

    const graphA = fx.store.getAssetGraph("WS-A", { role: "owner", includeProposed: true });
    assert.equal(graphA.nodes.length, 3);
    assert.equal(graphA.edges.length, 1);
    assert.equal(graphA.edges[0].relationType, "depends_on");
    assert.equal(graphA.edges[0].verificationStatus, "proposed");
    assert.deepEqual(graphA.edges[0].evidenceIds, ["EV-CORE-1", "EV-PARSER-1"]);
    assert.ok(graphA.edges[0].id.startsWith("REL-"));
    assert.equal(fx.store.getAssetGraph("WS-C", { role: "owner" }).nodes.length, 0);
  } finally {
    await fx.close();
  }
});

test("enforces relationship lifecycle, evidence links, and cross-workspace integrity", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.ensureWorkspace({ id: "WS-B", name: "乙公司" });
    fx.store.savePublication("WS-A", graphPublication());
    fx.store.savePublication("WS-B", graphPublication("JOB-GRAPH-2"));

    const relation = fx.store.createAssetRelationship("WS-A", {
      sourceAssetId: "IP-GRAPH-PARSER",
      targetAssetId: "IP-GRAPH-SECRET",
      relationType: "references",
      evidenceIds: ["EV-PARSER-1"],
      origin: "manual",
      createdBy: null,
    });
    assert.equal(relation.verificationStatus, "confirmed");
    assert.deepEqual(relation.evidenceIds, ["EV-PARSER-1"]);
    assert.throws(() => fx.store.createAssetRelationship("WS-A", {
      sourceAssetId: "IP-GRAPH-PARSER",
      targetAssetId: "IP-DOES-NOT-EXIST",
      relationType: "references",
    }), (error) => error.code === "INVALID_RELATION_ENDPOINT");
    assert.throws(() => fx.store.createAssetRelationship("WS-A", {
      sourceAssetId: "IP-GRAPH-PARSER",
      targetAssetId: "IP-GRAPH-CORE",
      relationType: "references",
      evidenceIds: ["EV-HALLUCINATED"],
    }), (error) => error.code === "INVALID_RELATION_EVIDENCE");

    const proposed = fx.store.getAssetGraph("WS-A", { role: "owner", includeProposed: true }).edges.find((edge) => edge.verificationStatus === "proposed");
    assert.equal(fx.store.updateAssetRelationshipStatus("WS-A", proposed.id, "confirmed", { updatedBy: null }).verificationStatus, "confirmed");
    assert.throws(() => fx.store.updateAssetRelationshipStatus("WS-B", proposed.id, "rejected"), (error) => error.code === "NOT_FOUND");
  } finally {
    await fx.close();
  }
});

test("commits relationship mutations and their audit event atomically", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.savePublication("WS-A", graphPublication());
    const relationship = fx.store.createAssetRelationship("WS-A", {
      sourceAssetId: "IP-GRAPH-PARSER",
      targetAssetId: "IP-GRAPH-SECRET",
      relationType: "references",
      evidenceIds: ["EV-PARSER-1"],
    }, { audit: { actorUserId: null, action: "relationship.create", objectType: "asset_relationship", detail: { reason: "人工复核" } } });
    assert.equal(fx.store.verifyAuditChain("WS-A").count, 1);
    assert.equal(fx.store.unsafeDatabaseForTests.prepare("SELECT object_id FROM audit_events WHERE workspace_id = ?").get("WS-A").object_id, relationship.id);

    assert.throws(() => fx.store.createAssetRelationship("WS-A", {
      sourceAssetId: "IP-GRAPH-CORE",
      targetAssetId: "IP-GRAPH-SECRET",
      relationType: "references",
    }, { audit: { actorUserId: "USR-NOT-IN-WORKSPACE", action: "relationship.create", objectType: "asset_relationship", detail: {} } }));
    assert.equal(fx.store.getAssetGraph("WS-A", { role: "owner" }).edges.filter((edge) => edge.relationType === "references").length, 1);
  } finally {
    await fx.close();
  }
});

test("filters confidential nodes before traversal and explains graph-expanded search", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.savePublication("WS-A", graphPublication());
    fx.store.createAssetRelationship("WS-A", {
      sourceAssetId: "IP-GRAPH-PARSER",
      targetAssetId: "IP-GRAPH-SECRET",
      relationType: "references",
      origin: "manual",
    });

    const viewerGraph = fx.store.getAssetGraph("WS-A", { role: "viewer", includeProposed: true, rootAssetId: "IP-GRAPH-PARSER", depth: 2 });
    assert.deepEqual(viewerGraph.nodes.map((node) => node.id).sort(), ["IP-GRAPH-PARSER"]);
    assert.equal(viewerGraph.edges.length, 0);
    const editorGraph = fx.store.getAssetGraph("WS-A", { role: "editor", includeProposed: true, rootAssetId: "IP-GRAPH-PARSER", depth: 2 });
    assert.ok(editorGraph.nodes.some((node) => node.id === "IP-GRAPH-SECRET"));

    const proposed = fx.store.getAssetGraph("WS-A", { role: "owner", includeProposed: true }).edges.find((edge) => edge.relationType === "depends_on");
    fx.store.updateAssetRelationshipStatus("WS-A", proposed.id, "confirmed");
    const search = fx.store.searchAssetGraph("WS-A", "多模态", { role: "viewer", depth: 1 });
    assert.equal(search.results[0].asset.id, "IP-GRAPH-CORE");
    assert.ok(search.results.some((result) => result.asset.id === "IP-GRAPH-PARSER" && result.matchKind === "graph_expansion"));
    assert.ok(search.results.every((result) => result.asset.id !== "IP-GRAPH-SECRET"));
  } finally {
    await fx.close();
  }
});
