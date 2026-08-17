import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { createRealAnalysisServer } from "./real-analysis-server.mjs";

test("serves the secured same-origin real analysis API", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-gateway-"));
  const dist = path.join(directory, "dist");
  await mkdir(dist);
  await writeFile(path.join(dist, "index.html"), "<!doctype html><title>intelifar</title>", "utf8");
  await writeFile(path.join(dist, "public-runtime.mjs"), "export const ready = true;", "utf8");
  const gateway = await createRealAnalysisServer({
    distRoot: dist,
    config: { mineruApiKey: "mineru-private", deepseekApiKey: "deepseek-private", deepseekModel: "deepseek-v4-flash" },
    mineruClient: { async parseDocument(file) { return { provider: "MinerU", model: "MinerU-HTML", batchId: "batch-test", traceId: "trace-test", fileName: file.name, markdown: "# parsed" }; } },
    registryRoot: path.join(directory, "registry"),
    deepseekClient: { async analyzeMarkdown() { return { provider: "DeepSeek", model: "deepseek-v4-flash", responseId: "chat-test", usage: { totalTokens: 12 }, analysis: { document: { title: "Report", summary: "Enterprise summary", category: "技术报告" }, assets: [{ id: "IP-1", title: "Published Wiki", type: "技术方案", summary: "Traceable", confidence: 0.97, tags: ["Wiki"], source_quotes: [{ quote: "Verbatim enterprise evidence", section: "Overview" }] }], risks: [], wiki: { executive_summary: "Wiki summary", key_mechanism: "Evidence first", metrics: [], relationships: [] } } }; } },
  });
  try {
    const baseUrl = await gateway.start();
    const health = await fetch(`${baseUrl}/api/health`);
    assert.equal(health.status, 200);
    assert.equal(health.headers.get("x-content-type-options"), "nosniff");
    assert.ok(health.headers.get("x-request-id"));
    const publicRuntime = await fetch(`${baseUrl}/public-runtime.mjs`);
    assert.equal(publicRuntime.status, 200);
    assert.match(publicRuntime.headers.get("content-type"), /^text\/javascript/);
    const healthPayload = await health.json();
    assert.equal(healthPayload.dataBoundary.gateway, "local");
    assert.deepEqual(healthPayload.dataBoundary.externalProcessors, ["MinerU", "DeepSeek"]);
    const form = new FormData();
    form.append("file", new Blob(["<!doctype html><html><body>IP</body></html>"], { type: "text/html" }), "report.html");
    form.append("category", "技术报告");
    const submitted = await fetch(`${baseUrl}/api/analysis`, { method: "POST", body: form });
    assert.equal(submitted.status, 202);
    const { job } = await submitted.json();
    const complete = await gateway.analysisService.whenSettled(job.id);
    const result = await (await fetch(`${baseUrl}/api/analysis/${job.id}`)).text();
    assert.equal(complete.state, "complete");
    assert.doesNotMatch(result, /mineru-private|deepseek-private/);
    const publishedResponse = await fetch(`${baseUrl}/api/analysis/${job.id}/publish`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ owner: "知识平台主管", sensitivity: "内部" }) });
    assert.equal(publishedResponse.status, 201);
    const { publication } = await publishedResponse.json();
    const assetId = publication.assets[0].id;
    const evidenceId = publication.assets[0].evidence[0].id;
    assert.equal((await (await fetch(`${baseUrl}/api/assets`)).json()).assets.length, 1);
    assert.equal((await (await fetch(`${baseUrl}/api/assets/${assetId}`)).json()).asset.title, "Published Wiki");
    assert.equal((await (await fetch(`${baseUrl}/api/wiki/${assetId}`)).json()).wiki.keyMechanism, "Evidence first");
    assert.equal((await (await fetch(`${baseUrl}/api/evidence/${evidenceId}`)).json()).evidence.precision, "章节级");
    assert.equal((await (await fetch(`${baseUrl}/api/search?q=Overview`)).json()).results.length, 1);
    const republished = await fetch(`${baseUrl}/api/analysis/${job.id}/publish`, { method: "POST" });
    assert.equal(republished.status, 200);
  } finally {
    await gateway.stop();
    await rm(directory, { recursive: true, force: true });
  }
});

test("rate limits analysis creation and rejects cross-origin state changes", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-gateway-"));
  const dist = path.join(directory, "dist");
  await mkdir(dist);
  await writeFile(path.join(dist, "index.html"), "<!doctype html><title>intelifar</title>", "utf8");
  const gateway = await createRealAnalysisServer({
    distRoot: dist,
    registryRoot: path.join(directory, "registry"),
    analysisRateLimit: 1,
    config: { mineruApiKey: "m", deepseekApiKey: "d", deepseekModel: "deepseek-v4-flash" },
    mineruClient: { async parseDocument(file) { return { provider: "MinerU", model: "HTML", batchId: "b", fileName: file.name, markdown: "# ok" }; } },
    deepseekClient: { async analyzeMarkdown() { return { provider: "DeepSeek", model: "deepseek-v4-flash", usage: {}, analysis: { document: {}, assets: [], risks: [], wiki: {} } }; } },
  });
  try {
    const baseUrl = await gateway.start();
    const makeForm = () => { const form = new FormData(); form.append("file", new Blob(["<html>ok</html>"], { type: "text/html" }), "ok.html"); return form; };
    assert.equal((await fetch(`${baseUrl}/api/analysis`, { method: "POST", body: makeForm() })).status, 202);
    const limited = await fetch(`${baseUrl}/api/analysis`, { method: "POST", body: makeForm() });
    assert.equal(limited.status, 429);
    assert.equal(limited.headers.get("retry-after"), "60");
    const crossOrigin = await fetch(`${baseUrl}/api/analysis`, { method: "POST", headers: { origin: "https://attacker.invalid" }, body: makeForm() });
    assert.equal(crossOrigin.status, 403);
  } finally {
    await gateway.stop();
    await rm(directory, { recursive: true, force: true });
  }
});

test("administrator semantic check receives only registry-authorized published assets", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-semantic-api-"));
  const dist = path.join(directory, "dist");
  await mkdir(dist);
  await writeFile(path.join(dist, "index.html"), "<!doctype html><title>intelifar</title>", "utf8");
  let receivedAssets = null;
  let receivedRole = null;
  const authorizedAsset = { id: "IP-REAL-A1", title: "企业知识中台", type: "技术方案", sensitivity: "内部", evidence: [] };
  const gateway = await createRealAnalysisServer({
    distRoot: dist,
    config: { mineruApiKey: "m", deepseekApiKey: "d", deepseekModel: "deepseek-chat" },
    mineruClient: { async parseDocument() { throw new Error("not used"); } },
    deepseekClient: { async analyzeMarkdown() { throw new Error("not used"); } },
    publicationRegistry: {
      async listAssets(_workspaceId, options) { receivedRole = options.role; return [authorizedAsset]; },
    },
    semanticaClient: {
      async status() { return { state: "ready", enabled: true, engine: "Semantica", version: "0.6.0", message: "本地语义增强可用" }; },
      async enrich(assets) { receivedAssets = assets; return { status: "complete", engine: "Semantica", version: "0.6.0", checkedAssets: assets.length, duplicates: [], conflicts: [], provenance: { assets: assets.length, evidence: 0, entries: [] } }; },
    },
    semanticRateLimit: 1,
  });
  try {
    const baseUrl = await gateway.start();
    const health = await (await fetch(`${baseUrl}/api/health`)).json();
    assert.equal(health.providers.semantica, "ready");
    assert.deepEqual(health.dataBoundary.localProcessors, ["Semantica"]);
    const response = await fetch(`${baseUrl}/api/admin/semantic/enrich`, { method: "POST" });
    assert.equal(response.status, 200);
    assert.equal((await response.json()).result.checkedAssets, 1);
    assert.equal(receivedRole, "owner");
    assert.deepEqual(receivedAssets, [authorizedAsset]);
    const limited = await fetch(`${baseUrl}/api/admin/semantic/enrich`, { method: "POST" });
    assert.equal(limited.status, 429);
    assert.equal(limited.headers.get("retry-after"), "60");
  } finally {
    await gateway.stop();
    await rm(directory, { recursive: true, force: true });
  }
});

test("semantic review API persists candidates and accepts one audited administrator decision", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-semantic-review-api-"));
  const dist = path.join(directory, "dist");
  await mkdir(dist);
  await writeFile(path.join(dist, "index.html"), "<!doctype html><title>intelifar</title>", "utf8");
  const gateway = await createRealAnalysisServer({
    distRoot: dist,
    databasePath: path.join(directory, "platform.sqlite"),
    config: { mineruApiKey: "m", deepseekApiKey: "d", deepseekModel: "deepseek-chat" },
    mineruClient: { async parseDocument() { throw new Error("not used"); } },
    deepseekClient: { async analyzeMarkdown() { throw new Error("not used"); } },
    semanticaClient: {
      async status() { return { state: "ready", enabled: true, engine: "Semantica", version: "0.6.0" }; },
      async enrich(assets) {
        return {
          status: "complete", engine: "Semantica", version: "0.6.0", checkedAssets: assets.length,
          duplicates: [{ assetIds: ["IP-REAL-SEM-A", "IP-REAL-SEM-B"], similarity: 0.96, confidence: 0.93, reasons: ["标题完全一致"] }],
          conflicts: [], provenance: { assets: assets.length, evidence: 0, entries: [] },
        };
      },
    },
  });
  try {
    const makePublication = (suffix, sourceName) => ({
      publicationId: `PUB-SEM-${suffix}`,
      sourceJobId: `JOB-SEM-${suffix}`,
      status: "published",
      version: "V1.0",
      publishedAt: `2026-08-12T0${suffix === "A" ? 1 : 2}:00:00.000Z`,
      document: { title: "企业知识中台报告", sourceName },
      assets: [{ id: `IP-REAL-SEM-${suffix}`, title: "企业知识中台", type: "技术方案", owner: suffix === "A" ? "产品部" : "研发部", sensitivity: "内部", confidence: 0.94, tags: ["知识治理"], wiki: { title: "企业知识中台", executiveSummary: "企业知识治理", keyMechanism: "语义检索", metrics: [], relationships: [] }, evidence: [] }],
    });
    gateway.platformStore.savePublication("WS-DEMO", makePublication("A", "a.pdf"));
    gateway.platformStore.savePublication("WS-DEMO", makePublication("B", "b.pdf"));
    const baseUrl = await gateway.start();
    const assetsBefore = await (await fetch(`${baseUrl}/api/assets`)).json();

    const checked = await fetch(`${baseUrl}/api/admin/semantic/enrich`, { method: "POST" });
    assert.equal(checked.status, 200);
    const checkedPayload = await checked.json();
    assert.equal(checkedPayload.reviews.length, 1);
    assert.equal(checkedPayload.reviews[0].status, "pending");
    assert.equal(checkedPayload.result.duplicates[0].reviewId, checkedPayload.reviews[0].id);

    const listed = await fetch(`${baseUrl}/api/admin/semantic/reviews?status=pending`);
    assert.equal(listed.status, 200);
    const { reviews } = await listed.json();
    assert.equal(reviews.length, 1);
    const reviewId = reviews[0].id;

    const invalid = await fetch(`${baseUrl}/api/admin/semantic/reviews/${reviewId}/decision`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ decision: "merged" }) });
    assert.equal(invalid.status, 400);
    const decided = await fetch(`${baseUrl}/api/admin/semantic/reviews/${reviewId}/decision`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ decision: "confirmed", reviewNote: "交由资产负责人继续治理" }) });
    assert.equal(decided.status, 200);
    assert.equal((await decided.json()).review.status, "confirmed");
    const repeated = await fetch(`${baseUrl}/api/admin/semantic/reviews/${reviewId}/decision`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ decision: "dismissed" }) });
    assert.equal(repeated.status, 409);

    const assetsAfter = await (await fetch(`${baseUrl}/api/assets`)).json();
    assert.deepEqual(assetsAfter.assets, assetsBefore.assets);
    const audit = await (await fetch(`${baseUrl}/api/audit?limit=10`)).json();
    assert.ok(audit.events.some((event) => event.action === "semantic.review_confirm" && event.objectId === reviewId && event.detail.formalKnowledgeMutation === false));
    const operations = await (await fetch(`${baseUrl}/api/admin/operations`)).json();
    assert.equal(operations.semanticReviews.find((review) => review.id === reviewId).status, "confirmed");
  } finally {
    await gateway.stop();
    await rm(directory, { recursive: true, force: true });
  }
});

test("persistent loopback mode exposes the real SMB data plane and audits actions", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-loopback-"));
  const dist = path.join(directory, "dist");
  await mkdir(dist);
  await writeFile(path.join(dist, "index.html"), "<!doctype html><title>intelifar</title>", "utf8");
  const gateway = await createRealAnalysisServer({
    distRoot: dist,
    databasePath: path.join(directory, "platform.sqlite"),
    registryRoot: path.join(directory, "publications"),
    backupRoot: path.join(directory, "backups"),
    config: { mineruApiKey: "m", deepseekApiKey: "d", deepseekModel: "deepseek-v4-flash" },
    mineruClient: { async parseDocument(file) { return { provider: "MinerU", model: "HTML", batchId: "b", fileName: file.name, markdown: "# ok" }; } },
    deepseekClient: { async analyzeMarkdown() { return { provider: "DeepSeek", model: "deepseek-v4-flash", usage: {}, analysis: { document: {}, assets: [], risks: [], wiki: {} } }; } },
  });
  try {
    const baseUrl = await gateway.start();
    const health = await (await fetch(`${baseUrl}/api/health`)).json();
    assert.deepEqual(
      { adapter: health.storage.adapter, agentTasks: health.storage.agentTasks, backups: health.storage.verifiedBackups, mode: health.auth.mode },
      { adapter: "sqlite", agentTasks: true, backups: true, mode: "loopback-persistent" },
    );
    const session = (await (await fetch(`${baseUrl}/api/session`)).json()).session;
    assert.equal(session.mode, "loopback-persistent");
    const members = (await (await fetch(`${baseUrl}/api/admin/members`)).json()).members;
    assert.deepEqual(members.map(({ id, role, status }) => ({ id, role, status })), [{ id: "USR-DEMO", role: "owner", status: "active" }]);

    const form = new FormData();
    form.append("file", new Blob(["<html>ok</html>"], { type: "text/html" }), "loopback.html");
    assert.equal((await fetch(`${baseUrl}/api/analysis`, { method: "POST", body: form })).status, 202);
    await new Promise((resolve) => setTimeout(resolve, 20));
    assert.equal(gateway.platformStore.verifyAuditChain("WS-DEMO").valid, true);
    assert.ok(gateway.platformStore.verifyAuditChain("WS-DEMO").count >= 1);

    const dashboard = await (await fetch(`${baseUrl}/api/dashboard`)).json();
    assert.equal(dashboard.dashboard.documents.total, 1);
    assert.equal(dashboard.dashboard.audit.integrity, true);
    const recorded = await fetch(`${baseUrl}/api/audit/events`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ eventType: "redaction_evidence_view", objectId: "REDACT-S1-P114-08" }),
    });
    assert.equal(recorded.status, 201);
    const rejected = await fetch(`${baseUrl}/api/audit/events`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ eventType: "owner.grant", objectId: "../../USR-OTHER" }),
    });
    assert.equal(rejected.status, 400);
    const audit = await (await fetch(`${baseUrl}/api/audit?limit=20`)).json();
    assert.equal(audit.integrity.valid, true);
    assert.equal(audit.events[0].action, "evidence.view");
    assert.equal(audit.events[0].objectId, "REDACT-S1-P114-08");
  } finally {
    await gateway.stop();
    await rm(directory, { recursive: true, force: true });
  }
});

test("only administrators can read the full workspace audit ledger", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-audit-role-"));
  const dist = path.join(directory, "dist");
  await mkdir(dist);
  await writeFile(path.join(dist, "index.html"), "<!doctype html><title>intelifar</title>", "utf8");
  const ownerEmail = "audit-owner@example.com";
  const ownerPassword = "Owner-Audit-2026!";
  const viewerEmail = "audit-viewer@example.com";
  const viewerPassword = "Viewer-Audit-2026!";
  const gateway = await createRealAnalysisServer({
    distRoot: dist,
    databasePath: path.join(directory, "platform.sqlite"),
    config: { mineruApiKey: "m", deepseekApiKey: "d", deepseekModel: "deepseek-chat" },
    auth: { required: true, secureCookies: false, workspaceId: "WS-AUDIT-ROLE", workspaceName: "审计权限空间", email: ownerEmail, password: ownerPassword, name: "审计管理员" },
    mineruClient: { async parseDocument() { throw new Error("not used"); } },
    deepseekClient: { async analyzeMarkdown() { throw new Error("not used"); } },
    semanticaClient: {
      async status() { return { state: "ready", enabled: true, engine: "Semantica", version: "0.6.0" }; },
      async enrich(assets) { return { status: "complete", engine: "Semantica", version: "0.6.0", checkedAssets: assets.length, duplicates: [], conflicts: [], provenance: { assets: assets.length, evidence: 0, entries: [] } }; },
    },
  });
  try {
    const invitation = gateway.authService.createInvitation({ workspaceId: "WS-AUDIT-ROLE", email: viewerEmail, name: "阅读成员", role: "viewer" });
    await gateway.authService.acceptInvitation({ token: invitation.token, password: viewerPassword });
    const baseUrl = await gateway.start();
    const login = async (email, password) => {
      const response = await fetch(`${baseUrl}/api/auth/login`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ email, password }) });
      assert.equal(response.status, 200);
      return response.headers.get("set-cookie").split(";")[0];
    };
    const ownerCookie = await login(ownerEmail, ownerPassword);
    const viewerCookie = await login(viewerEmail, viewerPassword);
    assert.equal((await fetch(`${baseUrl}/api/audit`, { headers: { cookie: ownerCookie } })).status, 200);
    assert.equal((await fetch(`${baseUrl}/api/audit`, { headers: { cookie: viewerCookie } })).status, 403);
    assert.equal((await fetch(`${baseUrl}/api/audit/events`, { method: "POST", headers: { cookie: viewerCookie, "content-type": "application/json" }, body: JSON.stringify({ eventType: "redaction_evidence_view", objectId: "REDACT-S1-P114-08" }) })).status, 201);
    assert.equal((await fetch(`${baseUrl}/api/audit/events`, { method: "POST", headers: { cookie: viewerCookie, "content-type": "application/json" }, body: JSON.stringify({ eventType: "audit_export", objectId: "WS-AUDIT-ROLE" }) })).status, 403);
    assert.equal((await fetch(`${baseUrl}/api/admin/semantic/enrich`, { method: "POST", headers: { cookie: ownerCookie } })).status, 200);
    assert.equal((await fetch(`${baseUrl}/api/admin/semantic/enrich`, { method: "POST", headers: { cookie: viewerCookie } })).status, 403);
    assert.equal((await fetch(`${baseUrl}/api/admin/semantic/reviews`, { headers: { cookie: ownerCookie } })).status, 200);
    assert.equal((await fetch(`${baseUrl}/api/admin/semantic/reviews`, { headers: { cookie: viewerCookie } })).status, 403);
    assert.equal((await fetch(`${baseUrl}/api/admin/semantic/reviews/SEMREV-AAAAAAAAAAAAAAAAAAAAAAAA/decision`, { method: "POST", headers: { cookie: viewerCookie, "content-type": "application/json" }, body: JSON.stringify({ decision: "dismissed" }) })).status, 403);
  } finally {
    await gateway.stop();
    await rm(directory, { recursive: true, force: true });
  }
});

test("routes editor Wiki changes through administrator publication approval", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-wiki-review-"));
  const dist = path.join(directory, "dist");
  await mkdir(dist);
  await writeFile(path.join(dist, "index.html"), "<!doctype html><title>intelifar</title>", "utf8");
  const ownerEmail = "review-owner@example.com";
  const ownerPassword = "Owner-Review-2026!";
  const editorEmail = "review-editor@example.com";
  const editorPassword = "Editor-Review-2026!";
  const viewerEmail = "review-viewer@example.com";
  const viewerPassword = "Viewer-Review-2026!";
  const workspaceId = "WS-WIKI-REVIEW";
  const gateway = await createRealAnalysisServer({
    distRoot: dist,
    databasePath: path.join(directory, "platform.sqlite"),
    config: { mineruApiKey: "m", deepseekApiKey: "d", deepseekModel: "deepseek-chat" },
    auth: { required: true, secureCookies: false, workspaceId, workspaceName: "发布审批空间", email: ownerEmail, password: ownerPassword, name: "审批管理员" },
    mineruClient: { async parseDocument() { throw new Error("not used"); } },
    deepseekClient: { async analyzeMarkdown() { throw new Error("not used"); } },
  });
  try {
    const editorInvitation = gateway.authService.createInvitation({ workspaceId, email: editorEmail, name: "知识编辑", role: "editor" });
    const viewerInvitation = gateway.authService.createInvitation({ workspaceId, email: viewerEmail, name: "只读成员", role: "viewer" });
    await gateway.authService.acceptInvitation({ token: editorInvitation.token, password: editorPassword });
    await gateway.authService.acceptInvitation({ token: viewerInvitation.token, password: viewerPassword });
    gateway.platformStore.savePublication(workspaceId, {
      publicationId: "PUB-REVIEW-1", sourceJobId: "JOB-REVIEW-1", status: "published", version: "V1.0", publishedAt: "2026-08-11T08:00:00.000Z",
      document: { title: "审批测试报告", sourceName: "review.pdf" },
      assets: [
        { id: "IP-REAL-ABCD1234", title: "发布审批知识", type: "业务规则", summary: "初始摘要", version: "V1.0", owner: "知识部", sensitivity: "内部", evidence: [], wiki: { title: "发布审批知识", executiveSummary: "初始摘要", keyMechanism: "初始机制", metrics: [], relationships: [] } },
        { id: "IP-REAL-ABCD5678", title: "批量治理知识", type: "业务规则", summary: "待确认摘要", version: "V1.0", owner: "待确权", sensitivity: "待复核", evidence: [], wiki: { title: "批量治理知识", executiveSummary: "待确认摘要", keyMechanism: "待确认机制", metrics: [], relationships: [] } },
      ],
    });
    const baseUrl = await gateway.start();
    const login = async (email, password) => {
      const response = await fetch(`${baseUrl}/api/auth/login`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ email, password }) });
      assert.equal(response.status, 200);
      return response.headers.get("set-cookie").split(";")[0];
    };
    const ownerCookie = await login(ownerEmail, ownerPassword);
    const editorCookie = await login(editorEmail, editorPassword);
    const viewerCookie = await login(viewerEmail, viewerPassword);
    assert.equal((await fetch(`${baseUrl}/api/assets/IP-REAL-ABCD1234/metadata`, { method: "PATCH", headers: { cookie: viewerCookie, "content-type": "application/json" }, body: JSON.stringify({ owner: "法务部", sensitivity: "机密" }) })).status, 403);
    const governed = await fetch(`${baseUrl}/api/assets/IP-REAL-ABCD1234/metadata`, { method: "PATCH", headers: { cookie: editorCookie, "content-type": "application/json" }, body: JSON.stringify({ owner: "法务部", sensitivity: "机密" }) });
    assert.equal(governed.status, 200);
    assert.deepEqual({ owner: (await governed.json()).asset.owner, sensitivity: (await (await fetch(`${baseUrl}/api/assets/IP-REAL-ABCD1234`, { headers: { cookie: editorCookie } })).json()).asset.sensitivity }, { owner: "法务部", sensitivity: "机密" });
    assert.equal((await fetch(`${baseUrl}/api/assets/IP-REAL-ABCD1234/metadata`, { method: "PATCH", headers: { cookie: editorCookie, "content-type": "application/json" }, body: JSON.stringify({ owner: "待确权", sensitivity: "绝密" }) })).status, 400);
    const batchBody = { assetIds: ["IP-REAL-ABCD1234", "IP-REAL-ABCD5678"], owner: "产品部", sensitivity: "内部" };
    assert.equal((await fetch(`${baseUrl}/api/assets/metadata`, { method: "PATCH", headers: { cookie: viewerCookie, "content-type": "application/json" }, body: JSON.stringify(batchBody) })).status, 403);
    assert.equal((await fetch(`${baseUrl}/api/assets/metadata`, { method: "PATCH", headers: { cookie: editorCookie, "content-type": "application/json" }, body: JSON.stringify({ ...batchBody, assetIds: Array.from({ length: 51 }, (_, index) => `IP-REAL-LIMIT${index}`) }) })).status, 400);
    const batchGoverned = await fetch(`${baseUrl}/api/assets/metadata`, { method: "PATCH", headers: { cookie: editorCookie, "content-type": "application/json" }, body: JSON.stringify(batchBody) });
    assert.equal(batchGoverned.status, 200);
    assert.equal((await batchGoverned.json()).assets.length, 2);
    const draft = { baseVersion: "V1.0", title: "发布审批知识（复核版）", executiveSummary: "经业务复核的摘要", keyMechanism: "管理员批准后形成新版本", changeNote: "补充审批结论" };

    assert.equal((await fetch(`${baseUrl}/api/wiki/IP-REAL-ABCD1234`, { method: "PATCH", headers: { cookie: editorCookie, "content-type": "application/json" }, body: JSON.stringify(draft) })).status, 403);
    const submitted = await fetch(`${baseUrl}/api/wiki/IP-REAL-ABCD1234/reviews`, { method: "POST", headers: { cookie: editorCookie, "content-type": "application/json" }, body: JSON.stringify(draft) });
    assert.equal(submitted.status, 201);
    const review = (await submitted.json()).review;
    assert.equal(review.status, "pending");
    assert.equal((await (await fetch(`${baseUrl}/api/wiki/IP-REAL-ABCD1234`, { headers: { cookie: editorCookie } })).json()).wiki.version, "V1.0");
    assert.equal((await fetch(`${baseUrl}/api/wiki/reviews?status=pending`, { headers: { cookie: viewerCookie } })).status, 403);
    assert.equal((await (await fetch(`${baseUrl}/api/wiki/reviews?status=pending`, { headers: { cookie: editorCookie } })).json()).reviews.length, 1);

    const approved = await fetch(`${baseUrl}/api/wiki/reviews/${review.id}/decision`, { method: "POST", headers: { cookie: ownerCookie, "content-type": "application/json" }, body: JSON.stringify({ decision: "approved", reviewNote: "原文依据充分" }) });
    assert.equal(approved.status, 200);
    assert.equal((await approved.json()).wiki.version, "V1.1");
    const audit = await (await fetch(`${baseUrl}/api/audit?limit=20`, { headers: { cookie: ownerCookie } })).json();
    assert.ok(audit.events.some((event) => event.action === "asset.metadata_update"));
    assert.ok(audit.events.some((event) => event.action === "asset.metadata_batch_update" && event.detail.count === 2));
    assert.ok(audit.events.some((event) => event.action === "wiki.review_submit"));
    assert.ok(audit.events.some((event) => event.action === "wiki.review_approve"));
  } finally {
    await gateway.stop();
    await rm(directory, { recursive: true, force: true });
  }
});
