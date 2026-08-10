import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { hashPassword } from "./auth-service.mjs";
import { createRealAnalysisServer } from "./real-analysis-server.mjs";

function providers() {
  return {
    mineruClient: { async parseDocument(file) { return { provider: "MinerU", model: "MinerU-HTML", batchId: "batch-smb", traceId: "trace-smb", fileName: file.name, markdown: "# 可恢复解析\n企业证据" }; } },
    deepseekClient: { async analyzeMarkdown() { return { provider: "DeepSeek", model: "deepseek-chat", responseId: "chat-smb", usage: { totalTokens: 12 }, analysis: { document: { title: "SMB Report", summary: "Workspace summary", category: "技术报告" }, assets: [{ id: "IP-1", title: "Workspace Wiki", type: "技术方案", summary: "Traceable", confidence: 0.97, tags: ["SMB"], source_quotes: [{ quote: "企业证据", section: "概览" }] }], risks: [], wiki: { executive_summary: "初始 Wiki 摘要", key_mechanism: "初始机制", metrics: [], relationships: [] } } }; } },
  };
}

async function login(baseUrl, email, password) {
  const response = await fetch(`${baseUrl}/api/auth/login`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ email, password }) });
  return { response, cookie: response.headers.get("set-cookie")?.split(";")[0] || "" };
}

test("protects the SMB API, scopes assets by workspace, and versions Wiki edits", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-smb-server-"));
  const dist = path.join(directory, "dist");
  await mkdir(dist);
  await writeFile(path.join(dist, "index.html"), "<!doctype html><title>intelifar</title>", "utf8");
  const gateway = await createRealAnalysisServer({
    distRoot: dist,
    databasePath: path.join(directory, "platform.sqlite"),
    uploadRoot: path.join(directory, "uploads"),
    config: { mineruApiKey: "mineru-private", deepseekApiKey: "deepseek-private", deepseekModel: "deepseek-chat" },
    ...providers(),
    auth: { required: true, workspaceId: "WS-A", workspaceName: "澜图科技", email: "owner@example.com", password: "Correct-Horse-2026", name: "林越" },
  });
  try {
    const baseUrl = await gateway.start();
    assert.equal((await fetch(`${baseUrl}/api/assets`)).status, 401);
    const health = await (await fetch(`${baseUrl}/api/health`)).json();
    assert.equal(health.auth.required, true);
    assert.equal(health.storage.adapter, "sqlite");

    const ownerLogin = await login(baseUrl, "owner@example.com", "Correct-Horse-2026");
    assert.equal(ownerLogin.response.status, 200);
    assert.match(ownerLogin.cookie, /^intelifar_session=/);
    const authHeaders = { cookie: ownerLogin.cookie };
    const session = await (await fetch(`${baseUrl}/api/session`, { headers: authHeaders })).json();
    assert.equal(session.session.workspace.id, "WS-A");
    assert.equal(session.session.user.role, "owner");

    const form = new FormData();
    form.append("file", new Blob(["<!doctype html><html><body>IP</body></html>"], { type: "text/html" }), "report.html");
    form.append("category", "技术报告");
    const submitted = await fetch(`${baseUrl}/api/analysis`, { method: "POST", headers: authHeaders, body: form });
    assert.equal(submitted.status, 202);
    const { job } = await submitted.json();
    await gateway.analysisService.whenSettled(job.id, "WS-A");
    const publishedResponse = await fetch(`${baseUrl}/api/analysis/${job.id}/publish`, { method: "POST", headers: { ...authHeaders, "content-type": "application/json" }, body: JSON.stringify({ owner: "研发部", sensitivity: "内部" }) });
    assert.equal(publishedResponse.status, 201);
    const publication = (await publishedResponse.json()).publication;
    const assetId = publication.assets[0].id;

    const originalWiki = (await (await fetch(`${baseUrl}/api/wiki/${assetId}`, { headers: authHeaders })).json()).wiki;
    assert.equal(originalWiki.version, "V1.0");
    const edited = await fetch(`${baseUrl}/api/wiki/${assetId}`, {
      method: "PATCH",
      headers: { ...authHeaders, "content-type": "application/json" },
      body: JSON.stringify({ baseVersion: "V1.0", title: "Workspace Wiki 复核版", executiveSummary: "人工复核摘要", keyMechanism: "人工复核机制", changeNote: "业务复核" }),
    });
    assert.equal(edited.status, 200);
    assert.equal((await edited.json()).wiki.version, "V1.1");
    const versions = await (await fetch(`${baseUrl}/api/wiki/${assetId}/versions`, { headers: authHeaders })).json();
    assert.deepEqual(versions.versions.map((item) => item.version), ["V1.1", "V1.0"]);
    const stale = await fetch(`${baseUrl}/api/wiki/${assetId}`, { method: "PATCH", headers: { ...authHeaders, "content-type": "application/json" }, body: JSON.stringify({ baseVersion: "V1.0", title: "stale", executiveSummary: "stale", keyMechanism: "stale" }) });
    assert.equal(stale.status, 409);

    await gateway.authService.bootstrap({ workspaceId: "WS-B", workspaceName: "乙公司", email: "owner-b@example.com", password: "Correct-Horse-B-2026", name: "乙方" });
    const otherLogin = await login(baseUrl, "owner-b@example.com", "Correct-Horse-B-2026");
    assert.equal((await fetch(`${baseUrl}/api/assets/${assetId}`, { headers: { cookie: otherLogin.cookie } })).status, 404);

    const viewerHash = await hashPassword("Viewer-Horse-2026");
    gateway.platformStore.createUser({ id: "USR-VIEWER", workspaceId: "WS-A", email: "viewer@example.com", name: "访客", role: "viewer", passwordHash: viewerHash });
    const viewerLogin = await login(baseUrl, "viewer@example.com", "Viewer-Horse-2026");
    const viewerForm = new FormData();
    viewerForm.append("file", new Blob(["<!doctype html><html></html>"], { type: "text/html" }), "viewer.html");
    viewerForm.append("category", "技术报告");
    assert.equal((await fetch(`${baseUrl}/api/analysis`, { method: "POST", headers: { cookie: viewerLogin.cookie }, body: viewerForm })).status, 403);

    assert.equal((await fetch(`${baseUrl}/api/admin/operations`, { headers: { cookie: viewerLogin.cookie } })).status, 403);
    const operations = await (await fetch(`${baseUrl}/api/admin/operations`, { headers: authHeaders })).json();
    assert.equal(operations.storage.integrity, "ok");
    assert.equal(operations.audit.valid, true);
    assert.equal(operations.scanner.mode, "built-in-preflight");
    const backupResponse = await fetch(`${baseUrl}/api/admin/backups`, { method: "POST", headers: authHeaders });
    assert.equal(backupResponse.status, 201);
    const backup = (await backupResponse.json()).backup;
    assert.equal(backup.integrity, "ok");
    assert.equal(backup.path, undefined);
    const verifyResponse = await fetch(`${baseUrl}/api/admin/backups/${backup.id}/verify`, { method: "POST", headers: authHeaders });
    assert.equal(verifyResponse.status, 200);
    assert.equal((await verifyResponse.json()).backup.integrity, "ok");

    const logout = await fetch(`${baseUrl}/api/auth/logout`, { method: "POST", headers: authHeaders });
    assert.equal(logout.status, 204);
    assert.match(logout.headers.get("set-cookie"), /Max-Age=0/);
    for (let attempt = 0; attempt < 3; attempt += 1) assert.equal((await login(baseUrl, "missing@example.com", "Wrong-Horse-2026")).response.status, 401);
    const throttled = await login(baseUrl, "missing@example.com", "Wrong-Horse-2026");
    assert.equal(throttled.response.status, 429);
    assert.equal(throttled.response.headers.get("retry-after"), "60");
    assert.equal(gateway.platformStore.verifyAuditChain("WS-A").valid, true);
  } finally {
    await gateway.stop();
    await rm(directory, { recursive: true, force: true });
  }
});

test("onboards members and enforces revocable double-secret Wiki sharing", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-collaboration-server-"));
  const dist = path.join(directory, "dist");
  await mkdir(dist);
  await writeFile(path.join(dist, "index.html"), "<!doctype html><title>intelifar</title>", "utf8");
  const gateway = await createRealAnalysisServer({
    distRoot: dist,
    databasePath: path.join(directory, "platform.sqlite"),
    config: { mineruApiKey: "m", deepseekApiKey: "d", deepseekModel: "deepseek-chat" },
    ...providers(),
    auth: { required: true, workspaceId: "WS-COLLAB", workspaceName: "协作空间", email: "owner-collab@example.com", password: "Owner-Collab-2026", name: "空间所有者" },
  });
  gateway.platformStore.savePublication("WS-COLLAB", {
    publicationId: "PUB-COLLAB",
    sourceJobId: "JOB-COLLAB",
    status: "published",
    version: "V1.0",
    publishedAt: "2026-08-10T08:00:00.000Z",
    document: { title: "机密技术报告", sourceName: "secret.pdf", markdownPreview: "不得公开的原文" },
    assets: [{ id: "IP-REAL-COLLAB", title: "协作技术", version: "V1.0", summary: "内部摘要", wiki: { title: "协作技术 Wiki", executiveSummary: "脱敏摘要", keyMechanism: "脱敏机制", metrics: [], relationships: [] }, evidence: [{ quote: "不得公开的证据" }] }],
  });
  try {
    const baseUrl = await gateway.start();
    const ownerLogin = await login(baseUrl, "owner-collab@example.com", "Owner-Collab-2026");
    const ownerHeaders = { cookie: ownerLogin.cookie, "content-type": "application/json" };
    const invitationResponse = await fetch(`${baseUrl}/api/admin/invitations`, { method: "POST", headers: ownerHeaders, body: JSON.stringify({ email: "editor-collab@example.com", name: "协作编辑", role: "editor", expires: "7d" }) });
    assert.equal(invitationResponse.status, 201);
    const invitation = await invitationResponse.json();
    assert.match(invitation.token, /^[A-Za-z0-9_-]{40,}$/);
    const memberListBefore = await (await fetch(`${baseUrl}/api/admin/members`, { headers: { cookie: ownerLogin.cookie } })).json();
    assert.doesNotMatch(JSON.stringify(memberListBefore), new RegExp(invitation.token));
    assert.equal((await fetch(`${baseUrl}/api/public/invitations/inspect`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ token: invitation.token }) })).status, 200);
    const acceptedResponse = await fetch(`${baseUrl}/api/public/invitations/accept`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ token: invitation.token, password: "Editor-Collab-2026" }) });
    assert.equal(acceptedResponse.status, 201);
    const member = (await acceptedResponse.json()).member;
    const editorLogin = await login(baseUrl, "editor-collab@example.com", "Editor-Collab-2026");
    assert.equal(editorLogin.response.status, 200);
    assert.equal((await fetch(`${baseUrl}/api/admin/members`, { headers: { cookie: editorLogin.cookie } })).status, 403);

    const shareResponse = await fetch(`${baseUrl}/api/shares`, { method: "POST", headers: ownerHeaders, body: JSON.stringify({ assetId: "IP-REAL-COLLAB", recipient: "partner@example.com", expires: "7d" }) });
    assert.equal(shareResponse.status, 201);
    const secureShare = await shareResponse.json();
    const storedShare = gateway.platformStore.unsafeDatabaseForTests.prepare("SELECT token_hash, access_code_hash FROM secure_shares WHERE id = ?").get(secureShare.share.id);
    assert.notEqual(storedShare.token_hash, secureShare.token);
    assert.notEqual(storedShare.access_code_hash, secureShare.accessCode);
    const inspected = await fetch(`${baseUrl}/api/public/shares/inspect`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ token: secureShare.token }) });
    assert.equal(inspected.status, 200);
    const badAccess = await fetch(`${baseUrl}/api/public/shares/access`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ token: secureShare.token, accessCode: "wrong" }) });
    assert.equal(badAccess.status, 404);
    const access = await fetch(`${baseUrl}/api/public/shares/access`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ token: secureShare.token, accessCode: secureShare.accessCode }) });
    assert.equal(access.status, 200);
    const publicWiki = await access.json();
    assert.equal(publicWiki.wiki.executiveSummary, "脱敏摘要");
    assert.doesNotMatch(JSON.stringify(publicWiki), /不得公开|secret\.pdf/);
    assert.equal((await fetch(`${baseUrl}/api/shares/${secureShare.share.id}`, { method: "DELETE", headers: { cookie: ownerLogin.cookie } })).status, 200);
    assert.equal((await fetch(`${baseUrl}/api/public/shares/access`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ token: secureShare.token, accessCode: secureShare.accessCode }) })).status, 404);

    const disableResponse = await fetch(`${baseUrl}/api/admin/members/${member.id}`, { method: "PATCH", headers: ownerHeaders, body: JSON.stringify({ role: "viewer", status: "disabled" }) });
    assert.equal(disableResponse.status, 200);
    assert.equal((await fetch(`${baseUrl}/api/session`, { headers: { cookie: editorLogin.cookie } })).status, 401);
    assert.equal(gateway.platformStore.verifyAuditChain("WS-COLLAB").valid, true);
  } finally {
    await gateway.stop();
    await rm(directory, { recursive: true, force: true });
  }
});
