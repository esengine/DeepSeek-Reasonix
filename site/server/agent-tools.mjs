const EDIT_ROLES = new Set(["owner", "admin", "editor"]);

function toolError(code, message) {
  const error = new Error(message);
  error.code = code;
  return error;
}

function safeEvidence(evidence) {
  return evidence ? {
    id: String(evidence.id),
    assetId: evidence.assetId ? String(evidence.assetId) : undefined,
    section: String(evidence.section || evidence.locator || "来源文档"),
    quote: String(evidence.quote || "").slice(0, 1_200),
    verified: evidence.verified !== false,
    documentName: evidence.documentName ? String(evidence.documentName).slice(0, 240) : undefined,
  } : null;
}

function safeAsset(asset, options = {}) {
  if (!asset) return null;
  const result = {
    id: String(asset.id),
    title: String(asset.title || "未命名资产").slice(0, 300),
    type: String(asset.type || "知识资产").slice(0, 100),
    summary: String(asset.summary || "").slice(0, 2_000),
    owner: String(asset.owner || "待确权").slice(0, 100),
    sensitivity: String(asset.sensitivity || "待复核").slice(0, 60),
    status: String(asset.status || "已发布").slice(0, 60),
    version: String(asset.version || "V1.0").slice(0, 40),
    confidence: Math.max(0, Math.min(1, Number(asset.confidence) || 0)),
    tags: (Array.isArray(asset.tags) ? asset.tags : []).map((tag) => String(tag).slice(0, 80)).slice(0, 12),
  };
  if (options.includeEvidence) result.evidence = (Array.isArray(asset.evidence) ? asset.evidence : []).map(safeEvidence).filter(Boolean).slice(0, 20);
  return result;
}

function safeWiki(wiki) {
  return wiki ? {
    assetId: String(wiki.assetId),
    version: String(wiki.version || "V1.0"),
    title: String(wiki.title || "未命名 Wiki").slice(0, 300),
    executiveSummary: String(wiki.executiveSummary || "").slice(0, 4_000),
    keyMechanism: String(wiki.keyMechanism || "").slice(0, 8_000),
    metrics: (Array.isArray(wiki.metrics) ? wiki.metrics : []).slice(0, 12),
    relationships: (Array.isArray(wiki.relationships) ? wiki.relationships : []).slice(0, 20),
    evidence: (Array.isArray(wiki.evidence) ? wiki.evidence : []).map(safeEvidence).filter(Boolean).slice(0, 20),
  } : null;
}

function safeGraph(graph) {
  return {
    nodes: (Array.isArray(graph?.nodes) ? graph.nodes : []).map((node) => safeAsset(node)).slice(0, 60),
    edges: (Array.isArray(graph?.edges) ? graph.edges : []).map((edge) => ({
      id: String(edge.id),
      sourceAssetId: String(edge.sourceAssetId),
      targetAssetId: String(edge.targetAssetId),
      relationType: String(edge.relationType || "references"),
      verificationStatus: String(edge.verificationStatus || "confirmed"),
      confidence: Math.max(0, Math.min(1, Number(edge.confidence) || 0)),
      evidenceIds: (Array.isArray(edge.evidenceIds) ? edge.evidenceIds : []).map(String).slice(0, 20),
    })).slice(0, 120),
    meta: { depth: Number(graph?.meta?.depth ?? 0), truncated: Boolean(graph?.meta?.truncated), totalVisibleNodes: Number(graph?.meta?.totalVisibleNodes ?? graph?.nodes?.length ?? 0), totalVisibleEdges: Number(graph?.meta?.totalVisibleEdges ?? graph?.edges?.length ?? 0) },
  };
}

export function createAgentTools({ publicationRegistry }) {
  if (!publicationRegistry) throw new Error("Publication registry is required for Agent tools");
  return {
    async execute(tool, args, context) {
      const workspaceId = String(context.workspaceId);
      const role = String(context.role);
      if (tool === "search_assets") {
        const assets = await publicationRegistry.search(args.query, workspaceId, { role });
        return { query: args.query, assets: assets.slice(0, args.limit).map((asset) => safeAsset(asset, { includeEvidence: true })) };
      }
      if (tool === "read_asset") {
        const asset = await publicationRegistry.getAsset(workspaceId, args.assetId, { role });
        if (!asset) throw toolError("AGENT_SOURCE_NOT_FOUND", "当前权限范围内未找到该资产");
        return { asset: safeAsset(asset, { includeEvidence: true }) };
      }
      if (tool === "read_wiki") {
        const wiki = await publicationRegistry.getWiki(workspaceId, args.assetId, { role });
        if (!wiki) throw toolError("AGENT_SOURCE_NOT_FOUND", "当前权限范围内未找到该 Wiki");
        return { wiki: safeWiki(wiki) };
      }
      if (tool === "read_evidence") {
        const evidence = await publicationRegistry.getEvidence(workspaceId, args.evidenceId, { role });
        if (!evidence) throw toolError("AGENT_SOURCE_NOT_FOUND", "当前权限范围内未找到该证据");
        return { evidence: safeEvidence(evidence) };
      }
      if (tool === "inspect_neighborhood") {
        const graph = await publicationRegistry.getAssetGraph(workspaceId, { role, rootAssetId: args.assetId, depth: args.depth, includeProposed: EDIT_ROLES.has(role) && args.includeProposed === true, limit: 60, edgeLimit: 120 });
        if (graph?.meta?.rootUnavailable) throw toolError("AGENT_SOURCE_NOT_FOUND", "当前权限范围内未找到该资产关系");
        return { graph: safeGraph(graph) };
      }
      if (tool === "compare_assets") {
        const assets = await Promise.all(args.assetIds.map((id) => publicationRegistry.getAsset(workspaceId, id, { role })));
        if (assets.some((asset) => !asset)) throw toolError("AGENT_SOURCE_NOT_FOUND", "比较对象包含当前用户不可见或不存在的资产");
        return { assets: assets.map((asset) => safeAsset(asset, { includeEvidence: true })) };
      }
      if (tool === "compose_wiki_draft") {
        if (!EDIT_ROLES.has(role)) throw toolError("AGENT_ROLE_REQUIRED", "生成 Wiki 草案需要知识编辑者权限");
        const wiki = await publicationRegistry.getWiki(workspaceId, args.assetId, { role });
        if (!wiki) throw toolError("AGENT_SOURCE_NOT_FOUND", "当前权限范围内未找到该 Wiki");
        return { mode: "draft_only", instructions: String(args.instructions).slice(0, 2_000), currentWiki: safeWiki(wiki), excludedAction: "未保存、发布或修改正式 Wiki" };
      }
      throw toolError("AGENT_TOOL_NOT_ALLOWED", "该工具不在 IP 任务助手能力范围内");
    },
  };
}
