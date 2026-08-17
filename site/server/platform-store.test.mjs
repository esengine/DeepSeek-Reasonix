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

test("persists creator-scoped Agent tasks and append-only step receipts", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.ensureWorkspace({ id: "WS-B", name: "乙公司" });
    const task = {
      id: "AGT-1",
      state: "running",
      prompt: "盘点知识抽取资产",
      createdBy: "USR-A",
      createdAt: "2026-08-10T08:00:00.000Z",
      updatedAt: "2026-08-10T08:00:01.000Z",
    };
    fx.store.saveAgentTask("WS-A", task);
    fx.store.appendAgentTaskEvent("WS-A", task.id, { id: "AGE-1", type: "plan.ready", stepId: null, detail: { stepCount: 2 }, createdAt: "2026-08-10T08:00:02.000Z" });
    fx.store.appendAgentTaskEvent("WS-A", task.id, { id: "AGE-2", type: "step.complete", stepId: "S1", detail: { tool: "search_assets", resultCount: 3 }, createdAt: "2026-08-10T08:00:03.000Z" });

    assert.equal(fx.store.getAgentTask("WS-A", "AGT-1", "USR-A").prompt, task.prompt);
    assert.equal(fx.store.getAgentTask("WS-A", "AGT-1", "USR-OTHER"), null);
    assert.equal(fx.store.getAgentTask("WS-B", "AGT-1", "USR-A"), null);
    assert.equal(fx.store.listAgentTasks("WS-A", "USR-A").length, 1);
    assert.equal(fx.store.listAgentTasks("WS-A", "USR-OTHER").length, 0);
    assert.deepEqual(fx.store.listAgentTaskEvents("WS-A", "AGT-1").map((event) => [event.type, event.stepId]), [["plan.ready", null], ["step.complete", "S1"]]);
    assert.throws(() => fx.store.appendAgentTaskEvent("WS-B", task.id, { id: "AGE-X", type: "step.complete", detail: {} }));
  } finally {
    await fx.close();
  }
});

test("marks in-flight Agent tasks interrupted without replaying them", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    for (const [id, state] of [["AGT-PLANNING", "planning"], ["AGT-RUNNING", "running"], ["AGT-DONE", "complete"], ["AGT-BLOCKED", "blocked"]]) {
      fx.store.saveAgentTask("WS-A", { id, state, prompt: "任务", createdBy: "USR-A", createdAt: "2026-08-10T08:00:00.000Z", updatedAt: "2026-08-10T08:00:00.000Z" });
    }
    assert.equal(fx.store.markInterruptedAgentTasks(), 2);
    assert.equal(fx.store.getAgentTask("WS-A", "AGT-PLANNING", "USR-A").state, "interrupted");
    assert.equal(fx.store.getAgentTask("WS-A", "AGT-RUNNING", "USR-A").state, "interrupted");
    assert.equal(fx.store.getAgentTask("WS-A", "AGT-DONE", "USR-A").state, "complete");
    assert.equal(fx.store.getAgentTask("WS-A", "AGT-BLOCKED", "USR-A").state, "blocked");
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

test("persists idempotent semantic review candidates without mutating formal assets", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.ensureWorkspace({ id: "WS-B", name: "乙公司" });
    const firstPublication = publication("JOB-SEM-A", "IP-REAL-A");
    firstPublication.assets[0].title = "企业知识中台";
    firstPublication.assets[0].owner = "产品部";
    firstPublication.assets[0].sensitivity = "内部";
    const secondPublication = publication("JOB-SEM-B", "IP-REAL-B");
    secondPublication.document.sourceName = "second-report.pdf";
    secondPublication.assets[0].title = "企业知识中台";
    secondPublication.assets[0].owner = "研发部";
    secondPublication.assets[0].sensitivity = "内部";
    fx.store.savePublication("WS-A", firstPublication);
    fx.store.savePublication("WS-A", secondPublication);
    const formalBefore = fx.store.listPublications("WS-A");
    const result = {
      engine: "Semantica",
      version: "0.6.0",
      duplicates: [{ assetIds: ["IP-REAL-B", "IP-REAL-A"], similarity: 0.94, confidence: 0.91, reasons: ["标题完全一致", ...Array.from({ length: 12 }, (_, index) => `原因 ${index}`)] }],
      conflicts: [{ title: "不可信标题", field: "owner", severity: "high", confidence: 0.92, values: ["研发部", "产品部"], sources: [{ assetId: "IP-REAL-A", document: "secret-a.pdf", value: "产品部" }, { assetId: "IP-REAL-B", document: "secret-b.pdf", value: "研发部" }] }],
    };

    const created = fx.store.upsertSemanticReviews("WS-A", result, { detectedAt: "2026-08-12T01:00:00.000Z" });
    assert.equal(created.length, 2);
    assert.ok(created.every((review) => review.id.startsWith("SEMREV-") && review.status === "pending"));
    assert.deepEqual(created.find((review) => review.kind === "duplicate").payload.assetIds, ["IP-REAL-A", "IP-REAL-B"]);
    assert.deepEqual(created.find((review) => review.kind === "duplicate").payload.assets.map((asset) => asset.sourceName), ["report.pdf", "second-report.pdf"]);
    assert.equal(created.find((review) => review.kind === "duplicate").payload.reasons.length, 8);
    assert.equal(created.find((review) => review.kind === "conflict").payload.title, "企业知识中台");
    assert.ok(!JSON.stringify(created).includes("secret-a.pdf"));

    const rerun = fx.store.upsertSemanticReviews("WS-A", { ...result, duplicates: [{ ...result.duplicates[0], confidence: 0.99 }] }, { detectedAt: "2026-08-12T02:00:00.000Z" });
    assert.deepEqual(rerun.map((review) => review.id).sort(), created.map((review) => review.id).sort());
    assert.ok(rerun.every((review) => review.lastSeenAt === "2026-08-12T02:00:00.000Z"));
    assert.equal(fx.store.listSemanticReviews("WS-A").length, 2);
    assert.equal(fx.store.listSemanticReviews("WS-B").length, 0);
    assert.deepEqual(fx.store.listPublications("WS-A"), formalBefore);
  } finally {
    await fx.close();
  }
});

test("commits a terminal semantic review decision and audit event atomically", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.createUser({ id: "USR-ADMIN", workspaceId: "WS-A", email: "admin@example.com", name: "管理员", role: "admin", passwordHash: "scrypt" });
    const first = publication("JOB-SEM-DECIDE-A", "IP-REAL-A");
    const second = publication("JOB-SEM-DECIDE-B", "IP-REAL-B");
    second.assets[0].title = first.assets[0].title;
    fx.store.savePublication("WS-A", first);
    fx.store.savePublication("WS-A", second);
    const [review] = fx.store.upsertSemanticReviews("WS-A", { engine: "Semantica", version: "0.6.0", duplicates: [{ assetIds: ["IP-REAL-A", "IP-REAL-B"], confidence: 0.9, reasons: ["标题完全一致"] }], conflicts: [] });

    const decided = fx.store.decideSemanticReview("WS-A", review.id, { decision: "confirmed", reviewNote: "交由知识产权负责人核对", reviewerUserId: "USR-ADMIN" }, { audit: { actorUserId: "USR-ADMIN", action: "semantic.review_confirm", objectType: "semantic_review" } });
    assert.equal(decided.status, "confirmed");
    assert.equal(decided.reviewedBy.name, "管理员");
    assert.equal(decided.reviewNote, "交由知识产权负责人核对");
    assert.equal(fx.store.listAuditEvents("WS-A", 10)[0].action, "semantic.review_confirm");
    assert.deepEqual(fx.store.listAuditEvents("WS-A", 10)[0].detail.assetIds, ["IP-REAL-A", "IP-REAL-B"]);
    assert.equal(fx.store.listAuditEvents("WS-A", 10)[0].detail.formalKnowledgeMutation, false);
    assert.throws(() => fx.store.decideSemanticReview("WS-A", review.id, { decision: "dismissed", reviewerUserId: "USR-ADMIN" }), (error) => error.code === "SEMANTIC_REVIEW_DECIDED");
    assert.throws(() => fx.store.decideSemanticReview("WS-B", review.id, { decision: "dismissed", reviewerUserId: "USR-ADMIN" }), (error) => error.code === "NOT_FOUND");

    fx.store.upsertSemanticReviews("WS-A", { engine: "Semantica", version: "0.6.0", duplicates: [{ assetIds: ["IP-REAL-A", "IP-REAL-B"], confidence: 0.99 }], conflicts: [] }, { detectedAt: "2026-08-12T03:00:00.000Z" });
    assert.equal(fx.store.listSemanticReviews("WS-A", { status: "confirmed" })[0].status, "confirmed");

    const conflictResult = { engine: "Semantica", version: "0.6.0", duplicates: [], conflicts: [{ title: first.assets[0].title, field: "owner", values: ["产品部", "研发部"], sources: [{ assetId: "IP-REAL-A", value: "产品部" }, { assetId: "IP-REAL-B", value: "研发部" }] }] };
    const [pending] = fx.store.upsertSemanticReviews("WS-A", conflictResult);
    assert.throws(() => fx.store.decideSemanticReview("WS-A", pending.id, { decision: "dismissed", reviewerUserId: "USR-ADMIN" }, { audit: { actorUserId: "USR-MISSING", action: "semantic.review_dismiss", objectType: "semantic_review" } }));
    assert.equal(fx.store.listSemanticReviews("WS-A").find((item) => item.id === pending.id).status, "pending");
  } finally {
    await fx.close();
  }
});

test("updates asset ownership and sensitivity with graph projection and audit", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.ensureWorkspace({ id: "WS-B", name: "乙公司" });
    fx.store.savePublication("WS-A", publication());

    const updated = fx.store.updateAssetMetadata("WS-A", "IP-REAL-A", { owner: "产品平台主管", sensitivity: "内部" }, {
      audit: { actorUserId: null, action: "asset.metadata_update", objectType: "ip_asset", detail: { owner: "产品平台主管", sensitivity: "内部" } },
    });

    assert.equal(updated.owner, "产品平台主管");
    assert.equal(updated.sensitivity, "内部");
    assert.equal(fx.store.findAsset("WS-A", "IP-REAL-A").owner, "产品平台主管");
    assert.equal(fx.store.getAssetGraph("WS-A", { role: "owner" }).nodes.find((node) => node.id === "IP-REAL-A").owner, "产品平台主管");
    assert.equal(fx.store.listAuditEvents("WS-A", 10)[0].action, "asset.metadata_update");
    assert.equal(fx.store.updateAssetMetadata("WS-B", "IP-REAL-A", { owner: "其他部门", sensitivity: "公开" }), null);
    assert.throws(() => fx.store.updateAssetMetadata("WS-A", "IP-REAL-A", { owner: "待确权", sensitivity: "绝密" }), (error) => error.code === "INVALID_ASSET_METADATA");
  } finally {
    await fx.close();
  }
});

test("updates a selected asset batch atomically and records one business event", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    const batch = publication("JOB-BATCH", "IP-REAL-A");
    batch.assets[0].owner = "待确权";
    batch.assets[0].sensitivity = "待复核";
    batch.assets.push({ ...structuredClone(batch.assets[0]), id: "IP-REAL-B", title: "文档检索方法" });
    fx.store.savePublication("WS-A", batch);

    assert.throws(() => fx.store.updateAssetMetadataBatch("WS-A", ["IP-REAL-A", "IP-REAL-MISSING"], { owner: "产品部", sensitivity: "内部" }), (error) => error.code === "NOT_FOUND");
    assert.equal(fx.store.findAsset("WS-A", "IP-REAL-A").owner, "待确权");

    const updated = fx.store.updateAssetMetadataBatch("WS-A", ["IP-REAL-A", "IP-REAL-B"], { owner: "产品部", sensitivity: "内部" }, {
      audit: { actorUserId: null, action: "asset.metadata_batch_update", objectType: "ip_asset_batch", objectId: "2-assets", detail: { count: 2 } },
    });
    assert.deepEqual(updated.map((asset) => asset.id), ["IP-REAL-A", "IP-REAL-B"]);
    assert.ok(updated.every((asset) => asset.owner === "产品部" && asset.sensitivity === "内部"));
    assert.ok(fx.store.getAssetGraph("WS-A", { role: "owner" }).nodes.filter((node) => updated.some((asset) => asset.id === node.id)).every((node) => node.owner === "产品部"));
    const events = fx.store.listAuditEvents("WS-A", 10).filter((event) => event.action === "asset.metadata_batch_update");
    assert.equal(events.length, 1);
    assert.equal(events[0].detail.count, 2);
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

test("rejects an unchanged Wiki update without creating a new version", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.savePublication("WS-A", publication());
    assert.throws(() => fx.store.saveWikiVersion("WS-A", "IP-REAL-A", {
      baseVersion: "V1.0",
      title: "推理路由方法",
      executiveSummary: "初始摘要",
      keyMechanism: "初始机制",
      changeNote: "没有实际变化",
    }), (error) => error.code === "NO_CHANGES");
    assert.equal(fx.store.listWikiVersions("WS-A", "IP-REAL-A").length, 1);
  } finally {
    await fx.close();
  }
});

test("persists Wiki review requests and publishes only after approval", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.createUser({ id: "USR-EDITOR", workspaceId: "WS-A", email: "editor@example.com", name: "知识编辑", role: "editor", passwordHash: "hash-editor" });
    fx.store.createUser({ id: "USR-ADMIN", workspaceId: "WS-A", email: "admin@example.com", name: "空间管理员", role: "admin", passwordHash: "hash-admin" });
    fx.store.savePublication("WS-A", publication());

    const review = fx.store.submitWikiReview("WS-A", "IP-REAL-A", {
      baseVersion: "V1.0",
      title: "推理路由方法（复核版）",
      executiveSummary: "等待审批的摘要",
      keyMechanism: "等待审批的机制",
      changeNote: "补充客户验证结论",
      submittedByUserId: "USR-EDITOR",
    });
    assert.match(review.id, /^WREV-/);
    assert.equal(review.status, "pending");
    assert.equal(review.submittedBy.name, "知识编辑");
    assert.equal(fx.store.getWiki("WS-A", "IP-REAL-A").version, "V1.0");
    assert.equal(fx.store.listWikiReviews("WS-A", { status: "pending" }).length, 1);

    const decision = fx.store.decideWikiReview("WS-A", review.id, { decision: "approved", reviewerUserId: "USR-ADMIN", reviewNote: "依据充分" });
    assert.equal(decision.review.status, "approved");
    assert.equal(decision.review.reviewedBy.name, "空间管理员");
    assert.equal(decision.wiki.version, "V1.1");
    assert.equal(decision.wiki.executiveSummary, "等待审批的摘要");
  } finally {
    await fx.close();
  }
});

test("rejects Wiki review without changing the published version and keeps a stale approval pending", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.createUser({ id: "USR-EDITOR", workspaceId: "WS-A", email: "editor@example.com", name: "知识编辑", role: "editor", passwordHash: "hash-editor" });
    fx.store.createUser({ id: "USR-ADMIN", workspaceId: "WS-A", email: "admin@example.com", name: "空间管理员", role: "admin", passwordHash: "hash-admin" });
    fx.store.savePublication("WS-A", publication());
    const rejected = fx.store.submitWikiReview("WS-A", "IP-REAL-A", { baseVersion: "V1.0", title: "退回标题", executiveSummary: "退回摘要", keyMechanism: "退回机制", submittedByUserId: "USR-EDITOR" });
    const rejectedDecision = fx.store.decideWikiReview("WS-A", rejected.id, { decision: "rejected", reviewerUserId: "USR-ADMIN", reviewNote: "需要补充原文依据" });
    assert.equal(rejectedDecision.review.status, "rejected");
    assert.equal(rejectedDecision.wiki, null);
    assert.equal(fx.store.getWiki("WS-A", "IP-REAL-A").version, "V1.0");

    const stale = fx.store.submitWikiReview("WS-A", "IP-REAL-A", { baseVersion: "V1.0", title: "过期草稿", executiveSummary: "过期摘要", keyMechanism: "过期机制", submittedByUserId: "USR-EDITOR" });
    fx.store.saveWikiVersion("WS-A", "IP-REAL-A", { baseVersion: "V1.0", title: "管理员更新", executiveSummary: "新摘要", keyMechanism: "新机制", editorUserId: "USR-ADMIN" });
    assert.throws(() => fx.store.decideWikiReview("WS-A", stale.id, { decision: "approved", reviewerUserId: "USR-ADMIN" }), (error) => error.code === "VERSION_CONFLICT");
    assert.equal(fx.store.listWikiReviews("WS-A", { status: "pending" })[0].id, stale.id);
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

test("lists sanitized audit events newest-first within one workspace", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.ensureWorkspace({ id: "WS-B", name: "乙公司" });
    fx.store.createUser({ id: "USR-A", workspaceId: "WS-A", email: "owner-a@example.com", name: "甲方", role: "owner", passwordHash: "hash-a" });
    fx.store.appendAudit("WS-A", { actorUserId: "USR-A", action: "evidence.view", objectType: "evidence", objectId: "EV-A", detail: { locator: "第一章" }, createdAt: "2026-08-10T08:00:00.000Z" });
    fx.store.appendAudit("WS-A", { actorUserId: "USR-A", action: "audit.export", objectType: "audit_ledger", objectId: "WS-A", detail: { format: "csv" }, createdAt: "2026-08-10T09:00:00.000Z" });
    fx.store.appendAudit("WS-B", { actorUserId: null, action: "other", objectType: "test", objectId: "B", detail: {} });

    const events = fx.store.listAuditEvents("WS-A", 10);
    assert.deepEqual(events.map((event) => event.action), ["audit.export", "evidence.view"]);
    assert.equal(events[0].actor.name, "甲方");
    assert.equal(events[0].detail.format, "csv");
    assert.equal(events[0].workspaceId, undefined);
    assert.equal(events.some((event) => "passwordHash" in event), false);
    assert.equal(fx.store.listAuditEvents("WS-B", 10).length, 1);
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
    fx.store.createSession({ id: "SES-MEMBER-OLD", userId: "USR-MEMBER", tokenHash: "session-member-old", createdAt: "2026-08-10T08:00:00.000Z", expiresAt: "2099-01-01T00:00:00.000Z" });
    fx.store.createSession({ id: "SES-MEMBER-NEW", userId: "USR-MEMBER", tokenHash: "session-member-new", createdAt: "2026-08-11T09:30:00.000Z", expiresAt: "2099-01-01T00:00:00.000Z" });
    const members = fx.store.listMembers("WS-A");
    assert.equal(members.find((member) => member.id === "USR-OWNER").lastLoginAt, null);
    assert.equal(members.find((member) => member.id === "USR-MEMBER").lastLoginAt, "2026-08-11T09:30:00.000Z");
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

test("resolves relationship titles within each publication when workspace titles repeat", async () => {
  const fx = await fixture();
  try {
    fx.store.ensureWorkspace({ id: "WS-A", name: "甲公司" });
    fx.store.savePublication("WS-A", graphPublication("JOB-GRAPH-V1"));
    const second = graphPublication("JOB-GRAPH-V2");
    second.assets = second.assets.map((asset) => ({
      ...asset,
      id: `${asset.id}-V2`,
      evidence: asset.evidence.map((evidence) => ({ ...evidence, id: `${evidence.id}-V2` })),
    }));
    fx.store.savePublication("WS-A", second);

    const graph = fx.store.getAssetGraph("WS-A", { role: "owner", includeProposed: true });
    assert.equal(graph.nodes.length, 6);
    assert.equal(graph.edges.length, 2);
    assert.ok(graph.edges.some((edge) => edge.sourceAssetId === "IP-GRAPH-CORE-V2" && edge.targetAssetId === "IP-GRAPH-PARSER-V2"));
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
