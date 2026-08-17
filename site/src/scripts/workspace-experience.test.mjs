import test from "node:test";
import assert from "node:assert/strict";
import { auditBusinessCategory, deriveWorkspaceActions, presentWorkspaceSearchResults } from "./workspace-experience.mjs";

test("derives only role-actionable workspace items from authoritative state", () => {
  const input = {
    role: "admin",
    assets: [
      { id: "IP-A", title: "安全模型", owner: "待确权", sensitivity: "待复核" },
      { id: "IP-B", title: "检索路由", owner: "研发部", sensitivity: "内部" },
    ],
    graph: { meta: { proposed: 3 } },
    jobs: [{ id: "JOB-1", status: "failed", documentName: "合同.pdf" }],
    invitations: [{ id: "INV-1", status: "pending" }],
    wikiReviews: [{ id: "WREV-1", assetId: "IP-B", assetTitle: "检索路由", status: "pending", submittedBy: { name: "知识编辑" } }],
    semanticReviews: [{ id: "SEMREV-1", kind: "duplicate", status: "pending", payload: { title: "安全模型" } }],
  };
  const actions = deriveWorkspaceActions(input);
  assert.deepEqual(actions.map((item) => item.id), ["wiki-review:WREV-1", "failed-jobs", "semantic-review", "asset-governance", "relationship-review", "member-invitations"]);
  assert.equal(actions[0].destination, "wiki");
  assert.equal(actions[0].canDecide, true);
  assert.equal(actions[0].ownerLabel, "空间管理员");
  assert.equal(actions[0].dueLabel, "建议今天完成");
  assert.match(actions[2].title, /1 条语义资产建议/);
  assert.equal(actions[2].destination, "system");
  assert.match(actions[3].title, /1 项资产/);
  assert.deepEqual(actions[3].assetIds, ["IP-A"]);
  assert.equal(actions[3].ownerLabel, "知识编辑者");
  assert.equal(actions[3].dueLabel, "建议三天内完成");
  assert.match(actions[4].title, /3 条关系/);

  const viewerActions = deriveWorkspaceActions({ ...input, role: "viewer" });
  assert.deepEqual(viewerActions, []);
});

test("presents duplicate-free cross-Wiki results with the matching business context", () => {
  const sharedDocument = { markdownSha256: "hash-a", title: "企业知识平台报告" };
  const results = [
    { id: "IP-NEW", title: "安全模型", type: "技术方案", publishedAt: "2026-08-11T10:00:00.000Z", document: sharedDocument, summary: "默认摘要", wiki: { executiveSummary: "通过不可逆脱敏保护客户数据", keyMechanism: "审批后发布" }, evidence: [] },
    { id: "IP-OLD", title: "安全模型", type: "技术方案", publishedAt: "2026-08-10T10:00:00.000Z", document: sharedDocument, summary: "旧记录", wiki: { executiveSummary: "旧摘要" }, evidence: [] },
    { id: "IP-OTHER", title: "检索路由", type: "技术方案", publishedAt: "2026-08-11T09:00:00.000Z", document: { markdownSha256: "hash-b", title: "检索白皮书" }, summary: "支持跨 Wiki 检索", wiki: {}, evidence: [] },
  ];
  const presented = presentWorkspaceSearchResults(results, "脱敏");
  assert.equal(presented.length, 1);
  assert.equal(presented[0].assetId, "IP-NEW");
  assert.equal(presented[0].matchLabel, "Wiki 摘要");
  assert.match(presented[0].snippet, /脱敏/);
  assert.equal(presented[0].recordCount, 2);
});

test("ranks exact Wiki titles before evidence-only matches", () => {
  const results = [
    { id: "IP-EVIDENCE", title: "访问控制", publishedAt: "2026-08-11T12:00:00.000Z", wiki: { executiveSummary: "权限策略" }, evidence: [{ quote: "客户尽调清单需要复核", section: "附件" }] },
    { id: "IP-TITLE", title: "客户尽调清单", publishedAt: "2026-08-10T12:00:00.000Z", wiki: { title: "客户尽调清单", executiveSummary: "准备对外材料" }, evidence: [] },
  ];
  const presented = presentWorkspaceSearchResults(results, "客户尽调清单");
  assert.deepEqual(presented.map((item) => item.assetId), ["IP-TITLE", "IP-EVIDENCE"]);
  assert.equal(presented[0].matchLabel, "Wiki 标题");
  assert.ok(presented[0].score > presented[1].score);
});

test("keeps governance and search complete across merged source records", () => {
  const canonical = {
    id: "IP-NEW",
    title: "客户知识规则",
    type: "业务规则",
    owner: "知识部",
    sensitivity: "内部",
    publishedAt: "2026-08-11T12:00:00.000Z",
    document: { markdownSha256: "shared-document" },
  };
  const duplicate = {
    ...canonical,
    id: "IP-OLD",
    owner: "待确权",
    sensitivity: "待复核",
    publishedAt: "2026-08-10T12:00:00.000Z",
    evidence: [{ section: "客户承诺", quote: "交付前必须完成业务负责人复核" }],
  };
  const grouped = { ...canonical, sourceRecordIds: [canonical.id, duplicate.id], duplicateRecords: [duplicate], duplicateCount: 1 };
  const actions = deriveWorkspaceActions({ role: "editor", assets: [grouped] });
  assert.equal(actions[0].id, "asset-governance");
  assert.match(actions[0].title, /1 项资产/);

  const presented = presentWorkspaceSearchResults([canonical, duplicate], "业务负责人复核");
  assert.equal(presented.length, 1);
  assert.equal(presented[0].assetId, "IP-NEW");
  assert.equal(presented[0].matchLabel, "原文依据");
  assert.equal(presented[0].recordCount, 2);
});

test("maps technical event names into four business operation categories", () => {
  assert.equal(auditBusinessCategory("wiki.review_approve"), "content");
  assert.equal(auditBusinessCategory("relationship.confirm"), "content");
  assert.equal(auditBusinessCategory("member.role_update"), "access");
  assert.equal(auditBusinessCategory("share.access"), "access");
  assert.equal(auditBusinessCategory("agent.task_blocked"), "security");
  assert.equal(auditBusinessCategory("backup.verify"), "operations");
});
