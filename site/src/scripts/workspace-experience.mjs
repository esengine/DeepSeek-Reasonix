import { groupAssetsForDisplay } from "./asset-presentation.mjs";

const writableRoles = new Set(["owner", "admin", "editor"]);
const adminRoles = new Set(["owner", "admin"]);

function text(value) {
  return String(value ?? "").normalize("NFKC").replace(/\s+/g, " ").trim();
}

function includes(value, query) {
  return text(value).toLocaleLowerCase("zh-CN").includes(query);
}

function priorityRank(priority) {
  return ({ urgent: 0, high: 1, normal: 2, low: 3 })[priority] ?? 9;
}

export function deriveWorkspaceActions({ role = "viewer", assets = [], graph = {}, jobs = [], invitations = [], wikiReviews = [], semanticReviews = [] } = {}) {
  if (!writableRoles.has(role)) return [];
  const actions = [];

  if (adminRoles.has(role)) {
    for (const review of wikiReviews.filter((item) => item.status === "pending")) {
      actions.push({
        id: `wiki-review:${review.id}`,
        category: "content",
        priority: "urgent",
        title: `审批 Wiki 更新：${review.assetTitle || review.assetId}`,
        detail: `${review.submittedBy?.name || "知识编辑者"}提交 · 基于 ${review.baseVersion || "当前版本"}`,
        destination: "wiki",
        targetId: review.assetId,
        reviewId: review.id,
        canDecide: true,
        ownerLabel: "空间管理员",
        dueLabel: "建议今天完成",
      });
    }
  }

  const failedJobs = jobs.filter((job) => /failed|blocked|失败|拦截/i.test(String(job.status || "")));
  if (adminRoles.has(role) && failedJobs.length) {
    actions.push({ id: "failed-jobs", category: "operations", priority: "high", title: `处理 ${failedJobs.length} 个异常文档任务`, detail: "检查失败原因后重试或保留记录", destination: "system", canDecide: false, ownerLabel: "空间管理员", dueLabel: "建议今天完成" });
  }

  const pendingSemanticReviews = semanticReviews.filter((review) => review.status === "pending");
  if (adminRoles.has(role) && pendingSemanticReviews.length) {
    actions.push({ id: "semantic-review", category: "governance", priority: "high", title: `复核 ${pendingSemanticReviews.length} 条语义资产建议`, detail: "确认是否需要后续治理，或保留为独立记录", destination: "system", canDecide: false, ownerLabel: "空间管理员", dueLabel: "建议今天完成" });
  }

  const pendingAssets = assets.filter((asset) => [asset, ...(asset.duplicateRecords || [])].some((record) => [record.owner, record.sensitivity, record.status].some((value) => /待确权|待认领|待复核|needs_review/i.test(String(value || "")))));
  if (pendingAssets.length) {
    actions.push({ id: "asset-governance", category: "governance", priority: "high", title: `确认 ${pendingAssets.length} 项资产的权属与密级`, detail: "确认后再用于正式分享或跨部门复用", destination: "assets", canDecide: false, ownerLabel: "知识编辑者", dueLabel: "建议三天内完成", assetIds: pendingAssets.map((asset) => asset.id) });
  }

  const proposed = Number(graph?.meta?.proposed ?? 0);
  if (proposed > 0) {
    actions.push({ id: "relationship-review", category: "governance", priority: "normal", title: `复核 ${proposed} 条关系`, detail: "逐条确认资产之间的依赖、实现或引用关系", destination: "assets", canDecide: false, ownerLabel: "知识编辑者", dueLabel: "建议三天内完成" });
  }

  const pendingInvitations = invitations.filter((invitation) => invitation.status === "pending");
  if (adminRoles.has(role) && pendingInvitations.length) {
    actions.push({ id: "member-invitations", category: "access", priority: "normal", title: `跟进 ${pendingInvitations.length} 个待加入成员`, detail: "确认邀请是否仍需保留或撤销", destination: "members", canDecide: false, ownerLabel: "空间管理员", dueLabel: "建议三天内完成" });
  }

  return actions.sort((left, right) => priorityRank(left.priority) - priorityRank(right.priority) || left.title.localeCompare(right.title, "zh-CN"));
}

function assetLineage(asset) {
  return [asset, ...(Array.isArray(asset.duplicateRecords) ? asset.duplicateRecords : [])];
}

function lineageValue(asset, selector) {
  return [...new Set(assetLineage(asset).map(selector).flat().map(text).filter(Boolean))].join(" · ");
}

function matchingContext(asset, normalizedQuery) {
  const candidates = [
    ["Wiki 标题", lineageValue(asset, (record) => record.wiki?.title || record.title)],
    ["Wiki 摘要", lineageValue(asset, (record) => record.wiki?.executiveSummary)],
    ["核心机制", lineageValue(asset, (record) => record.wiki?.keyMechanism)],
    ["资产摘要", lineageValue(asset, (record) => record.summary)],
    ["来源文档", lineageValue(asset, (record) => `${record.document?.title || ""} ${record.document?.sourceName || ""}`)],
    ["标签", lineageValue(asset, (record) => Array.isArray(record.tags) ? record.tags : [])],
    ["原文依据", lineageValue(asset, (record) => Array.isArray(record.evidence) ? record.evidence.map((item) => `${item.section || ""} ${item.quote || ""}`) : [])],
  ];
  return candidates.find(([, value]) => includes(value, normalizedQuery)) || candidates.find(([, value]) => text(value)) || ["Wiki", asset.title];
}

function relevanceScore(asset, normalizedQuery) {
  const records = assetLineage(asset);
  const titles = records.flatMap((record) => [record.wiki?.title, record.title]).map((value) => text(value).toLocaleLowerCase("zh-CN"));
  if (titles.some((value) => value === normalizedQuery)) return 100;
  if (titles.some((value) => value.includes(normalizedQuery))) return 90;
  if (records.some((record) => text(record.id).toLocaleLowerCase("zh-CN") === normalizedQuery)) return 85;
  if (records.some((record) => [record.type, ...(record.tags || [])].some((value) => includes(value, normalizedQuery)))) return 72;
  if (records.some((record) => includes(record.wiki?.executiveSummary, normalizedQuery))) return 66;
  if (records.some((record) => includes(record.wiki?.keyMechanism, normalizedQuery))) return 62;
  if (records.some((record) => includes(record.summary, normalizedQuery))) return 56;
  if (records.some((record) => includes(`${record.document?.title || ""} ${record.document?.sourceName || ""}`, normalizedQuery))) return 46;
  if (records.some((record) => (record.evidence || []).some((item) => includes(`${item.section || ""} ${item.quote || ""}`, normalizedQuery)))) return 36;
  return records.some((record) => includes(record.owner, normalizedQuery)) ? 30 : 0;
}

export function presentWorkspaceSearchResults(records = [], query = "", limit = 8) {
  const normalizedQuery = text(query).toLocaleLowerCase("zh-CN");
  if (!normalizedQuery) return [];
  return groupAssetsForDisplay(records).assets
    .map((asset) => ({ asset, score: relevanceScore(asset, normalizedQuery) }))
    .filter((item) => item.score > 0)
    .sort((left, right) => right.score - left.score || String(right.asset.publishedAt || "").localeCompare(String(left.asset.publishedAt || "")) || String(left.asset.title || "").localeCompare(String(right.asset.title || ""), "zh-CN"))
    .slice(0, Math.max(1, Number(limit) || 8)).map(({ asset, score }) => {
    const [matchLabel, value] = matchingContext(asset, normalizedQuery);
    return {
      assetId: asset.id,
      title: text(asset.wiki?.title || asset.title) || "未命名 Wiki",
      type: text(asset.type) || "知识资产",
      sourceTitle: text(asset.document?.title || asset.document?.sourceName) || "企业知识空间",
      matchLabel,
      snippet: text(value).slice(0, 180),
      recordCount: Number(asset.duplicateCount || 0) + 1,
      score,
    };
  });
}

export function auditBusinessCategory(action = "") {
  const value = String(action);
  if (/block|deny|reject|failed|security_scan|安全|拦截|拒绝/i.test(value) && !/^relationship\.reject$/i.test(value)) return "security";
  if (/^(?:member|share)\.|invitation|permission|role/i.test(value)) return "access";
  if (/^(?:backup|audit)\.|operations|health|restore/i.test(value)) return "operations";
  return "content";
}
