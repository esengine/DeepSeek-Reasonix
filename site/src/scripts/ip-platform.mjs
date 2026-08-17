import {
  PIPELINE_STAGES,
  advanceAnalysis,
  appendAudit,
  createInitialState,
  makeAuditEvent,
  stageState,
  validateIntake,
  validateShare,
} from "./ip-platform-state.mjs";
import {
  ASSET_TYPE_COLORS,
  RELATION_LABELS,
  edgePath,
  graphZoomLevel,
  layoutAssetGraph,
  normalizeGraphCamera,
  panGraphCamera,
  relatedAssetIds,
  zoomGraphCameraAt,
} from "./asset-graph.mjs";
import { collapseAssetGraphForDisplay, groupAssetsForDisplay } from "./asset-presentation.mjs";
import { auditBusinessCategory, deriveWorkspaceActions, presentWorkspaceSearchResults } from "./workspace-experience.mjs";
import { createAgentWorkbench } from "./agent-workbench.mjs";

const storageKey = "intelifar-ip-platform-v1";
const titles = {
  overview: "企业 IP 指挥台",
  tasks: "待办中心",
  documents: "文档中心",
  analysis: "智能分析工作室",
  agent: "IP 任务助手",
  assets: "IP 资产库",
  wiki: "IP Wiki",
  redaction: "敏感信息与风险线索",
  lifecycle: "IP 全生命周期",
  audit: "操作记录",
  members: "成员与权限",
  system: "系统状态",
};

function loadState() {
  const fallback = createInitialState();
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey) ?? "null");
    return saved ? { ...fallback, ...saved, analysis: { ...fallback.analysis, ...saved.analysis } } : fallback;
  } catch {
    return fallback;
  }
}

let state = loadState();
let selectedRealFile = null;
let intakeUsesDemo = false;
let currentRealJob = null;
let recentRealJobs = [];
let workspaceJobs = [];
let currentPublishedAssets = [];
let currentAssetPresentation = groupAssetsForDisplay([]);
let selectedPublishedAsset = null;
let activeEvidence = null;
let lastDrawerTrigger = null;
let activeAssetType = "all";
let activeAssetTag = "all";
let currentSession = null;
let currentMembers = [];
let currentInvitations = [];
let currentWikiReviews = [];
let currentSemanticReviews = [];
let currentWorkspaceActions = [];
let activeActionFilter = "all";
let pendingAssetGovernanceIds = [];
let assetGovernanceBatchMode = false;
let pendingMemberChange = null;
let pendingSemanticDecision = null;
let lastSemanticCheckResult = null;
let currentAuditEvents = [];
let currentDashboard = null;
let currentShares = [];
let currentAssetGraph = { nodes: [], edges: [], meta: {} };
let graphFocusId = null;
let selectedGraphNode = null;
let selectedGraphRelationship = null;
function defaultGraphCamera() {
  const camera = normalizeGraphCamera();
  return window.matchMedia("(max-width: 660px)").matches
    ? zoomGraphCameraAt(camera, 0.42, { x: 540, y: 310 })
    : camera;
}

let graphCamera = defaultGraphCamera();
let graphMobileViewport = window.matchMedia("(max-width: 660px)").matches;
const graphPointers = new Map();
let graphGesture = null;
let agentWorkbench = null;
let wikiEditBaseline = null;
let globalSearchTimer = null;
let globalSearchRequest = 0;
let globalSearchActiveIndex = -1;
const qs = (selector, root = document) => root.querySelector(selector);
const qsa = (selector, root = document) => [...root.querySelectorAll(selector)];

window.addEventListener("resize", () => {
  const mobile = window.matchMedia("(max-width: 660px)").matches;
  if (mobile === graphMobileViewport) return;
  graphMobileViewport = mobile;
  graphCamera = defaultGraphCamera();
  applyGraphCamera();
});

const DEMO_EVIDENCE = Object.freeze({
  id: "DEMO-S-049-01",
  assetId: "IP-DEMO-0841",
  quote: "系统在每个推理批次开始前计算专家负载向量，并依据预测结果动态调整 Top-K 专家集合。",
  section: "演示样本 · 3.2 稀疏专家路由机制",
  locator: "S-049-01",
  documentName: "星穹推理引擎技术白皮书（演示）",
  precision: "演示定位",
  quoteHash: "",
  parserBatchId: "",
  verified: false,
  sourceMode: "demo",
});

const REDACTION_DEMO_EVIDENCE = Object.freeze({
  id: "REDACT-S1-P114-08",
  assetId: "IP-DEMO-REDACTION",
  quote: "敏感参数已从演示导出副本的数据层移除；此处只展示内容块定位，不返回涂黑原值。",
  section: "演示样本 · 6.4 专家路由参数与工程实现",
  locator: "第 114 页 · P-114-08",
  documentName: "星穹推理引擎技术白皮书（演示）",
  precision: "内容块级",
  quoteHash: "",
  parserBatchId: "",
  verified: false,
  sourceMode: "demo",
});

const roleLabels = { owner: "空间所有者", admin: "空间管理员", editor: "知识编辑者", viewer: "只读成员" };

function canAnalyzeDocuments(session = currentSession) {
  return ["owner", "admin", "editor"].includes(session?.user?.role);
}

function canReadWorkspaceAudit(session = currentSession) {
  return ["owner", "admin"].includes(session?.user?.role);
}

const analysisPermissionMessage = "当前账号为只读成员，不能接入或分析文档。您仍可阅读 Wiki、查看原文依据并运行只读任务。";

function openIntakeDialog() {
  if (!canAnalyzeDocuments()) {
    showToast("当前岗位没有文档接入权限", analysisPermissionMessage, "error");
    return;
  }
  setText("#intake-submit-error", "");
  showDialog(qs("#intake-dialog"));
}

function applySession(session, mode = "local-session") {
  currentSession = { ...session, mode: session.mode || mode };
  document.body.dataset.sessionState = "ready";
  setText("#workspace-name", session.workspace?.name || "intelifar 工作空间");
  setText("#workspace-avatar", (session.workspace?.name || "I").trim().slice(0, 1));
  setText("#workspace-mode", currentSession.mode === "static-demo" || currentSession.mode === "loopback-demo" ? "受控演示空间" : currentSession.mode === "loopback-persistent" ? "本机持久工作空间" : "小微企业知识空间");
  setText("#profile-name", session.user?.name || session.user?.email || "当前用户");
  setText("#profile-avatar", (session.user?.name || session.user?.email || "U").trim().slice(0, 1));
  setText("#profile-role", roleLabels[session.user?.role] || session.user?.role || "成员");
  const canEdit = ["owner", "admin", "editor"].includes(session.user?.role);
  const canAnalyze = canAnalyzeDocuments(currentSession);
  const canAudit = canReadWorkspaceAudit(currentSession);
  const canManageMembers = ["owner", "admin"].includes(session.user?.role);
  const canApproveWiki = ["owner", "admin"].includes(session.user?.role);
  const demoMode = ["static-demo", "loopback-demo"].includes(currentSession.mode);
  qsa("[data-open-intake]").forEach((button) => {
    button.disabled = !canAnalyze;
    button.hidden = !canAnalyze;
    button.title = canAnalyze ? "从企业文档提取可溯源的 IP 资产" : analysisPermissionMessage;
  });
  qsa('[data-nav="audit"], [data-nav-target="audit"]').forEach((button) => { button.hidden = !canAudit; });
  qsa('[data-nav="members"], [data-nav-target="members"]').forEach((button) => { button.hidden = !canManageMembers; });
  qs("#document-role-note").hidden = canAnalyze;
  qs("#readonly-role-note").hidden = canAnalyze;
  qs("#view-audit").dataset.authorized = String(canAudit);
  qs("#view-members").dataset.authorized = String(canManageMembers);
  const wikiDraftTemplate = qs('[data-agent-template="wiki_draft"]');
  wikiDraftTemplate.disabled = !canEdit;
  wikiDraftTemplate.title = canEdit ? "只生成更新建议，不会保存正式 Wiki" : "需要知识编辑者、空间管理员或空间所有者权限";
  qs("#wiki-edit").disabled = !canEdit;
  qs("#asset-governance-open").hidden = !canEdit || demoMode;
  setText("#wiki-submit-mode-copy", canApproveWiki ? "您可以直接发布新版本；系统会保留旧版本与操作记录。" : "您的修改会先提交给空间管理员审批，不会立即改变已发布 Wiki。");
  setText("#wiki-change-impact", canApproveWiki ? "保存后会立即成为当前空间的新版本，历史内容仍可追溯。" : "提交后进入待办中心，由空间管理员批准后才会形成新版本。");
  setText("[data-testid='wiki-save']", canApproveWiki ? "保存并发布新版本" : "提交发布审批");
  qs("#open-share").disabled = !canEdit;
  qs("#share-wiki").disabled = !canEdit;
  qs("#graph-include-proposed").disabled = !canEdit;
  qs("#graph-include-proposed").checked = canEdit;
  qs("#graph-include-proposed").closest("label").title = canEdit ? "显示系统建议但尚未人工确认的关系" : "只读成员仅查看已确认关系";
  renderWorkspaceActions();
  qsa("[data-demo-lifecycle]").forEach((surface) => { surface.hidden = !demoMode; });
  qs("#lifecycle-real-governance").hidden = demoMode;
  qsa("[data-demo-redaction], [data-demo-analysis]").forEach((surface) => { surface.hidden = !demoMode; });
  qs("#redaction-real-workspace").hidden = demoMode;
  renderLifecycleGovernance();
  renderRealRiskWorkspace();
  if ((state.activeView === "audit" && !canAudit) || (state.activeView === "members" && !canManageMembers)) navigate("overview", false);
  if (qs("#session-dialog").open) qs("#session-dialog").close();
}

function lockWorkspace(message = "") {
  currentSession = null;
  document.body.dataset.sessionState = "locked";
  setText("#session-error", message);
  const dialog = qs("#session-dialog");
  if (!dialog.open) dialog.showModal();
  queueMicrotask(() => qs("#login-email")?.focus());
}

async function initializeSession() {
  try {
    const response = await fetch("/api/session", { headers: { accept: "application/json" } });
    if (response.status === 401) { lockWorkspace(); return false; }
    if (!response.ok) throw new Error("static");
    const payload = await response.json();
    applySession(payload.session, payload.session?.mode === "demo" ? "loopback-demo" : "local-session");
    return true;
  } catch {
    applySession({ user: { id: "USR-STATIC", name: "林越", role: "owner" }, workspace: { id: "WS-STATIC", name: "澜图科技" } }, "static-demo");
    return true;
  }
}

function saveState() {
  localStorage.setItem(storageKey, JSON.stringify(state));
}

function showToast(title, detail, type = "success") {
  const region = qs("#toast-region");
  const toast = document.createElement("div");
  toast.className = `toast ${type}`;
  const icon = document.createElement("span");
  const copy = document.createElement("div");
  const heading = document.createElement("strong");
  const description = document.createElement("small");
  icon.textContent = type === "error" ? "!" : "✓";
  heading.textContent = String(title);
  description.textContent = String(detail);
  copy.append(heading, description);
  toast.append(icon, copy);
  region.append(toast);
  setTimeout(() => toast.remove(), 4200);
}

const SVG_NS = "http://www.w3.org/2000/svg";

function svgElement(name, attributes = {}, text = "") {
  const element = document.createElementNS(SVG_NS, name);
  Object.entries(attributes).forEach(([key, value]) => element.setAttribute(key, String(value)));
  if (text) element.textContent = text;
  return element;
}

function demoAssetGraph() {
  const nodes = [
    { id: "IP-2026-0841", title: "稀疏专家路由与动态负载均衡方法", type: "技术方案", owner: "推理架构组", sensitivity: "机密", summary: "面向大规模混合专家模型推理的动态路由与拥塞预测方案。", tags: ["动态路由", "负载均衡"], confidence: .986, status: "已入库", version: "V1.3", evidenceIds: Array.from({ length: 14 }, (_, index) => `S-048-${String(index + 1).padStart(2, "0")}`) },
    { id: "IP-2026-0840", title: "长上下文分层缓存策略", type: "算法模型", owner: "基础模型组", sensitivity: "内部", summary: "降低长上下文推理过程中的重复计算与显存抖动。", tags: ["长上下文", "缓存"], confidence: .964, status: "已入库", version: "V1.2", evidenceIds: Array.from({ length: 9 }, (_, index) => `S-052-${index + 1}`) },
    { id: "IP-2026-0839", title: "跨模态语义一致性评估体系", type: "评估方法", owner: "多模态组", sensitivity: "内部", summary: "对文本、图像和表格抽取结果进行统一语义校验。", tags: ["多模态", "评估"], confidence: .948, status: "已入库", version: "V1.1", evidenceIds: Array.from({ length: 11 }, (_, index) => `S-067-${index + 1}`) },
    { id: "IP-2026-0838", title: "异构算力运行时编排框架", type: "软件架构", owner: "平台工程部", sensitivity: "机密", summary: "跨 GPU 与推理节点的统一运行时编排和资源隔离框架。", tags: ["异构算力", "编排"], confidence: .932, status: "已入库", version: "V2.0", evidenceIds: Array.from({ length: 18 }, (_, index) => `S-075-${index + 1}`) },
    { id: "IP-2026-0837", title: "推理服务 SLA 分级模型", type: "业务规则", owner: "解决方案部", sensitivity: "公开", summary: "将客户服务等级映射到延迟、吞吐与恢复目标。", tags: ["SLA", "服务治理"], confidence: .917, status: "已入库", version: "V1.4", evidenceIds: Array.from({ length: 6 }, (_, index) => `S-081-${index + 1}`) },
    { id: "IP-2026-0836", title: "路由决策回放与审计机制", type: "软件著作权", owner: "平台工程部", sensitivity: "内部", summary: "按推理批次回放路由决策并验证关键参数变更。", tags: ["审计", "可观测性"], confidence: .941, status: "已入库", version: "V1.0", evidenceIds: ["S-091-02", "S-091-03", "S-092-01"] },
    { id: "IP-2026-0835", title: "专家节点故障隔离策略", type: "业务规则", owner: "可靠性团队", sensitivity: "内部", summary: "识别异常专家节点并在不中断服务的情况下执行软隔离。", tags: ["容错", "隔离"], confidence: .925, status: "待复核", version: "V1.0", evidenceIds: ["S-096-04", "S-097-01"] },
  ];
  const edges = [
    { id: "REL-DEMO-001", sourceAssetId: "IP-2026-0841", targetAssetId: "IP-2026-0840", relationType: "depends_on", confidence: .97, verificationStatus: "confirmed", origin: "manual", evidenceIds: ["S-048-03"] },
    { id: "REL-DEMO-002", sourceAssetId: "IP-2026-0841", targetAssetId: "IP-2026-0838", relationType: "implements", confidence: .95, verificationStatus: "confirmed", origin: "manual", evidenceIds: ["S-048-09"] },
    { id: "REL-DEMO-003", sourceAssetId: "IP-2026-0839", targetAssetId: "IP-2026-0840", relationType: "similar_to", confidence: .88, verificationStatus: "confirmed", origin: "manual", evidenceIds: ["S-067-06"] },
    { id: "REL-DEMO-004", sourceAssetId: "IP-2026-0838", targetAssetId: "IP-2026-0837", relationType: "part_of", confidence: .91, verificationStatus: "confirmed", origin: "manual", evidenceIds: ["S-075-11"] },
    { id: "REL-DEMO-005", sourceAssetId: "IP-2026-0836", targetAssetId: "IP-2026-0841", relationType: "references", confidence: .93, verificationStatus: "confirmed", origin: "manual", evidenceIds: ["S-091-02"] },
    { id: "REL-DEMO-006", sourceAssetId: "IP-2026-0835", targetAssetId: "IP-2026-0841", relationType: "depends_on", confidence: .82, verificationStatus: "proposed", origin: "model", evidenceIds: [] },
    { id: "REL-DEMO-007", sourceAssetId: "IP-2026-0835", targetAssetId: "IP-2026-0838", relationType: "conflicts_with", confidence: .74, verificationStatus: "proposed", origin: "model", evidenceIds: [] },
  ];
  return { nodes, edges, meta: { totalVisibleNodes: nodes.length, totalVisibleEdges: edges.length, storageMode: "static-demo" } };
}

function graphAsset(node) {
  const published = currentPublishedAssets.find((asset) => asset.id === node.id);
  if (published) return published;
  const evidence = (node.evidenceIds ?? []).map((id) => ({ id, assetId: node.id, section: "来源文档", quote: "此关系节点已绑定可追溯的来源证据。", verified: true }));
  return {
    ...node,
    title: node.title,
    summary: node.summary || "尚未补充资产摘要。",
    tags: node.tags ?? [],
    evidence,
    document: { sourceName: "企业知识资产来源文档", title: "知识资产来源文档" },
    wiki: { title: node.title, executiveSummary: node.summary || "尚未补充资产摘要。", keyMechanism: "请在资产 Wiki 中查看完整机制说明。", metrics: [], relationships: [] },
    publishedAt: node.updatedAt || new Date().toISOString(),
  };
}

function graphNodeColor(type) {
  return ASSET_TYPE_COLORS[type] || ASSET_TYPE_COLORS.未分类;
}

function graphCameraPoint(event) {
  const rect = qs("#asset-graph").getBoundingClientRect();
  return {
    x: (event.clientX - rect.left) * 1080 / Math.max(1, rect.width),
    y: (event.clientY - rect.top) * 620 / Math.max(1, rect.height),
  };
}

function applyGraphCamera({ announce = true } = {}) {
  graphCamera = normalizeGraphCamera(graphCamera);
  const viewport = qs("#graph-viewport");
  const graph = qs("#asset-graph");
  viewport.setAttribute("transform", `translate(${graphCamera.x.toFixed(2)} ${graphCamera.y.toFixed(2)}) scale(${graphCamera.scale.toFixed(3)})`);
  const level = graphZoomLevel(graphCamera.scale);
  graph.dataset.zoomLevel = level;
  graph.dataset.cameraX = graphCamera.x.toFixed(2);
  graph.dataset.cameraY = graphCamera.y.toFixed(2);
  graph.dataset.cameraScale = graphCamera.scale.toFixed(3);
  const levelLabel = level === "overview" ? "全景脉络" : level === "detail" ? "节点细节" : "关系网络";
  const status = `${Math.round(graphCamera.scale * 100)}% · ${levelLabel}`;
  setText("#graph-zoom-reset", `${Math.round(graphCamera.scale * 100)}%`);
  if (announce || qs("#graph-camera-status")?.textContent !== status) setText("#graph-camera-status", status);
}

function resetGraphCamera() {
  graphCamera = defaultGraphCamera();
  applyGraphCamera();
}

function updateGraphInspector(node) {
  selectedGraphNode = node;
  selectedGraphRelationship = null;
  qs("#relationship-inspector").hidden = true;
  const inspector = qs("#graph-inspector");
  inspector.hidden = false;
  setText("#graph-inspector-type", `${node.type || "知识资产"} · ${node.id}`);
  setText("#graph-inspector-title", node.title);
  setText("#graph-inspector-summary", node.summary || "尚未补充资产摘要。打开资产详情可继续完善。 ");
  setText("#graph-inspector-owner", node.owner || "待认领");
  setText("#graph-inspector-sensitivity", node.sensitivity || "待复核");
  const includeProposed = qs("#graph-include-proposed").checked && !qs("#graph-include-proposed").disabled;
  const degree = currentAssetGraph.edges.filter((edge) => (edge.verificationStatus === "confirmed" || includeProposed) && (edge.sourceAssetId === node.id || edge.targetAssetId === node.id)).length;
  setText("#graph-inspector-degree", `${degree} 条`);
  setText("#graph-inspector-evidence", `${node.evidenceIds?.length ?? 0} 处`);
  qsa(".graph-node", qs("#graph-nodes")).forEach((element) => element.classList.toggle("is-selected", element.dataset.graphNodeId === node.id));
  qsa(".graph-edge, .graph-edge-glow", qs("#graph-viewport")).forEach((element) => {
    const connected = element.dataset.source === node.id || element.dataset.target === node.id;
    element.classList.toggle("is-highlighted", connected);
    element.classList.toggle("is-dimmed", !connected);
  });
  qsa(".graph-edge-label", qs("#graph-edge-labels")).forEach((element) => element.classList.toggle("is-dimmed", element.dataset.source !== node.id && element.dataset.target !== node.id));
}

function updateRelationshipInspector(edge) {
  selectedGraphNode = null;
  selectedGraphRelationship = edge;
  qs("#graph-inspector").hidden = true;
  const inspector = qs("#relationship-inspector");
  inspector.hidden = false;
  const source = currentAssetGraph.nodes.find((node) => node.id === edge.sourceAssetId);
  const target = currentAssetGraph.nodes.find((node) => node.id === edge.targetAssetId);
  const label = RELATION_LABELS[edge.relationType] || edge.relationType;
  const proposed = edge.verificationStatus === "proposed";
  const canReview = proposed && ["owner", "admin", "editor"].includes(currentSession?.user?.role);
  setText("#relationship-inspector-status", proposed ? "待人工复核 · AI 建议" : "已确认关系");
  setText("#relationship-inspector-title", `${source?.title || edge.sourceAssetId} ${label} ${target?.title || edge.targetAssetId}`);
  setText("#relationship-inspector-source", source?.title || edge.sourceAssetId);
  setText("#relationship-inspector-label", label);
  setText("#relationship-inspector-target", target?.title || edge.targetAssetId);
  setText("#relationship-inspector-confidence", `${Math.round(Number(edge.confidence || 0) * 100)}%`);
  setText("#relationship-inspector-origin", edge.origin === "model" ? "文档智能发现" : edge.origin === "import" ? "批量导入" : "人工建立");
  setText("#relationship-inspector-verification", proposed ? "待复核" : "已确认");
  setText("#relationship-inspector-evidence", `${edge.evidenceIds?.length ?? 0} 处`);
  const evidenceButton = qs("#relationship-open-evidence");
  evidenceButton.disabled = !edge.evidenceIds?.length;
  evidenceButton.textContent = edge.evidenceIds?.length ? `查看关联证据（${edge.evidenceIds.length}）` : "暂无关联证据";
  setText("#relationship-review-note", proposed ? "关联证据来自两端资产，仅作为人工复核线索；未确认前不参与只读成员搜索。" : "已确认关系会参与资产搜索扩展与影响分析。 ");
  qs("#relationship-review-actions").hidden = !canReview;
  qsa(".graph-node", qs("#graph-nodes")).forEach((element) => element.classList.remove("is-selected", "is-dimmed"));
  qsa(".graph-edge, .graph-edge-glow", qs("#graph-viewport")).forEach((element) => {
    const selected = element.dataset.relationshipId === edge.id;
    element.classList.toggle("is-highlighted", selected);
    element.classList.toggle("is-dimmed", !selected);
  });
  qsa(".graph-edge-label", qs("#graph-edge-labels")).forEach((element) => {
    element.classList.toggle("is-dimmed", element.dataset.relationshipId !== edge.id);
  });
}

async function reviewSelectedRelationship(nextStatus) {
  const edge = selectedGraphRelationship;
  if (!edge || edge.verificationStatus !== "proposed") return;
  const buttons = qsa("button", qs("#relationship-review-actions"));
  buttons.forEach((button) => { button.disabled = true; });
  try {
    if (currentSession?.mode === "static-demo") {
      currentAssetGraph.edges = nextStatus === "rejected"
        ? currentAssetGraph.edges.filter((candidate) => candidate.id !== edge.id)
        : currentAssetGraph.edges.map((candidate) => candidate.id === edge.id ? { ...candidate, verificationStatus: "confirmed", origin: "model" } : candidate);
    } else {
      const action = nextStatus === "confirmed" ? "confirm" : "reject";
      const response = await fetch(`/api/relationships/${encodeURIComponent(edge.id)}/${action}`, { method: "POST", headers: { accept: "application/json" } });
      const payload = await response.json().catch(() => ({}));
      if (response.status === 401) { lockWorkspace(); return; }
      if (!response.ok) throw new Error(payload.error || "关系复核失败");
      await loadAssetGraph();
    }
    selectedGraphRelationship = null;
    qs("#relationship-inspector").hidden = true;
    renderAssetGraph();
    showToast(nextStatus === "confirmed" ? "关系已确认" : "关系建议已拒绝", nextStatus === "confirmed" ? "该关系现在可用于检索扩展与影响分析" : "该建议已从当前关系网络移除");
  } catch (error) {
    showToast("无法完成关系复核", String(error.message || "请稍后重试"), "error");
  } finally {
    buttons.forEach((button) => { button.disabled = false; });
  }
}

function populateGraphFilters() {
  const select = qs("#graph-type-filter");
  const current = select.value;
  const known = new Set(qsa("option", select).map((option) => option.value));
  [...new Set(currentAssetGraph.nodes.map((node) => node.type || "未分类"))].sort((left, right) => left.localeCompare(right, "zh-CN")).forEach((type) => {
    if (known.has(type)) return;
    const option = document.createElement("option");
    option.value = type;
    option.textContent = type;
    select.append(option);
  });
  if ([...select.options].some((option) => option.value === current)) select.value = current;
}

function renderRelationshipRegister(edges, nodesById) {
  const register = qs("#graph-relation-list");
  register.replaceChildren();
  edges.forEach((edge) => {
    const article = document.createElement("article");
    const source = document.createElement("strong");
    const relation = document.createElement("span");
    const target = document.createElement("strong");
    article.tabIndex = 0;
    article.setAttribute("role", "button");
    article.setAttribute("aria-label", `查看关系：${nodesById.get(edge.sourceAssetId)?.title || edge.sourceAssetId} ${RELATION_LABELS[edge.relationType] || edge.relationType} ${nodesById.get(edge.targetAssetId)?.title || edge.targetAssetId}`);
    source.textContent = nodesById.get(edge.sourceAssetId)?.title || edge.sourceAssetId;
    relation.textContent = `${RELATION_LABELS[edge.relationType] || edge.relationType}${edge.verificationStatus === "proposed" ? " · 待复核" : ""}`;
    target.textContent = nodesById.get(edge.targetAssetId)?.title || edge.targetAssetId;
    article.append(source, relation, target);
    article.addEventListener("click", () => updateRelationshipInspector(edge));
    article.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") { event.preventDefault(); updateRelationshipInspector(edge); }
    });
    register.append(article);
  });
  setText("#graph-relation-total", `${edges.length} 条`);
}

function renderAssetGraph() {
  const graph = currentAssetGraph;
  const query = qs("#graph-search").value.trim().toLocaleLowerCase("zh-CN");
  const type = qs("#graph-type-filter").value;
  const relationType = qs("#graph-relation-filter").value;
  const includeProposed = qs("#graph-include-proposed").checked && !qs("#graph-include-proposed").disabled;
  let edges = graph.edges.filter((edge) => (edge.verificationStatus === "confirmed" || includeProposed) && (relationType === "all" || edge.relationType === relationType));
  let nodes = graph.nodes.filter((node) => type === "all" || node.type === type);
  const allowedTypeIds = new Set(nodes.map((node) => node.id));
  edges = edges.filter((edge) => allowedTypeIds.has(edge.sourceAssetId) && allowedTypeIds.has(edge.targetAssetId));
  if (query) {
    const matches = new Set(nodes.filter((node) => [node.id, node.title, node.type, node.owner, node.summary, ...(node.tags ?? [])].join(" ").toLocaleLowerCase("zh-CN").includes(query)).map((node) => node.id));
    const adjacent = new Set(matches);
    edges.forEach((edge) => {
      if (matches.has(edge.sourceAssetId)) adjacent.add(edge.targetAssetId);
      if (matches.has(edge.targetAssetId)) adjacent.add(edge.sourceAssetId);
    });
    nodes = nodes.filter((node) => adjacent.has(node.id));
  }
  if (graphFocusId) {
    const focused = relatedAssetIds({ nodes, edges }, graphFocusId, 1);
    if (focused.size) nodes = nodes.filter((node) => focused.has(node.id));
    else graphFocusId = null;
  }
  const nodeIds = new Set(nodes.map((node) => node.id));
  edges = edges.filter((edge) => nodeIds.has(edge.sourceAssetId) && nodeIds.has(edge.targetAssetId));
  if (selectedGraphRelationship && !edges.some((edge) => edge.id === selectedGraphRelationship.id)) {
    selectedGraphRelationship = null;
    qs("#relationship-inspector").hidden = true;
  }
  const nodesById = new Map(nodes.map((node) => [node.id, node]));
  const positioned = layoutAssetGraph({ nodes, edges }, { width: 1080, height: 620, focusId: graphFocusId });
  const positionedById = new Map(positioned.map((node) => [node.id, node]));
  const glowLayer = qs("#graph-edge-glows");
  const edgeLayer = qs("#graph-edges");
  const labelLayer = qs("#graph-edge-labels");
  const nodeLayer = qs("#graph-nodes");
  glowLayer.replaceChildren(); edgeLayer.replaceChildren(); labelLayer.replaceChildren(); nodeLayer.replaceChildren();
  edges.forEach((edge) => {
    const source = positionedById.get(edge.sourceAssetId);
    const target = positionedById.get(edge.targetAssetId);
    if (!source || !target) return;
    const pathData = edgePath(source, target, edge.id);
    const stateClass = edge.verificationStatus === "proposed" ? " is-proposed" : "";
    const glow = svgElement("path", { d: pathData, class: `graph-edge-glow${stateClass}`, "aria-hidden": "true", "data-source": edge.sourceAssetId, "data-target": edge.targetAssetId, "data-relationship-id": edge.id });
    const path = svgElement("path", { d: pathData, class: `graph-edge${edge.verificationStatus === "proposed" ? " is-proposed" : ""}`, "aria-hidden": "true", "data-source": edge.sourceAssetId, "data-target": edge.targetAssetId, "data-relationship-id": edge.id });
    const hitTarget = svgElement("path", { d: pathData, class: `graph-edge-hit${edge.verificationStatus === "proposed" ? " is-proposed" : ""}`, tabindex: "0", role: "button", "aria-label": `查看关系：${source.title} ${RELATION_LABELS[edge.relationType] || edge.relationType} ${target.title}`, "data-source": edge.sourceAssetId, "data-target": edge.targetAssetId, "data-relationship-id": edge.id });
    const title = svgElement("title", {}, `${source.title} ${RELATION_LABELS[edge.relationType] || edge.relationType} ${target.title}${edge.verificationStatus === "proposed" ? "，待复核" : "，已确认"}`);
    hitTarget.append(title);
    hitTarget.addEventListener("click", () => updateRelationshipInspector(edge));
    hitTarget.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") { event.preventDefault(); updateRelationshipInspector(edge); }
    });
    glowLayer.append(glow);
    edgeLayer.append(path, hitTarget);
    if (edges.length <= 28) {
      const label = svgElement("text", { x: (source.x + target.x) / 2, y: (source.y + target.y) / 2 - 7, class: "graph-edge-label", "data-source": edge.sourceAssetId, "data-target": edge.targetAssetId, "data-relationship-id": edge.id }, RELATION_LABELS[edge.relationType] || edge.relationType);
      labelLayer.append(label);
    }
  });
  positioned.forEach((node) => {
    const color = graphNodeColor(node.type);
    const radius = Math.min(24, 11 + node.degree * 1.8 + (node.isFocus ? 3 : 0));
    const labelSide = node.x < 500 ? -1 : 1;
    const labelX = labelSide * (radius + 13);
    const labelAnchor = labelSide < 0 ? "end" : "start";
    const group = svgElement("g", { class: `graph-node${selectedGraphNode?.id === node.id ? " is-selected" : ""}${node.isFocus ? " is-core" : ""}`, transform: `translate(${node.x} ${node.y})`, tabindex: "0", role: "button", "aria-label": `${node.title}，${node.type || "知识资产"}，${node.degree} 条直接关系`, "data-graph-node-id": node.id, "data-node-degree": node.degree, style: `color:${color}` });
    group.append(
      svgElement("circle", { class: "node-hit", r: Math.max(34, radius + 12) }),
      svgElement("circle", { class: "node-ripple", r: radius + 17 }),
      svgElement("circle", { class: "node-orbit", r: radius + 8 }),
      svgElement("circle", { class: "node-core", r: radius, fill: color }),
      svgElement("circle", { class: "node-center", r: Math.max(3.5, radius * .26) }),
      svgElement("circle", { class: "node-degree-badge", cx: -radius * .72, cy: -radius * .72, r: 8 }),
      svgElement("text", { class: "node-degree", x: -radius * .72, y: -radius * .72 }, String(node.degree)),
      svgElement("text", { class: "node-title", x: labelX, y: -4, "text-anchor": labelAnchor }, node.title.length > 18 ? `${node.title.slice(0, 18)}…` : node.title),
      svgElement("text", { class: "node-kicker", x: labelX, y: 12, "text-anchor": labelAnchor }, `${node.type || "知识资产"} · ${node.id.slice(-6)}`),
      svgElement("text", { class: "node-meta", x: labelX, y: 27, "text-anchor": labelAnchor }, `${node.owner || "待认领"} · ${Math.round(Number(node.confidence || 0) * 100)}%`),
    );
    group.addEventListener("click", () => updateGraphInspector(node));
    group.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") { event.preventDefault(); updateGraphInspector(node); }
    });
    nodeLayer.append(group);
  });
  const proposedCount = graph.edges.filter((edge) => edge.verificationStatus === "proposed").length;
  const connectedIds = new Set(graph.edges.flatMap((edge) => [edge.sourceAssetId, edge.targetAssetId]));
  setText("#graph-node-count", String(graph.nodes.length));
  setText("#graph-edge-count", String(graph.edges.filter((edge) => edge.verificationStatus === "confirmed").length));
  setText("#graph-proposed-count", String(proposedCount));
  setText("#graph-coverage", graph.nodes.length ? `${Math.round(connectedIds.size / graph.nodes.length * 100)}%` : "0%");
  const duplicateGraphCopy = Number(graph.meta?.duplicateRecordCount || 0) > 0 ? ` · 已合并 ${graph.meta.duplicateRecordCount} 条重复记录` : "";
  setText("#graph-summary", graphFocusId ? `已聚焦 ${nodesById.get(graphFocusId)?.title || graphFocusId} 的一跳网络` : `神经全景 · ${nodes.length} 项独立资产 · ${edges.length} 条关系${duplicateGraphCopy}`);
  qs("#graph-empty").hidden = positioned.length > 0;
  renderRelationshipRegister(edges, nodesById);
  const legend = qs("#graph-legend");
  legend.replaceChildren();
  [...new Set(nodes.map((node) => node.type || "未分类"))].slice(0, 7).forEach((nodeType) => {
    const item = document.createElement("span"); const marker = document.createElement("i"); const label = document.createElement("b");
    marker.style.background = graphNodeColor(nodeType); label.textContent = nodeType; item.append(marker, label); legend.append(item);
  });
  applyGraphCamera({ announce: false });
}

async function loadAssetGraph() {
  qs("#graph-loading").hidden = false;
  try {
    const params = new URLSearchParams({ limit: "100", edgeLimit: "200" });
    if (!qs("#graph-include-proposed").disabled) params.set("includeProposed", "true");
    const response = await fetch(`/api/assets/graph?${params}`, { headers: { accept: "application/json" } });
    if (response.status === 401) { lockWorkspace(); return; }
    if (!response.ok) throw new Error("graph-api-unavailable");
    const payload = await response.json();
    currentAssetGraph = collapseAssetGraphForDisplay(payload.graph ?? { nodes: [], edges: [], meta: {} });
  } catch {
    currentAssetGraph = currentSession?.mode === "static-demo" ? demoAssetGraph() : { nodes: [], edges: [], meta: {} };
  } finally {
    qs("#graph-loading").hidden = true;
  }
  populateGraphFilters();
  renderAssetGraph();
  renderWorkspaceActions();
}

function closeDrawers(restoreFocus = true) {
  qsa(".drawer.is-open").forEach((drawer) => {
    drawer.classList.remove("is-open");
    drawer.setAttribute("aria-hidden", "true");
  });
  qs("#drawer-backdrop").hidden = true;
  if (restoreFocus && lastDrawerTrigger?.isConnected) lastDrawerTrigger.focus();
  lastDrawerTrigger = null;
}

function navigate(view, updateHash = true) {
  if (!titles[view]) return;
  if (view === "audit" && currentSession && !canReadWorkspaceAudit()) {
    showToast("审计总账仅限管理员", "普通成员不会看到其他成员、分享对象或运维事件。", "error");
    view = "overview";
  }
  if (view === "members" && currentSession && !["owner", "admin"].includes(currentSession.user?.role)) {
    showToast("成员名单仅限管理员", "您仍可从左下角账号入口查看自己的权限。", "error");
    view = "overview";
  }
  state.activeView = view;
  qsa("[data-view]").forEach((section) => {
    const active = section.dataset.view === view;
    section.hidden = !active;
    section.classList.toggle("is-active", active);
  });
  qsa("[data-nav]").forEach((button) => {
    const active = button.dataset.nav === view;
    button.classList.toggle("is-active", active);
    if (active) button.setAttribute("aria-current", "page");
    else button.removeAttribute("aria-current");
  });
  qs("#page-title").textContent = titles[view];
  qs(".sidebar").classList.remove("is-open");
  qs("#mobile-menu").setAttribute("aria-expanded", "false");
  closeDrawers();
  if (updateHash) history.replaceState(null, "", `#${view}`);
  saveState();
  window.scrollTo({ top: 0, behavior: "smooth" });
}

function updateStageCollection(selector, progress) {
  qsa(`${selector} [data-threshold]`).forEach((element, index) => {
    const result = stageState(progress, Number(element.dataset.threshold));
    element.classList.toggle("is-complete", result === "complete");
    element.classList.toggle("is-active", result === "active");
    if (element.classList.contains("pipeline-step")) {
      const marker = element.querySelector(":scope > span");
      if (marker) marker.textContent = result === "complete" ? "✓" : String(index + 1);
    }
    if (element.classList.contains("analysis-stage")) {
      const marker = element.querySelector(":scope > strong");
      if (marker) {
        marker.classList.toggle("stage-pulse", result === "active");
        marker.textContent = result === "complete" ? "✓" : result === "active" ? "" : "—";
      }
    }
  });
}

function updateAnalysisUI() {
  const { progress, status, document: name, category } = state.analysis;
  const isReal = state.analysis.mode === "real";
  const number = qs("#analysis-progress-number");
  if (number) number.textContent = `${progress}%`;
  qs("#analysis-progress-ring")?.style.setProperty("--progress", progress);
  qs("#overview-progress-number").textContent = `${progress}%`;
  qs("#overview-pipeline-fill").style.width = `${progress}%`;
  updateStageCollection("#analysis-stage-grid", progress);
  updateStageCollection("#overview-pipeline", progress);

  const current = PIPELINE_STAGES.find((stage) => stageState(progress, stage.threshold) === "active");
  qs("#overview-progress-text").textContent = status === "idle" ? "等待接入文档" : status === "complete" ? "分析已完成" : status === "failed" ? "分析失败" : status === "blocked" ? "文件已安全拦截" : `正在${current?.label ?? "生成 Wiki"}`;
  qs("#analysis-status-label").textContent = status === "idle" ? "等待任务" : status === "complete" ? "已完成" : status === "failed" ? "失败" : status === "blocked" ? "已拦截" : "运行中";
  qs("#analysis-live-text").textContent = state.analysis.liveText || (status === "complete" ? "18 项 IP 资产与 Wiki 已生成" : `正在处理：${current?.label ?? "智能分析"}`);
  const button = qs("#advance-analysis");
  button.hidden = isReal;
  button.disabled = status === "complete" || status === "failed";
  button.textContent = status === "complete" ? "✓ 分析已完成" : "推进演示任务";

  qs("#view-analysis .analysis-meta h2")?.replaceChildren(document.createTextNode(name));
  qs("#analysis-file-type").textContent = name.split(".").at(-1)?.slice(0, 4).toUpperCase() || "DOC";
  qs("#analysis-meta-copy").textContent = `${state.analysis.id} · ${category} · ${isReal ? "真实服务链路" : "离线演示"}`;
  const overviewFile = qs(".pipeline-summary .file-glyph + span strong");
  if (overviewFile) overviewFile.textContent = name;
  const overviewMeta = qs(".pipeline-summary .file-glyph + span small");
  if (overviewMeta) overviewMeta.textContent = `18.6 MB · 326 页 · ${category}`;
}

function showDialog(dialog) {
  if (!dialog.open) dialog.showModal();
}

function clearErrors(form) {
  qsa(".field-error", form).forEach((element) => (element.textContent = ""));
  qsa("[aria-invalid='true']", form).forEach((element) => element.removeAttribute("aria-invalid"));
}

function applyErrors(form, errors) {
  Object.entries(errors).forEach(([field, message]) => {
    const input = form.elements.namedItem(field);
    input?.setAttribute("aria-invalid", "true");
    const target = qs(`[data-error-for="${field}"]`, form);
    if (target) target.textContent = message;
  });
}

function openDrawer(drawer, trigger = document.activeElement) {
  closeDrawers(false);
  lastDrawerTrigger = trigger instanceof HTMLElement ? trigger : null;
  drawer.classList.add("is-open");
  drawer.setAttribute("aria-hidden", "false");
  qs("#drawer-backdrop").hidden = false;
  qs(".dialog-close", drawer)?.focus();
}

function trapDrawerFocus(event) {
  const drawer = qs(".drawer.is-open");
  if (!drawer || event.key !== "Tab") return;
  const focusable = qsa("button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex='-1'])", drawer).filter((element) => !element.hidden);
  if (!focusable.length) return;
  const first = focusable[0];
  const last = focusable.at(-1);
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
}

const auditActionLabels = {
  "file.security_scan": "完成文件安全检查",
  "auth.login": "成员登录工作空间",
  "auth.logout": "成员退出工作空间",
  "analysis.submit": "接入文档并启动分析",
  "analysis.retry": "重试分析任务",
  "publication.publish": "发布 IP 资产",
  "asset.metadata_update": "完善资产信息",
  "asset.metadata_batch_update": "批量完善资产信息",
  "wiki.update": "更新 Wiki",
  "wiki.review_submit": "提交 Wiki 发布审批",
  "wiki.review_approve": "批准 Wiki 更新发布",
  "wiki.review_reject": "退回 Wiki 更新",
  "evidence.view": "查看原文依据",
  "audit.export": "导出操作记录",
  "backup.create": "创建数据库备份",
  "backup.verify": "校验数据库备份",
  "member.invitation_create": "邀请成员加入",
  "member.invitation_revoke": "撤销成员邀请",
  "member.update": "更新成员权限",
  "share.create": "创建安全分享",
  "share.revoke": "撤销安全分享",
  "relationship.create": "创建资产关系",
  "relationship.confirm": "确认资产关系",
  "relationship.reject": "拒绝资产关系",
  "share.access": "访问安全分享",
  "member.invitation_accept": "成员完成账号激活",
  "agent.task_create": "创建 IP 分析任务",
  "agent.task_complete": "完成 IP 任务",
  "agent.task_needs_review": "IP 任务等待人工复核",
  "agent.task_failed": "IP 任务执行失败",
  "agent.task_blocked": "拦截越界 IP 任务",
  "agent.task_cancel": "取消 IP 任务",
};

const agentIntentLabels = {
  asset_inventory: "资产盘点",
  evidence_review: "原文依据核查",
  impact_analysis: "影响分析",
  document_comparison: "资产对比",
  wiki_draft: "Wiki 草案",
  risk_gap_review: "风险缺口核查",
  due_diligence_pack: "客户尽调材料",
};

const agentFailureLabels = {
  INVALID_AGENT_PLAN: "任务计划格式不符合安全要求，未执行任何工具",
  AGENT_MODEL_ERROR: "智能分析服务暂时不可用",
  AGENT_EXECUTION_ERROR: "只读分析步骤未能完成",
  AGENT_SOURCE_NOT_FOUND: "权限范围内没有找到所需资产或原文依据",
  AGENT_ROLE_REQUIRED: "当前角色缺少所需的知识编辑权限",
};

function auditDetailText(event) {
  if (typeof event.detail === "string") return event.detail;
  const detail = event.detail ?? {};
  if (event.action === "evidence.view") return `${detail.documentId || "文档"} · ${detail.locator || event.objectId} · ${detail.sensitivity === "S1" ? "高敏内容访问" : detail.sensitivity || "受控访问"}`;
  if (event.action === "analysis.submit") return `${detail.documentName || event.objectId} · 已开始处理`;
  if (event.action === "publication.publish") return `${detail.assetCount || 0} 项资产 · 来源任务 ${detail.sourceJobId || "—"}`;
  if (event.action === "asset.metadata_update") return `${event.objectId || "IP 资产"} · ${detail.owner || "权属部门已确认"} · ${detail.sensitivity || "密级已确认"}`;
  if (event.action === "asset.metadata_batch_update") return `${detail.count || detail.assetIds?.length || 0} 项资产 · ${detail.owner || "权属部门已确认"} · ${detail.sensitivity || "密级已确认"}`;
  if (event.action === "wiki.review_submit") return `${detail.assetId || "Wiki"} · 基于 ${detail.baseVersion || "当前版本"} · ${detail.changeNote || "内容更新"}`;
  if (event.action === "wiki.review_approve") return `${detail.assetId || "Wiki"} · 已发布 ${detail.version || "新版本"}`;
  if (event.action === "wiki.review_reject") return `${detail.assetId || "Wiki"} · 已发布内容未变化 · ${detail.reviewNote || "退回补充依据"}`;
  if (event.action === "wiki.update") return `${event.objectId || "Wiki"} · 已发布 ${detail.version || "新版本"} · ${detail.changeNote || "内容更新"}`;
  if (event.action === "file.security_scan") return detail.decision === "allow" ? "文件内容与类型检查通过，可以进入分析" : "文件安全检查未通过，已停止处理";
  if (event.action === "backup.create") return `可恢复副本已生成${detail.size ? ` · ${formatBytes(detail.size)}` : ""}`;
  if (event.action === "backup.verify") return detail.integrity === "ok" ? "备份文件完整，可以用于恢复" : "备份文件需要管理员检查";
  if (event.action === "member.invitation_create") return `${detail.email || "新成员"} · ${roleLabels[detail.role] || detail.role || "成员"} · 等待加入`;
  if (event.action === "member.invitation_revoke") return `${detail.email || "成员邀请"} · 已停止使用`;
  if (event.action === "member.update") return `${event.objectId || "成员"} · ${roleLabels[detail.role] || detail.role || "角色未变"} · ${detail.status === "disabled" ? "账号已停用" : "账号有效"}`;
  if (event.action === "share.create") return `${detail.assetId || "Wiki"} · 分享给 ${detail.recipient || "指定接收方"} · ${detail.expiresAt ? `${formatDate(detail.expiresAt)} 到期` : "限时访问"}`;
  if (event.action === "share.revoke") return `${detail.assetId || "Wiki"} · 已停止 ${detail.recipient || "接收方"} 后续访问`;
  if (event.action === "auth.login") return "成员通过企业账号进入当前工作空间";
  if (event.action === "auth.logout") return "成员主动结束当前工作空间会话";
  if (event.action === "agent.task_create") return `${agentIntentLabels[detail.templateId] || "自定义分析"} · 仅限当前账号可见资产`;
  if (event.action === "agent.task_complete") return `${agentIntentLabels[detail.intent] || "IP 分析"} · ${detail.stepCount || 0} 个只读步骤 · 证据覆盖 ${Math.round(Number(detail.evidenceCoverage || 0) * 100)}%`;
  if (event.action === "agent.task_needs_review") return `${agentIntentLabels[detail.intent] || "IP 分析"} · 结果已保留，需业务人员复核`;
  if (event.action === "agent.task_failed") return agentFailureLabels[detail.code] || "任务已安全停止，未改变正式知识";
  if (event.action === "agent.task_blocked") return "请求超出文档、IP 资产、原文依据与 Wiki 的分析范围";
  if (event.action === "agent.task_cancel") return "任务由创建人主动取消，未改变正式知识";
  if (event.action.startsWith("relationship.")) return `${detail.sourceAssetId || event.objectId || "资产关系"} · ${RELATION_LABELS[detail.relationType] || detail.relationType || detail.verificationStatus || "状态已更新"}`;
  if (event.action === "share.access") return `${event.objectId || "安全分享"} · 已记录一次受控访问`;
  if (event.action === "member.invitation_accept") return `${event.objectId || "新成员"} · ${roleLabels[detail.role] || detail.role || "成员"}`;
  if (event.action === "audit.export") return "当前工作空间操作记录 · CSV";
  return Object.entries(detail).slice(0, 4).map(([key, value]) => `${key}: ${String(value)}`).join(" · ") || `${event.objectType || "对象"} ${event.objectId || ""}`;
}

function auditPresentation(event, requestedLevel = "") {
  if (requestedLevel) return { level: requestedLevel, label: requestedLevel === "danger" ? "已拦截" : requestedLevel === "warning" ? "敏感操作" : "成功" };
  const action = String(event?.action || "");
  if (/fail|error|失败/i.test(action)) return { level: "danger", label: "失败" };
  if (/block|deny|拦截/i.test(action)) return { level: "danger", label: "已拦截" };
  if (/reject|拒绝/i.test(action)) return { level: "danger", label: "已拒绝" };
  if (/cancel|取消|interrupt|中断/i.test(action)) return { level: "info", label: "已取消" };
  if (/needs_review|复核/i.test(action)) return { level: "warning", label: "待复核" };
  if (action === "evidence.view") return { level: "warning", label: "敏感操作" };
  return { level: "secure", label: "成功" };
}

function appendAuditToDom(event, requestedLevel = "") {
  const log = qs("#audit-log");
  const presentation = auditPresentation(event, requestedLevel);
  const article = document.createElement("article");
  const icon = document.createElement("span");
  const copy = document.createElement("div");
  const heading = document.createElement("strong");
  const detail = document.createElement("p");
  const metadata = document.createElement("small");
  const timestamp = document.createElement("time");
  const status = document.createElement("span");
  article.dataset.auditEntry = "";
  article.dataset.auditAction = String(event.action || "");
  article.dataset.auditStatus = presentation.label;
  article.dataset.auditCategory = auditBusinessCategory(event.action);
  icon.className = `audit-icon ${presentation.level === "danger" ? "red" : presentation.level === "warning" ? "amber" : "purple"}`;
  icon.textContent = presentation.level === "danger" ? "!" : presentation.level === "warning" ? "◎" : "↗";
  heading.textContent = auditActionLabels[event.action] || event.action;
  detail.textContent = auditDetailText(event);
  metadata.textContent = `${event.id} · ${event.actor?.name || event.actor || "系统"}`;
  timestamp.textContent = event.createdAt ? formatDate(event.createdAt) : event.timestamp;
  status.className = `status-chip ${presentation.level}`;
  status.textContent = presentation.label;
  copy.append(heading, detail, metadata);
  article.append(icon, copy, timestamp, status);
  log.prepend(article);
}

const actionCategoryLabels = Object.freeze({ content: "内容与发布", governance: "资产治理", access: "成员协作", operations: "系统维护" });
const actionPriorityLabels = Object.freeze({ urgent: "急", high: "高", normal: "办", low: "低" });

function applyWorkspaceActionFilter() {
  let visible = 0;
  qsa("#workspace-action-list [data-workspace-action]").forEach((item) => {
    const match = activeActionFilter === "all" || item.dataset.category === activeActionFilter;
    item.hidden = !match;
    if (match) visible += 1;
  });
  setText("#action-filter-status", `${visible} 项当前岗位待办`);
}

function openWorkspaceAction(action) {
  if (action.targetId) {
    const asset = currentPublishedAssets.find((item) => item.id === action.targetId) || currentAssetPresentation.assets.find((item) => item.id === action.targetId);
    if (asset) renderDynamicWiki(asset);
  }
  navigate(action.destination);
  if (action.id === "asset-governance") {
    const filter = qs("#asset-filter");
    if ([...filter.options].some((option) => option.value === "待复核" || option.textContent === "待复核")) filter.value = "待复核";
    filter.dispatchEvent(new Event("change"));
    qs("#asset-list-panel")?.scrollIntoView({ behavior: "smooth", block: "start" });
  }
  if (action.id === "relationship-review") qs("#graph-canvas")?.scrollIntoView({ behavior: "smooth", block: "center" });
  if (action.id === "failed-jobs") qs("#operations-job-list")?.scrollIntoView({ behavior: "smooth", block: "center" });
  if (action.id === "semantic-review") qs("#semantic-check-panel")?.scrollIntoView({ behavior: "smooth", block: "start" });
}

function workspaceActionItem(action) {
  const article = document.createElement("article");
  article.className = "workspace-action-item";
  article.dataset.workspaceAction = action.id;
  article.dataset.category = action.category;
  article.dataset.priority = action.priority;
  const priority = document.createElement("span");
  priority.className = "workspace-action-priority";
  priority.textContent = actionPriorityLabels[action.priority] || "办";
  const copy = document.createElement("div");
  copy.className = "workspace-action-copy";
  const title = document.createElement("strong");
  const detail = document.createElement("span");
  const meta = document.createElement("small");
  title.textContent = action.title;
  detail.textContent = action.detail;
  meta.textContent = `${actionCategoryLabels[action.category] || "工作空间"} · 负责岗位：${action.ownerLabel || "当前岗位"} · ${action.dueLabel || "建议及时处理"}`;
  copy.append(title, detail, meta);
  const buttons = document.createElement("div");
  buttons.className = "workspace-action-buttons";
  const open = document.createElement("button");
  open.type = "button";
  open.className = "secondary-btn small";
  const canBatchAssets = action.id === "asset-governance" && Array.isArray(action.assetIds) && action.assetIds.length > 1;
  open.textContent = action.canDecide ? "查看 Wiki" : canBatchAssets ? "逐项查看" : "去处理";
  open.addEventListener("click", () => openWorkspaceAction(action));
  buttons.append(open);
  if (canBatchAssets) {
    const batch = document.createElement("button");
    batch.type = "button";
    batch.className = "primary-btn small asset-governance-batch";
    batch.textContent = `批量处理 ${action.assetIds.length} 项`;
    batch.addEventListener("click", () => openAssetGovernanceDialog(action.assetIds));
    buttons.append(batch);
  }
  if (action.canDecide) {
    const reject = document.createElement("button");
    reject.type = "button";
    reject.className = "text-link wiki-review-decision";
    reject.dataset.reviewId = action.reviewId;
    reject.dataset.decision = "rejected";
    reject.textContent = "退回补充依据";
    const approve = document.createElement("button");
    approve.type = "button";
    approve.className = "primary-btn small wiki-review-decision";
    approve.dataset.reviewId = action.reviewId;
    approve.dataset.decision = "approved";
    approve.textContent = "批准发布";
    buttons.append(reject, approve);
  }
  article.append(priority, copy, buttons);
  return article;
}

function renderWorkspaceActions() {
  currentWorkspaceActions = deriveWorkspaceActions({ role: currentSession?.user?.role, assets: currentAssetPresentation.assets, graph: currentAssetGraph, jobs: workspaceJobs, invitations: currentInvitations, wikiReviews: currentWikiReviews, semanticReviews: currentSemanticReviews });
  setText("#nav-task-count", String(currentWorkspaceActions.length));
  qs(".notification span").textContent = currentWorkspaceActions.length ? String(Math.min(99, currentWorkspaceActions.length)) : "";
  setText("#action-total-count", String(currentWorkspaceActions.length));
  setText("#action-urgent-count", String(currentWorkspaceActions.filter((item) => ["urgent", "high"].includes(item.priority)).length));
  setText("#action-content-count", String(currentWorkspaceActions.filter((item) => item.category === "content").length));
  setText("#action-governance-count", String(currentWorkspaceActions.filter((item) => item.category === "governance").length));
  const list = qs("#workspace-action-list");
  list.replaceChildren();
  if (currentWorkspaceActions.length) list.append(...currentWorkspaceActions.map(workspaceActionItem));
  else {
    const empty = document.createElement("div");
    empty.className = "action-empty";
    empty.innerHTML = "<span>✓</span><strong>当前岗位没有待处理事项</strong><p>工作空间状态发生变化后，新事项会自动出现在这里。</p>";
    list.append(empty);
  }
  applyWorkspaceActionFilter();
}

async function loadWikiReviews() {
  if (!["owner", "admin", "editor"].includes(currentSession?.user?.role) || ["static-demo", "loopback-demo"].includes(currentSession?.mode)) {
    currentWikiReviews = [];
    renderWorkspaceActions();
    return;
  }
  try {
    const response = await fetch("/api/wiki/reviews?status=pending", { headers: { accept: "application/json" } });
    if (response.status === 401) { lockWorkspace(); return; }
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(payload.error || "发布审批暂时不可用");
    currentWikiReviews = Array.isArray(payload.reviews) ? payload.reviews : [];
  } catch {
    currentWikiReviews = [];
  }
  renderWorkspaceActions();
}

qsa("[data-nav]").forEach((button) => button.addEventListener("click", () => navigate(button.dataset.nav)));
qsa("[data-nav-target]").forEach((button) => button.addEventListener("click", () => navigate(button.dataset.navTarget)));
qsa("[data-open-intake]").forEach((button) => button.addEventListener("click", openIntakeDialog));
qsa("[data-close-dialog]").forEach((button) => button.addEventListener("click", () => button.closest("dialog")?.close()));
qsa("[data-close-drawer]").forEach((button) => button.addEventListener("click", closeDrawers));
qs("#drawer-backdrop").addEventListener("click", closeDrawers);
qs("#session-dialog").addEventListener("cancel", (event) => event.preventDefault());

qs("#login-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const submit = qs("[type='submit']", form);
  const input = Object.fromEntries(new FormData(form));
  setText("#session-error", "");
  submit.disabled = true;
  submit.setAttribute("aria-busy", "true");
  try {
    const response = await fetch("/api/auth/login", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ email: input.email, password: input.password }) });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok || !payload.session) throw new Error(response.status === 401 ? "邮箱或密码不正确" : payload.error || "暂时无法登录");
    form.reset();
    applySession(payload.session);
    await initializeAuthenticatedWorkspace();
    showToast("已进入安全工作空间", `${payload.session.workspace.name} · ${roleLabels[payload.session.user.role] || payload.session.user.role}`);
  } catch (error) {
    setText("#session-error", String(error.message || "暂时无法登录"));
    qs("#login-password").select();
  } finally {
    submit.disabled = false;
    submit.removeAttribute("aria-busy");
  }
});

qs("#mobile-menu").addEventListener("click", (event) => {
  const open = qs(".sidebar").classList.toggle("is-open");
  event.currentTarget.setAttribute("aria-expanded", String(open));
});

qs("#theme-toggle").addEventListener("click", () => {
  state.theme = document.documentElement.dataset.theme === "dark" ? "light" : "dark";
  document.documentElement.dataset.theme = state.theme;
  saveState();
  showToast("显示模式已更新", state.theme === "dark" ? "已切换为夜间工作台" : "已切换为明亮工作台");
});

qs("#use-demo-file").addEventListener("click", () => {
  selectedRealFile = null;
  intakeUsesDemo = true;
  qs("#intake-name").value = "星穹推理引擎技术白皮书 V3.2.pdf";
  qs("#intake-category").value = "技术白皮书";
  qs("#selected-file").hidden = true;
  clearErrors(qs("#intake-form"));
  showToast("演示文档已就绪", "326 页 PDF，可直接开始分析");
});

function selectRealFile(file) {
  if (!file) return;
  if (file.size > 25 * 1024 * 1024) {
    showToast("文件超过安全上限", "真实分析网关单文件最大 25 MB", "error");
    return;
  }
  selectedRealFile = file;
  if (intakeUsesDemo) qs("#intake-category").value = "";
  intakeUsesDemo = false;
  qs("#intake-name").value = file.name;
  const selected = qs("#selected-file");
  selected.textContent = `✓ 已选择 ${file.name} · ${(file.size / 1024).toFixed(1)} KB · 将使用 MinerU + DeepSeek`;
  selected.hidden = false;
  clearErrors(qs("#intake-form"));
}

qs("#select-real-file").addEventListener("click", () => qs("#real-file-input").click());
qs("#real-file-input").addEventListener("change", (event) => selectRealFile(event.currentTarget.files?.[0]));
qs("#upload-zone").addEventListener("dragover", (event) => event.preventDefault());
qs("#upload-zone").addEventListener("drop", (event) => {
  event.preventDefault();
  selectRealFile(event.dataTransfer?.files?.[0]);
});

qs("#upload-zone").addEventListener("keydown", (event) => {
  if (event.key === "Enter" || event.key === " ") qs("#real-file-input").click();
});

function setText(selector, value) {
  const element = qs(selector);
  if (element) element.textContent = String(value ?? "—");
}

function normalizeEvidenceText(value) {
  return String(value || "").normalize("NFKC").replace(/\s+/g, " ").trim();
}

function matchPublishedEvidence(source, assetTitle) {
  const quote = normalizeEvidenceText(source?.quote);
  if (!quote) return null;
  const jobAssets = currentRealJob?.id ? currentPublishedAssets.filter((asset) => asset.sourceJobId === currentRealJob.id) : currentPublishedAssets;
  const candidates = jobAssets.length ? jobAssets : currentPublishedAssets;
  const titleMatch = candidates.find((asset) => asset.title === assetTitle && asset.evidence?.some((evidence) => normalizeEvidenceText(evidence.quote) === quote));
  const asset = titleMatch || candidates.find((item) => item.evidence?.some((evidence) => normalizeEvidenceText(evidence.quote) === quote));
  return asset?.evidence?.find((evidence) => normalizeEvidenceText(evidence.quote) === quote) || null;
}

function renderRealRiskWorkspace() {
  const root = qs("#redaction-real-workspace");
  if (!root || root.hidden) return;
  const items = recentRealJobs.flatMap((job) => (job.result?.analysis?.risks || []).map((risk) => ({
    ...risk,
    job,
    documentTitle: job.result?.analysis?.document?.title || job.document?.name || "未命名文档",
  })));
  const highLevels = new Set(["high", "高", "s1"]);
  setText("#redaction-risk-total", String(items.length));
  setText("#redaction-risk-high", String(items.filter((item) => highLevels.has(String(item.level || "").toLocaleLowerCase())).length));
  setText("#redaction-document-count", String(new Set(items.map((item) => item.job.id)).size));
  const list = qs("#real-risk-workspace-list");
  list.replaceChildren();
  for (const item of items) {
    const card = document.createElement("article");
    const level = document.createElement("span");
    const copy = document.createElement("div");
    const title = document.createElement("h3");
    const meta = document.createElement("small");
    const detail = document.createElement("p");
    const quote = document.createElement("blockquote");
    const actions = document.createElement("div");
    const openAnalysis = document.createElement("button");
    const normalizedLevel = String(item.level || "medium").toLocaleLowerCase();
    const severity = highLevels.has(normalizedLevel) ? "high" : ["low", "低", "s3"].includes(normalizedLevel) ? "low" : "medium";
    level.className = `risk-level ${severity}`;
    level.textContent = severity === "high" ? "高" : severity === "low" ? "低" : "中";
    title.textContent = item.title || "待复核风险";
    meta.textContent = `${item.documentTitle} · ${item.job.id}`;
    detail.textContent = item.detail || "请结合原文与业务背景复核。";
    quote.textContent = item.source_quote ? `原文线索：${item.source_quote}` : "当前风险未附逐字引用，需要人工补充依据。";
    openAnalysis.type = "button";
    openAnalysis.className = "text-link";
    openAnalysis.textContent = "打开文档分析 →";
    openAnalysis.addEventListener("click", () => { showRecentRealJob(item.job); navigate("analysis"); });
    actions.append(openAnalysis);
    const evidence = matchPublishedEvidence({ quote: item.source_quote }, "");
    if (evidence) actions.prepend(evidenceButton(evidence, "查看正式证据"));
    copy.append(title, meta, detail, quote, actions);
    card.append(level, copy);
    list.append(card);
  }
  if (!items.length) {
    const empty = document.createElement("div");
    empty.className = "ledger-empty";
    empty.textContent = "当前工作空间尚无风险提示；接入文档后，分析结果会在这里集中汇总。";
    list.append(empty);
  }
}

function renderRealResults(result) {
  const { parser, llm, analysis } = result;
  setText("#real-provider-id", parser.batchId);
  setText("#real-parser-model", parser.model);
  setText("#real-model-name", llm.model);
  setText("#real-response-id", llm.responseId || "响应编号不可用");
  setText("#real-token-usage", `${Number(llm.usage?.totalTokens || 0).toLocaleString("zh-CN")} tokens`);
  const parsedCharacters = Number(parser.markdownCharacters || 0);
  const analysisCharacters = Number(parser.analysisInputCharacters || parsedCharacters);
  setText("#real-markdown-count", `${parsedCharacters.toLocaleString("zh-CN")} 字符`);
  setText("#real-analysis-range", parser.analysisSamplingStrategy === "section-balanced"
    ? `DeepSeek 分段分析 ${analysisCharacters.toLocaleString("zh-CN")} 字符 · ${Number(parser.analysisSelectedSections || 0)}/${Number(parser.analysisTotalSections || 0)} 章节`
    : `DeepSeek 全量分析 ${analysisCharacters.toLocaleString("zh-CN")} 字符`);
  setText("#real-markdown-hash", `SHA-256 ${parser.markdownSha256.slice(0, 12)}…`);
  setText("#real-document-title", analysis.document.title);
  setText("#real-document-summary", analysis.document.summary);
  setText("#real-wiki-mechanism", analysis.wiki.key_mechanism || analysis.wiki.executive_summary);
  setText("#real-asset-count", `${analysis.assets.length} 项`);
  const sourceQuotes = analysis.assets.flatMap((asset) => Array.isArray(asset.source_quotes) ? asset.source_quotes : []);
  const published = currentRealJob?.id && currentPublishedAssets.some((asset) => asset.sourceJobId === currentRealJob.id);
  setText("#analysis-stage-parse-detail", `${parsedCharacters.toLocaleString("zh-CN")} 字符 · ${Number(parser.analysisTotalSections || 0)} 个章节`);
  setText("#analysis-stage-classify-detail", `${analysis.document.category || "已识别"} · ${analysis.document.language || "语言已识别"}`);
  setText("#analysis-stage-assets-detail", `${analysis.assets.length} 项资产 · ${(analysis.risks || []).length} 项风险提示`);
  setText("#analysis-stage-evidence-detail", `${sourceQuotes.filter((source) => source.verified !== false).length} / ${sourceQuotes.length} 条引用已核验`);
  setText("#analysis-stage-publish-detail", published ? "已发布到资产库与 Wiki" : "分析完成，等待人工复核发布");

  const assetList = qs("#real-asset-list");
  assetList.replaceChildren();
  analysis.assets.forEach((asset) => {
    const article = document.createElement("article");
    const top = document.createElement("div");
    const type = document.createElement("span");
    const confidence = document.createElement("b");
    const title = document.createElement("h4");
    const summary = document.createElement("p");
    const tags = document.createElement("div");
    type.textContent = asset.type;
    confidence.textContent = `${Math.round(Number(asset.confidence || 0) * 100)}%`;
    title.textContent = asset.title;
    summary.textContent = asset.summary;
    asset.tags.forEach((tag) => { const chip = document.createElement("span"); chip.textContent = tag; tags.append(chip); });
    top.append(type, confidence);
    article.append(top, title, summary, tags);
    assetList.append(article);
  });

  const evidence = qs("#real-source-quotes");
  evidence.replaceChildren();
  const quotes = analysis.assets.flatMap((asset) => asset.source_quotes.map((source) => ({ ...source, asset: asset.title }))).slice(0, 8);
  quotes.forEach((source, index) => {
    const block = document.createElement("blockquote");
    const marker = document.createElement("span");
    const copy = document.createElement("p");
    const citation = document.createElement("cite");
    const matchedEvidence = matchPublishedEvidence(source, source.asset);
    marker.textContent = `依据 ${String(index + 1).padStart(2, "0")}`;
    copy.textContent = source.quote;
    citation.textContent = `${source.section} · ${source.asset} · ${matchedEvidence ? "打开正式证据" : "发布后可查看正式证据"}`;
    if (matchedEvidence) {
      block.classList.add("is-actionable");
      block.tabIndex = 0;
      block.setAttribute("role", "button");
      block.setAttribute("aria-label", `查看原文依据：${source.section}`);
      block.dataset.evidenceId = matchedEvidence.id;
      const open = () => openAgentSource(matchedEvidence.id, block);
      block.addEventListener("click", open);
      block.addEventListener("keydown", (event) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); open(); } });
    }
    block.append(marker, copy, citation);
    evidence.append(block);
  });

  const risks = qs("#real-risk-list");
  risks.replaceChildren();
  analysis.risks.slice(0, 4).forEach((risk) => {
    const item = document.createElement("div");
    const level = document.createElement("span");
    const copy = document.createElement("p");
    const heading = document.createElement("strong");
    const detail = document.createElement("small");
    level.textContent = risk.level;
    heading.textContent = risk.title;
    detail.textContent = risk.detail;
    copy.append(heading, detail);
    item.append(level, copy);
    risks.append(item);
  });
  qs("#real-analysis-results").hidden = false;
  const publishButton = qs("#publish-analysis");
  publishButton.disabled = false;
  publishButton.removeAttribute("aria-busy");
  publishButton.textContent = "复核并发布到资产库 →";
  setText("#publication-status", "待人工复核");
}

function populateRecentRealJobs() {
  const select = qs("#real-job-select");
  select.replaceChildren();
  recentRealJobs.forEach((job) => {
    const option = document.createElement("option");
    option.value = job.id;
    option.textContent = `${job.result?.analysis?.document?.title || job.document?.name || job.id} · ${job.result?.analysis?.assets?.length || 0} 项资产`;
    select.append(option);
  });
  select.disabled = recentRealJobs.length === 0;
}

function renderAnalysisRunLog(job = null) {
  const button = qs("#analysis-run-log");
  const panel = qs("#analysis-run-log-panel");
  const list = qs("#analysis-log-steps");
  panel.hidden = true;
  button.setAttribute("aria-expanded", "false");
  list.replaceChildren();
  if (!job) {
    button.disabled = true;
    button.title = currentPublishedAssets.length ? "历史迁移资料未保留可回放的运行记录" : "接入文档后可查看运行记录";
    setText("#analysis-log-title", "暂无可回放任务");
    setText("#analysis-log-meta", button.title);
    return;
  }
  button.disabled = false;
  button.removeAttribute("title");
  setText("#analysis-log-title", job.document?.name || job.result?.analysis?.document?.title || job.id);
  setText("#analysis-log-meta", `${job.id} · ${operationStateLabels[job.state] || job.state} · 更新于 ${formatDate(job.updatedAt)}`);
  const progress = job.state === "complete" ? 100 : Number(state.analysis.progress || 0);
  PIPELINE_STAGES.forEach((stage, index) => {
    const item = document.createElement("li");
    const marker = document.createElement("span");
    const heading = document.createElement("strong");
    const detail = document.createElement("small");
    const stageStatus = stageState(progress, stage.threshold);
    marker.textContent = String(index + 1).padStart(2, "0");
    heading.textContent = stage.label;
    detail.textContent = stageStatus === "complete" ? "已完成" : stageStatus === "active" ? "正在处理" : "等待前序步骤";
    item.append(marker, heading, detail);
    list.append(item);
  });
}

function showRecentRealJob(job) {
  if (!job?.result) return;
  currentRealJob = job;
  state.analysis = {
    ...state.analysis,
    id: job.id,
    mode: "real",
    document: job.document?.name || job.result.analysis.document.title,
    category: job.result.analysis.document.category,
    progress: 100,
    status: "complete",
    liveText: `${job.result.analysis.assets.length} 项真实 IP 资产与 Wiki 已生成`,
    updatedAt: new Date(job.updatedAt || Date.now()).toLocaleString("zh-CN"),
  };
  renderRealResults(job.result);
  if (currentPublishedAssets.some((asset) => asset.sourceJobId === job.id)) {
    qs("#publish-analysis").disabled = true;
    qs("#publish-analysis").textContent = "✓ 已发布到资产库与 Wiki";
    setText("#publication-status", "已发布");
  }
  qs("#real-job-select").value = job.id;
  updateAnalysisUI();
  renderAnalysisRunLog(job);
  saveState();
}

function showEmptyRealAnalysis(migrationRecord = null) {
  currentRealJob = null;
  const sourceDocuments = new Set(currentPublishedAssets.map((asset) => asset.document?.sourceName).filter(Boolean));
  const migrated = currentPublishedAssets.length > 0;
  const migratedCopy = `已有 ${sourceDocuments.size || 1} 份历史文档形成 ${currentAssetPresentation.uniqueCount || currentPublishedAssets.length} 项独立资产；迁移记录未保留可回放的分析过程`;
  state.analysis = {
    ...state.analysis,
    id: migrationRecord?.id || "",
    mode: "real",
    document: migrationRecord?.name || (migrated ? "历史资料已发布，暂无可回放任务" : "尚无分析任务"),
    category: migrationRecord?.status || (migrated ? "历史迁移记录" : "接入文档后自动识别"),
    progress: 0,
    status: "idle",
    liveText: migrated ? migratedCopy : "请接入一份文档开始真实分析",
    updatedAt: migrationRecord?.updatedAt || "—",
  };
  qs("#real-analysis-results").hidden = true;
  const stageCopy = migrated ? "历史记录未保留" : "等待真实任务";
  setText("#analysis-stage-parse-detail", stageCopy);
  setText("#analysis-stage-classify-detail", stageCopy);
  setText("#analysis-stage-assets-detail", migrated ? `${currentAssetPresentation.uniqueCount || currentPublishedAssets.length} 项资产已发布` : stageCopy);
  setText("#analysis-stage-evidence-detail", migrated ? "可从资产与 Wiki 查看" : stageCopy);
  setText("#analysis-stage-publish-detail", migrated ? "已迁移到当前空间" : "等待复核");
  updateAnalysisUI();
  renderAnalysisRunLog();
}

async function loadRecentRealJobs(preferredId = state.analysis.id) {
  try {
    const response = await fetch("/api/analysis", { headers: { accept: "application/json" } });
    if (response.status === 401) { lockWorkspace(); return; }
    if (!response.ok) return;
    const payload = await response.json();
    workspaceJobs = payload.jobs || [];
    recentRealJobs = workspaceJobs.filter((job) => job.state === "complete" && job.result);
    populateRecentRealJobs();
    renderWorkspaceDocuments();
    const selected = recentRealJobs.find((job) => job.id === preferredId) || recentRealJobs[0];
    if (selected) showRecentRealJob(selected);
    else if (!["static-demo", "loopback-demo"].includes(currentSession?.mode)) showEmptyRealAnalysis();
    renderRealRiskWorkspace();
    renderWorkspaceActions();
  } catch {
    // Static acceptance mode intentionally has no persisted real jobs.
  }
}

function evidenceButton(evidence, label = "查看原文依据") {
  const button = document.createElement("button");
  button.className = "citation";
  button.dataset.evidenceId = evidence.id;
  button.textContent = `${label} · ${evidence.id}`;
  button.addEventListener("click", (event) => {
    event.stopPropagation();
    renderEvidence(evidence, event.currentTarget);
  });
  return button;
}

function renderEvidence(evidence, trigger = document.activeElement) {
  if (!evidence) return;
  activeEvidence = evidence;
  setText("#evidence-asset-id", evidence.assetId);
  setText("#evidence-id", evidence.id);
  setText("#evidence-locator", evidence.locator || evidence.section || "章节级定位");
  setText("#evidence-document", evidence.documentName);
  setText("#evidence-precision", `${evidence.precision || "章节级"} · ${evidence.section || "MinerU Markdown"}`);
  setText("#evidence-hash", evidence.quoteHash ? `${evidence.quoteHash.slice(0, 16)}…` : "未生成");
  setText("#evidence-provider-task", evidence.parserBatchId ? `MinerU ${evidence.parserBatchId}` : "解析任务不可用");
  setText("#evidence-section", evidence.section || "原文引用");
  const quote = qs("#evidence-quote");
  quote.replaceChildren(document.createTextNode(evidence.quote || "暂无引用文本"));
  const verified = document.createElement("span");
  verified.textContent = evidence.sourceMode === "demo" ? "功能演示证据，不代表当前工作空间资料" : evidence.verified ? "逐字引用与哈希已绑定" : "等待人工校验";
  quote.append(verified);
  setText("#evidence-integrity-title", evidence.sourceMode === "demo" ? "演示上下文已隔离" : evidence.verified ? "证据完整性记录已生成" : "证据等待人工校验");
  setText("#evidence-integrity-copy", evidence.sourceMode === "demo" ? "该样本仅用于展示原文定位，不会冒充当前工作空间的真实资料。" : evidence.verified ? "引用内容与已保存原文一致；页面没有提供精确页码时，会按实际可用位置显示。" : "这条原文依据尚未完成完整核验。");
  openDrawer(qs("#provenance-drawer"), trigger);
}

async function openAgentSource(sourceId, trigger) {
  const id = String(sourceId || "");
  try {
    if (id.startsWith("EV-")) {
      const response = await fetch(`/api/evidence/${encodeURIComponent(id)}`, { headers: { accept: "application/json" } });
      if (response.status === 401) { lockWorkspace(); return; }
      const payload = await response.json().catch(() => ({}));
      if (!response.ok || !payload.evidence) throw new Error("当前权限范围内无法读取该证据");
      renderEvidence(payload.evidence, trigger);
      return;
    }
    const assetId = id.startsWith("WIKI:") ? id.slice(5) : id.startsWith("IP-") ? id : "";
    if (assetId) {
      let asset = currentPublishedAssets.find((item) => item.id === assetId);
      if (!asset && currentSession?.mode !== "static-demo") {
        const response = await fetch(`/api/assets/${encodeURIComponent(assetId)}`, { headers: { accept: "application/json" } });
        if (response.ok) asset = (await response.json()).asset;
      }
      if (asset && id.startsWith("WIKI:")) { renderDynamicWiki(asset); navigate("wiki"); return; }
      if (asset) { populateAssetDrawer(asset, trigger); return; }
      const demoRow = Array.from(document.querySelectorAll("[data-asset-id]")).find((row) => row.dataset.assetId === assetId);
      if (demoRow && ["static-demo", "loopback-demo"].includes(currentSession?.mode)) {
        if (id.startsWith("WIKI:")) navigate("wiki");
        else { navigate("assets"); openDrawer(qs("#asset-drawer"), trigger); }
        showToast("已打开任务来源", `${assetId} · 当前演示空间`);
        return;
      }
      navigate("assets");
      qs("#asset-search").value = assetId;
      qs("#asset-search").dispatchEvent(new Event("input"));
      showToast("已定位到资产库", `请在当前权限范围内复核 ${assetId}`);
      return;
    }
    if (id.startsWith("REL-")) {
      navigate("assets");
      qs("#graph-search").value = id;
      renderAssetGraph();
      showToast("已打开关系全景", `关系收据 ${id}`);
    }
  } catch (error) { showToast("无法打开任务来源", String(error.message || error), "error"); }
}

function populateAssetDrawer(asset, trigger = document.activeElement) {
  selectedPublishedAsset = asset;
  setText("#asset-drawer-title", asset.title);
  setText("#asset-drawer-meta", `${asset.id} · ${asset.type}`);
  setText("#asset-drawer-sensitivity", asset.sensitivity);
  setText("#asset-drawer-owner", asset.owner);
  setText("#asset-drawer-document", asset.document?.sourceName);
  setText("#asset-drawer-status", asset.status);
  setText("#asset-drawer-confidence", `${Math.round(Number(asset.confidence || 0) * 100)}%`);
  setText("#asset-drawer-evidence-count", `${asset.evidence.length} 处真实引用`);
  setText("#asset-drawer-version", asset.version);
  setText("#asset-drawer-summary", asset.summary);
  const sensitivity = qs("#asset-drawer-sensitivity");
  sensitivity.className = `status-chip ${asset.sensitivity === "机密" ? "danger" : asset.sensitivity === "内部" ? "info" : "warning"}`;
  const tags = qs("#asset-drawer-tags");
  tags.replaceChildren();
  (asset.tags.length ? asset.tags : ["待补充标签"]).forEach((tag) => { const chip = document.createElement("span"); chip.textContent = tag; tags.append(chip); });
  const evidenceAction = qs("#asset-view-evidence");
  evidenceAction.dataset.evidenceId = asset.evidence[0]?.id || "";
  evidenceAction.disabled = !asset.evidence.length;
  qs("#asset-governance-open").hidden = !["owner", "admin", "editor"].includes(currentSession?.user?.role) || !asset.id.startsWith("IP-REAL-") || ["static-demo", "loopback-demo"].includes(currentSession?.mode);
  renderDynamicWiki(asset);
  openDrawer(qs("#asset-drawer"), trigger);
}

function selectedAssetGovernanceIds() {
  if (!assetGovernanceBatchMode) return pendingAssetGovernanceIds.slice(0, 1);
  return qsa("#asset-governance-selection-list input[type='checkbox']:checked").map((input) => input.value);
}

function updateAssetGovernanceSelectionCount() {
  const count = selectedAssetGovernanceIds().length;
  setText("#asset-governance-selection-count", `已选择 ${count} 项`);
  const submit = qs("[data-testid='asset-governance-save']");
  submit.textContent = assetGovernanceBatchMode ? count ? `确认并更新 ${count} 项` : "请先选择资产" : "确认并完成待办";
  submit.disabled = assetGovernanceBatchMode && count === 0;
}

function renderAssetGovernanceSelection(assets) {
  const section = qs("#asset-governance-selection");
  section.hidden = !assetGovernanceBatchMode;
  const list = qs("#asset-governance-selection-list");
  list.replaceChildren();
  for (const asset of assets) {
    const label = document.createElement("label");
    const input = document.createElement("input");
    const copy = document.createElement("span");
    const title = document.createElement("strong");
    const detail = document.createElement("small");
    input.type = "checkbox";
    input.value = asset.id;
    input.checked = true;
    input.addEventListener("change", updateAssetGovernanceSelectionCount);
    title.textContent = asset.title;
    detail.textContent = `${asset.type || "知识资产"} · ${asset.owner || "待确权"} · ${asset.sensitivity || "待复核"}`;
    copy.append(title, detail);
    label.append(input, copy);
    list.append(label);
  }
  updateAssetGovernanceSelectionCount();
}

function openAssetGovernanceDialog(assetIds = []) {
  const canEdit = ["owner", "admin", "editor"].includes(currentSession?.user?.role);
  const requestedIds = Array.isArray(assetIds) ? [...new Set(assetIds)] : [];
  assetGovernanceBatchMode = requestedIds.length > 1;
  const assets = assetGovernanceBatchMode
    ? currentAssetPresentation.assets.filter((asset) => requestedIds.includes(asset.id) && asset.id.startsWith("IP-REAL-"))
    : selectedPublishedAsset?.id?.startsWith("IP-REAL-") ? [selectedPublishedAsset] : [];
  if (!canEdit || !assets.length) {
    showToast("当前岗位不能修改资产信息", "请联系知识编辑者或空间管理员处理", "error");
    return;
  }
  pendingAssetGovernanceIds = assets.map((asset) => asset.id);
  const owners = [...new Set(assets.map((asset) => String(asset.owner || "").trim()))];
  const sensitivities = [...new Set(assets.map((asset) => String(asset.sensitivity || "").trim()))];
  const commonOwner = owners.length === 1 && !/^(?:待确权|待认领|待复核)$/i.test(owners[0]) ? owners[0] : "";
  const commonSensitivity = sensitivities.length === 1 && ["公开", "内部", "机密"].includes(sensitivities[0]) ? sensitivities[0] : "";
  setText("#asset-governance-heading", assetGovernanceBatchMode ? "批量完善资产信息" : "完善资产信息");
  setText("#asset-governance-intro", assetGovernanceBatchMode ? "选择具有相同权属和敏感级别的资产，一次完成确认。" : "确认由哪个部门负责，以及哪些成员可以看到这项资产。");
  qs("#asset-governance-owner").value = commonOwner;
  qs("#asset-governance-sensitivity").value = commonSensitivity;
  renderAssetGovernanceSelection(assets);
  setText("[data-error-for='asset-owner']", "");
  setText("#asset-governance-error", "");
  showDialog(qs("#asset-governance-dialog"));
  queueMicrotask(() => qs("#asset-governance-owner")?.focus());
}

async function saveAssetGovernance(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const selectedGroupIds = selectedAssetGovernanceIds();
  const assetIds = [...new Set(selectedGroupIds.flatMap((assetId) => {
    const asset = currentAssetPresentation.assets.find((item) => item.id === assetId) || (selectedPublishedAsset?.id === assetId ? selectedPublishedAsset : null);
    return Array.isArray(asset?.sourceRecordIds) && asset.sourceRecordIds.length ? asset.sourceRecordIds : [assetId];
  }))];
  const wasBatch = assetGovernanceBatchMode;
  const usesBatchEndpoint = wasBatch || assetIds.length > 1;
  const owner = qs("#asset-governance-owner").value.normalize("NFKC").trim();
  const sensitivity = qs("#asset-governance-sensitivity").value;
  const submit = qs("[data-testid='asset-governance-save']", form);
  setText("[data-error-for='asset-owner']", "");
  setText("#asset-governance-error", "");
  if (!selectedGroupIds.length) { setText("#asset-governance-error", "请至少选择一项需要完善的资产"); return; }
  if (assetIds.length > 50) { setText("#asset-governance-error", `所选资产包含 ${assetIds.length} 条来源记录，请减少选择后重试`); return; }
  if (!owner || /^(?:待确权|待认领|待复核)$/i.test(owner)) { setText("[data-error-for='asset-owner']", "请填写实际负责该资产的部门"); qs("#asset-governance-owner").focus(); return; }
  if (!["公开", "内部", "机密"].includes(sensitivity)) { setText("#asset-governance-error", "请选择资产的敏感级别"); qs("#asset-governance-sensitivity").focus(); return; }
  submit.disabled = true;
  submit.setAttribute("aria-busy", "true");
  try {
    const response = await fetch(usesBatchEndpoint ? "/api/assets/metadata" : `/api/assets/${encodeURIComponent(assetIds[0])}/metadata`, { method: "PATCH", headers: { "content-type": "application/json" }, body: JSON.stringify(usesBatchEndpoint ? { assetIds, owner, sensitivity } : { owner, sensitivity }) });
    if (response.status === 401) { form.closest("dialog").close(); lockWorkspace(); return; }
    const payload = await response.json().catch(() => ({}));
    if (!response.ok || (usesBatchEndpoint ? !Array.isArray(payload.assets) : !payload.asset)) throw new Error(payload.error || "资产信息暂时无法保存");
    form.closest("dialog").close();
    await Promise.all([loadPublishedAssets(), loadAssetGraph(), loadAuditLedger(), loadDashboard()]);
    const assetFilter = qs("#asset-filter");
    if (assetFilter.value === "待复核") { assetFilter.value = "all"; updateAssetTableFilter(); }
    if (!wasBatch) {
      const updated = currentAssetPresentation.assets.find((asset) => asset.id === selectedGroupIds[0]) || payload.asset || payload.assets?.[0];
      populateAssetDrawer(updated, qs("#asset-governance-open"));
    }
    showToast(wasBatch ? `${selectedGroupIds.length} 项资产已统一确认` : "资产信息已确认", `${owner} · ${sensitivity} · 待办状态已同步`);
    pendingAssetGovernanceIds = [];
    assetGovernanceBatchMode = false;
  } catch (error) {
    setText("#asset-governance-error", String(error.message || "资产信息暂时无法保存"));
  } finally {
    submit.disabled = false;
    submit.removeAttribute("aria-busy");
    if (form.closest("dialog").open) updateAssetGovernanceSelectionCount();
  }
}

qs("#asset-governance-open").addEventListener("click", () => openAssetGovernanceDialog());
qs("#asset-governance-form").addEventListener("submit", saveAssetGovernance);
qs("#asset-governance-select-all").addEventListener("click", () => { qsa("#asset-governance-selection-list input[type='checkbox']").forEach((input) => { input.checked = true; }); updateAssetGovernanceSelectionCount(); });
qs("#asset-governance-select-none").addEventListener("click", () => { qsa("#asset-governance-selection-list input[type='checkbox']").forEach((input) => { input.checked = false; }); updateAssetGovernanceSelectionCount(); });

function bindAssetRow(row, asset = null) {
  row.setAttribute("role", "button");
  row.setAttribute("tabindex", "0");
  row.setAttribute("aria-label", `打开资产 ${row.dataset.name || asset?.title || "详情"}`);
  const activate = (event) => {
    if (event.target.closest("button")) return;
    if (asset) populateAssetDrawer(asset, row);
    else openDrawer(qs("#asset-drawer"), row);
  };
  row.addEventListener("click", activate);
  row.addEventListener("keydown", (event) => {
    if (event.key === "Enter" || event.key === " ") { event.preventDefault(); activate(event); }
  });
}

function renderPublishedAsset(asset) {
  if (qs(`[data-asset-id="${CSS.escape(asset.id)}"]`, qs("#asset-table-body"))) return;
  const row = document.createElement("tr");
  row.className = "clickable-row published-row";
  row.dataset.assetId = asset.id;
  row.dataset.name = `${asset.title} ${asset.id} ${(asset.sourceRecordIds || []).join(" ")} ${asset.tags.join(" ")}`;
  row.dataset.owner = asset.owner;
  row.dataset.sensitivity = asset.sensitivity;
  row.dataset.status = asset.sensitivity;
  row.dataset.type = asset.type;
  row.dataset.tags = asset.tags.join(" ").toLocaleLowerCase("zh-CN");
  const titleCell = document.createElement("td");
  const titleWrap = document.createElement("span");
  titleWrap.className = "asset-title";
  const icon = document.createElement("i"); icon.textContent = "已发布";
  const titleCopy = document.createElement("span");
  const title = document.createElement("strong"); title.textContent = asset.title;
  const id = document.createElement("small"); id.textContent = asset.duplicateCount ? `${asset.id} · ${asset.duplicateCount + 1} 条来源记录` : asset.id;
  titleCopy.append(title, id); titleWrap.append(icon, titleCopy); titleCell.append(titleWrap);
  const cells = [asset.type, asset.owner, asset.sensitivity, `${Math.round(Number(asset.confidence || 0) * 100)}%`].map((value) => { const cell = document.createElement("td"); cell.textContent = value; return cell; });
  const evidenceCell = document.createElement("td");
  if (asset.evidence[0]) evidenceCell.append(evidenceButton(asset.evidence[0], `${asset.evidence.length} 处`));
  else evidenceCell.textContent = "待补证";
  const statusCell = document.createElement("td");
  const status = document.createElement("span"); status.className = `status-chip ${asset.duplicateCount ? "warning" : "secure"}`; status.textContent = asset.duplicateCount ? `已合并展示 · ${asset.duplicateCount + 1} 条` : "已发布"; statusCell.append(status);
  row.append(titleCell, ...cells, evidenceCell, statusCell);
  bindAssetRow(row, asset);
  qs("#asset-table-body").prepend(row);
}

function renderOverviewAssets(assets) {
  const body = qs("#overview-asset-table-body");
  body.replaceChildren();
  for (const asset of assets.slice(0, 4)) {
    const row = document.createElement("tr");
    row.className = "clickable-row";
    row.dataset.assetId = asset.id;
    row.dataset.name = asset.title;
    const titleCell = document.createElement("td");
    const wrap = document.createElement("span");
    wrap.className = "asset-title";
    const icon = document.createElement("i"); icon.textContent = "IP";
    const copy = document.createElement("span");
    const title = document.createElement("strong"); title.textContent = asset.title;
    const id = document.createElement("small"); id.textContent = asset.id;
    copy.append(title, id); wrap.append(icon, copy); titleCell.append(wrap);
    const values = [asset.type, asset.owner, asset.sensitivity, `${Math.round(Number(asset.confidence || 0) * 100)}%`]
      .map((value) => { const cell = document.createElement("td"); cell.textContent = value || "—"; return cell; });
    const evidence = document.createElement("td");
    if (asset.evidence?.[0]) evidence.append(evidenceButton(asset.evidence[0], `${asset.evidence.length} 处`));
    else evidence.textContent = "待补证";
    row.append(titleCell, ...values, evidence);
    bindAssetRow(row, asset);
    body.append(row);
  }
  if (!assets.length) {
    const row = document.createElement("tr");
    const cell = document.createElement("td");
    cell.colSpan = 6;
    cell.className = "muted-copy";
    cell.textContent = "当前工作空间尚无已发布 IP 资产，请先接入文档并完成复核发布。";
    row.append(cell); body.append(row);
  }
}

function renderAssetDuplicateSummary(presentation) {
  const details = qs("#asset-duplicate-note");
  const list = qs("#asset-duplicate-list");
  details.hidden = presentation.duplicateRecordCount === 0;
  details.open = false;
  list.replaceChildren();
  if (!presentation.duplicateRecordCount) return;
  setText("#asset-duplicate-summary", `发现 ${presentation.duplicateRecordCount} 条重复来源记录`);
  presentation.assets.filter((asset) => asset.duplicateCount > 0).forEach((asset) => {
    const item = document.createElement("article");
    const title = document.createElement("strong");
    const ids = document.createElement("small");
    title.textContent = asset.title;
    ids.textContent = `${asset.sourceRecordIds.length} 条记录 · ${asset.sourceRecordIds.join("、")}`;
    item.append(title, ids);
    list.append(item);
  });
}

function renderAssetTagFilters(assets) {
  const options = qs("#asset-tag-options");
  options.replaceChildren();
  const tags = [...new Set(assets.flatMap((asset) => asset.tags || []))].sort((left, right) => left.localeCompare(right, "zh-CN"));
  if (!tags.length) {
    const empty = document.createElement("span");
    empty.className = "muted-copy";
    empty.textContent = "当前资产尚未添加标签";
    options.append(empty);
    qs("#asset-tag-filter").disabled = true;
    return;
  }
  qs("#asset-tag-filter").disabled = false;
  for (const tag of tags) {
    const button = document.createElement("button");
    button.type = "button";
    button.dataset.assetTag = tag.toLocaleLowerCase("zh-CN");
    button.textContent = tag;
    button.classList.toggle("is-active", activeAssetTag === button.dataset.assetTag);
    options.append(button);
  }
}

function applyAssetPresentationMetrics() {
  if (!currentAssetPresentation.rawCount && !currentDashboard) return;
  const uniqueCount = currentAssetPresentation.rawCount ? currentAssetPresentation.uniqueCount : Number(currentDashboard?.assets?.total || 0);
  const rawCount = currentAssetPresentation.rawCount || Number(currentDashboard?.assets?.total || 0);
  setText("#metric-assets-total", String(uniqueCount));
  setText("#metric-assets-period", `${rawCount} 条已发布记录`);
  const added = Number(currentDashboard?.assets?.addedThisMonth || 0);
  setText("#metric-assets-detail", currentAssetPresentation.duplicateRecordCount
    ? `本月新增 ${added} 条记录 · 合并展示 ${currentAssetPresentation.duplicateRecordCount} 条重复记录`
    : `本月新增 ${added} 项`);
  if (currentDashboard) setText("#overview-summary", `当前空间已沉淀 ${uniqueCount} 项独立 IP 资产，${currentDashboard.documents.processing} 份文档正在处理，${currentDashboard.risks.high} 项高敏风险待复核。`);
}

function renderWorkspaceDocuments() {
  if (["static-demo", "loopback-demo"].includes(currentSession?.mode)) return;
  const records = new Map();
  for (const job of workspaceJobs) {
    records.set(job.id, {
      id: job.id,
      name: job.document?.name || job.result?.analysis?.document?.title || job.id,
      category: job.result?.analysis?.document?.category || job.document?.expectedCategory || "自动判断",
      owner: "当前工作空间",
      status: operationStateLabels[job.state] || job.state,
      updatedAt: job.updatedAt,
      state: job.state,
    });
  }
  for (const asset of currentPublishedAssets) {
    const id = asset.sourceJobId || asset.publicationId;
    if (records.has(id)) continue;
    records.set(id, {
      id,
      name: asset.document?.sourceName || asset.document?.title || "已发布来源文档",
      category: asset.document?.category || "已发布资产来源",
      owner: asset.owner || "待确权",
      status: "已发布（迁移）",
      updatedAt: asset.publishedAt,
      state: "complete",
    });
  }
  const body = qs("#document-table-body");
  body.replaceChildren();
  for (const record of records.values()) {
    const row = document.createElement("tr");
    row.dataset.documentRow = "";
    row.dataset.name = record.name;
    row.dataset.owner = record.owner;
    row.dataset.status = record.status;
    row.dataset.category = record.category;
    const selectCell = document.createElement("td");
    const checkbox = document.createElement("input"); checkbox.type = "checkbox"; checkbox.setAttribute("aria-label", `选择 ${record.name}`); selectCell.append(checkbox);
    const documentCell = document.createElement("td");
    const wrap = document.createElement("span"); wrap.className = "asset-title";
    const type = document.createElement("i"); type.className = "file-type"; type.textContent = record.name.split(".").at(-1)?.slice(0, 4).toUpperCase() || "DOC";
    const copy = document.createElement("span"); const name = document.createElement("strong"); name.textContent = record.name; const id = document.createElement("small"); id.textContent = record.id; copy.append(name, id); wrap.append(type, copy); documentCell.append(wrap);
    const categoryCell = document.createElement("td"); const category = document.createElement("span"); category.className = "category-chip"; category.textContent = record.category; categoryCell.append(category);
    const ownerCell = document.createElement("td"); ownerCell.textContent = record.owner;
    const statusCell = document.createElement("td"); const status = document.createElement("span"); status.className = `status-chip ${record.state === "complete" ? "secure" : ["failed", "blocked", "interrupted"].includes(record.state) ? "danger" : "processing"}`; status.textContent = record.status; statusCell.append(status);
    const timeCell = document.createElement("td"); timeCell.textContent = formatDate(record.updatedAt);
    const actionCell = document.createElement("td"); const action = document.createElement("button"); action.type = "button"; action.className = "icon-btn small"; action.setAttribute("aria-label", `查看 ${record.name} 任务详情`); action.textContent = "•••"; action.addEventListener("click", () => { navigate("analysis"); const job = workspaceJobs.find((item) => item.id === record.id); if (job?.result) showRecentRealJob(job); else showEmptyRealAnalysis(record); showToast(job?.result ? "已打开文档任务" : "已打开历史文档说明", `${record.id} · ${record.status}`); }); actionCell.append(action);
    row.append(selectCell, documentCell, categoryCell, ownerCell, statusCell, timeCell, actionCell);
    body.append(row);
  }
  if (!records.size) {
    const row = document.createElement("tr"); const cell = document.createElement("td"); cell.colSpan = 7; cell.className = "muted-copy"; cell.textContent = "当前工作空间尚无文档，请点击“接入文档”开始。"; row.append(cell); body.append(row);
  }
  setText("#document-total", `当前工作空间 ${records.size} 份文档`);
  setText("#nav-document-count", String(records.size));
  qs("#document-pagination").hidden = records.size <= 20;
  populateDocumentAdvancedFilters();
  applyDocumentFilters();
}

function renderDynamicWiki(asset) {
  if (!asset) return;
  selectedPublishedAsset = asset;
  setText("#wiki-dynamic-type", `${asset.type} · ${asset.id}`);
  setText("#wiki-dynamic-title", asset.wiki?.title || asset.title);
  setText("#wiki-dynamic-summary", asset.summary);
  setText("#wiki-owner", asset.owner);
  setText("#wiki-version", asset.version);
  setText("#wiki-confidence", `${Math.round(Number(asset.confidence || 0) * 100)}%`);
  setText("#wiki-evidence-count", `${asset.evidence.length} 处`);
  setText("#wiki-sensitivity", asset.sensitivity);
  setText("#wiki-publication-state", "已发布");
  setText("#wiki-updated-at", `${asset.wiki?.updatedAt ? "更新于" : "发布于"} ${new Date(asset.wiki?.updatedAt || asset.publishedAt).toLocaleString("zh-CN")}`);
  setText("#wiki-executive-summary", asset.wiki?.executiveSummary || asset.summary);
  setText("#wiki-overview-copy", asset.document?.summary || asset.summary);
  setText("#wiki-source-name", asset.document?.sourceName);
  setText("#wiki-source-meta", `原文已核验 · 读取批次 ${asset.document?.parserBatchId || "—"} · 内容校验码 ${asset.document?.markdownSha256?.slice(0, 12) || "—"}…`);
  const mechanism = qs("#wiki-mechanism-grid");
  mechanism.replaceChildren();
  const mechanismCard = document.createElement("article");
  const marker = document.createElement("i"); marker.textContent = "AI";
  const heading = document.createElement("h3"); heading.textContent = "关键机制";
  const copy = document.createElement("p"); copy.textContent = asset.wiki?.keyMechanism || "等待人工补充机制说明";
  mechanismCard.append(marker, heading, copy);
  if (asset.evidence[0]) mechanismCard.append(evidenceButton(asset.evidence[0]));
  mechanism.append(mechanismCard);
  const metrics = qs("#wiki-metric-grid");
  metrics.replaceChildren();
  const metricItems = asset.wiki?.metrics?.length ? asset.wiki.metrics : [{ label: "综合置信度", value: `${Math.round(Number(asset.confidence || 0) * 100)}%` }];
  metricItems.forEach((item) => { const card = document.createElement("div"); const label = document.createElement("span"); const value = document.createElement("strong"); const note = document.createElement("small"); label.textContent = item.label; value.textContent = item.value; note.textContent = "来自当前文档分析结果"; card.append(label, value, note); metrics.append(card); });
  const relationships = qs("#wiki-relationship-map");
  relationships.replaceChildren();
  relationships.classList.add("dynamic-relationship-list");
  const relationItems = asset.wiki?.relationships?.length ? asset.wiki.relationships : [{ source: asset.title, relation: "来源于", target: asset.document?.title }];
  relationItems.forEach((item) => { const card = document.createElement("article"); const source = document.createElement("strong"); const relation = document.createElement("span"); const target = document.createElement("b"); source.textContent = item.source; relation.textContent = RELATION_LABELS[item.relation] || item.relation || "关联"; target.textContent = item.target; card.append(source, relation, target); relationships.append(card); });
  qsa("[data-evidence-id]", qs("#view-wiki")).forEach((button) => button.remove());
  if (asset.evidence[0]) qs("#wiki-overview .insight-callout p").append(document.createTextNode(" "), evidenceButton(asset.evidence[0]));
  const canEdit = ["owner", "admin", "editor"].includes(currentSession?.user?.role);
  qs("#wiki-edit").disabled = !canEdit || !asset.id.startsWith("IP-REAL-");
}

async function loadPublishedAssets() {
  try {
    const response = await fetch("/api/assets", { headers: { accept: "application/json" } });
    if (response.status === 401) { lockWorkspace(); return; }
    if (!response.ok) return;
    const { assets = [] } = await response.json();
    currentPublishedAssets = assets;
    currentAssetPresentation = groupAssetsForDisplay(assets);
    if (!["static-demo", "loopback-demo"].includes(currentSession?.mode)) {
      qsa("#asset-table-body [data-demo-asset]").forEach((row) => row.remove());
      qsa("#asset-table-body .published-row").forEach((row) => row.remove());
    }
    currentAssetPresentation.assets.slice().reverse().forEach(renderPublishedAsset);
    renderOverviewAssets(currentAssetPresentation.assets);
    renderWorkspaceDocuments();
    if (currentAssetPresentation.assets[0]) renderDynamicWiki(currentAssetPresentation.assets[0]);
    renderAssetDuplicateSummary(currentAssetPresentation);
    renderAssetTagFilters(currentAssetPresentation.assets);
    refreshAssetTabs();
    updateAssetTableFilter();
    setText("#asset-total", `${currentAssetPresentation.uniqueCount} 项独立资产 · ${currentAssetPresentation.rawCount} 条记录`);
    qs("#asset-empty-state").hidden = currentAssetPresentation.uniqueCount > 0;
    applyAssetPresentationMetrics();
    renderLifecycleGovernance();
    renderWorkspaceActions();
    if (currentRealJob?.result) renderRealResults(currentRealJob.result);
  } catch {
    // Static acceptance mode intentionally has no API gateway.
  }
}

async function publishCurrentAnalysis() {
  if (!currentRealJob?.id) return;
  const button = qs("#publish-analysis");
  button.disabled = true;
  button.setAttribute("aria-busy", "true");
  setText("#publication-status", "发布中");
  try {
    const response = await fetch(`/api/analysis/${encodeURIComponent(currentRealJob.id)}/publish`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ owner: "待确权", sensitivity: "待复核" }) });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok || !payload.publication) throw new Error(payload.error || "发布失败");
    payload.publication.assets.forEach((asset) => {
      const existingIndex = currentPublishedAssets.findIndex((item) => item.id === asset.id);
      if (existingIndex >= 0) currentPublishedAssets[existingIndex] = asset;
      else currentPublishedAssets.unshift(asset);
    });
    renderRealResults(currentRealJob.result);
    const first = payload.publication.assets[0];
    if (first) renderDynamicWiki(first);
    await loadPublishedAssets();
    await loadAssetGraph();
    setText("#publication-status", "已发布");
    button.textContent = "✓ 已发布到资产库与 Wiki";
    const audit = makeAuditEvent("发布真实 IP 资产", `${payload.publication.assets.length} 项资产已形成 ${payload.publication.version} 证据快照`);
    state.auditEvents = appendAudit(state.auditEvents, audit);
    appendAuditToDom(audit);
    saveState();
    showToast("发布完成", `${payload.publication.assets.length} 项真实资产现在可在资产库与 Wiki 检索`);
  } catch (error) {
    button.disabled = false;
    setText("#publication-status", "发布失败");
    showToast("无法发布", String(error.message || "请稍后重试"), "error");
  } finally {
    button.removeAttribute("aria-busy");
  }
}

async function pollRealJob(jobId) {
  const deadline = Date.now() + 15 * 60_000;
  while (Date.now() < deadline) {
    const response = await fetch(`/api/analysis/${encodeURIComponent(jobId)}`, { headers: { accept: "application/json" } });
    if (!response.ok) throw new Error("无法读取真实分析任务状态");
    const { job } = await response.json();
    state.analysis = {
      ...state.analysis,
      id: job.id,
      progress: Number(job.progress || 0),
      status: job.state === "complete" ? "complete" : job.state === "failed" ? "failed" : job.state === "blocked" ? "blocked" : "running",
      liveText: job.stageLabel,
      updatedAt: "刚刚",
    };
    updateAnalysisUI();
    saveState();
    if (job.state === "complete") return job;
    if (["failed", "blocked"].includes(job.state)) throw new Error(job.error || (job.state === "blocked" ? "文件已被安全策略拦截" : "真实分析失败"));
    await new Promise((resolve) => setTimeout(resolve, 2_000));
  }
  throw new Error("真实分析等待超时");
}

async function runRealAnalysis(intake) {
  const payload = new FormData();
  payload.append("file", selectedRealFile, selectedRealFile.name);
  payload.append("category", intake.category);
  const response = await fetch("/api/analysis", { method: "POST", body: payload });
  const submitted = await response.json().catch(() => ({}));
  if (!response.ok || !submitted.job?.id) {
    if (response.status === 401) { lockWorkspace(); throw new Error("登录状态已失效，请重新登录后接入文档"); }
    if (response.status === 403) throw new Error(analysisPermissionMessage);
    throw new Error(submitted.error || "文档未能进入分析，请检查文件后重试");
  }
  state.analysis = {
    ...state.analysis,
    id: submitted.job.id,
    mode: "real",
    document: intake.name,
    category: intake.category,
    progress: Number(submitted.job.progress || 2),
    status: "running",
    liveText: submitted.job.stageLabel || "文档已受理，正在进行安全检查",
    updatedAt: "刚刚",
  };
  qs("#real-analysis-results").hidden = true;
  qs("#intake-dialog").close();
  navigate("analysis");
  updateAnalysisUI();
  saveState();
  showToast("文档已受理", "权限与文件检查通过，正在读取文档内容");
  const job = await pollRealJob(submitted.job.id);
  currentRealJob = job;
  recentRealJobs = [job, ...recentRealJobs.filter((entry) => entry.id !== job.id)];
  populateRecentRealJobs();
  qs("#real-job-select").value = job.id;
  state.analysis.category = job.result.analysis.document.category;
  state.analysis.liveText = `${job.result.analysis.assets.length} 项真实 IP 资产与 Wiki 已生成`;
  renderRealResults(job.result);
  updateAnalysisUI();
  saveState();
  const audit = makeAuditEvent("完成真实 IP 智能分析", `MinerU ${job.result.parser.batchId} 与 ${job.result.llm.model} 生成 ${job.result.analysis.assets.length} 项资产`);
  state.auditEvents = appendAudit(state.auditEvents, audit);
  appendAuditToDom(audit);
  showToast("真实分析完成", `MinerU 与 ${job.result.llm.model} 已返回可溯源结果`);
}

qs("#publish-analysis").addEventListener("click", publishCurrentAnalysis);
qs("#real-job-select").addEventListener("change", (event) => {
  const selected = recentRealJobs.find((job) => job.id === event.currentTarget.value);
  if (selected) showRecentRealJob(selected);
});

qs("#intake-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  clearErrors(form);
  const result = validateIntake(Object.fromEntries(new FormData(form)));
  if (!result.valid) {
    applyErrors(form, result.errors);
    form.querySelector("[aria-invalid='true']")?.focus();
    showToast("请完善接入信息", "文档名称和分类为必填项", "error");
    return;
  }
  const submitButton = qs("[data-testid='start-analysis']", form);
  setText("#intake-submit-error", "");
  if (selectedRealFile) {
    if (!canAnalyzeDocuments()) {
      setText("#intake-submit-error", analysisPermissionMessage);
      showToast("当前岗位没有文档接入权限", analysisPermissionMessage, "error");
      return;
    }
    submitButton.disabled = true;
    submitButton.setAttribute("aria-busy", "true");
    setText("#intake-submit-error", "正在验证账号权限与文件安全，请稍候…");
    try {
      await runRealAnalysis(result.value);
    } catch (error) {
      const message = String(error.message || "文档未能进入分析");
      setText("#intake-submit-error", message);
      showToast("文档未进入分析", message, "error");
    } finally {
      submitButton.disabled = false;
      submitButton.removeAttribute("aria-busy");
    }
    return;
  }
  state.analysis = {
    ...state.analysis,
    document: result.value.name,
    category: result.value.category,
    progress: 8,
    status: "running",
    mode: "demo",
    liveText: "正在处理：离线演示文档",
    updatedAt: "刚刚",
  };
  const audit = makeAuditEvent("启动文档分析", `接入文档「${result.value.name}」，分类为${result.value.category}`);
  state.auditEvents = appendAudit(state.auditEvents, audit);
  appendAuditToDom(audit);
  saveState();
  updateAnalysisUI();
  qs("#intake-dialog").close();
  navigate("analysis");
  qs("#real-analysis-results").hidden = true;
  showToast("演示分析已启动", "此任务只使用本地示例，不会上传企业文档");
});

qs("#advance-analysis").addEventListener("click", () => {
  if (state.analysis.mode === "real") return;
  const previous = state.analysis.status;
  state.analysis = advanceAnalysis(state.analysis, 28);
  updateAnalysisUI();
  saveState();
  if (state.analysis.status === "complete" && previous !== "complete") {
    const audit = makeAuditEvent("完成 IP 智能分析", "生成 18 项结构化资产、14 处原文依据与 1 个 Wiki");
    state.auditEvents = appendAudit(state.auditEvents, audit);
    appendAuditToDom(audit);
    showToast("分析完成", "18 项 IP 资产已沉淀，Wiki 与风险线索可供复核");
  } else {
    showToast("分析进度已推进", `当前完成度 ${state.analysis.progress}%`);
  }
});

function bindTableFilter(inputSelector, filterSelector, rowSelector, fields) {
  const input = qs(inputSelector);
  const filter = qs(filterSelector);
  const update = () => {
    const query = input.value.trim().toLocaleLowerCase("zh-CN");
    const choice = filter.value;
    qsa(rowSelector).forEach((row) => {
      const haystack = fields.map((field) => row.dataset[field] ?? "").join(" ").toLocaleLowerCase("zh-CN");
      const category = row.dataset.status ?? row.dataset.sensitivity ?? "";
      const typeMismatch = rowSelector.includes("asset-table-body") && activeAssetType !== "all" && row.dataset.type !== activeAssetType;
      const tagMismatch = rowSelector.includes("asset-table-body") && activeAssetTag !== "all" && !String(row.dataset.tags || "").split(" ").includes(activeAssetTag);
      row.hidden = Boolean(query && !haystack.includes(query)) || Boolean(choice !== "all" && category !== choice) || typeMismatch || tagMismatch;
    });
    if (rowSelector.includes("asset-table-body")) {
      const visible = qsa(rowSelector).filter((row) => !row.hidden).length;
      qs("#asset-empty-state").hidden = visible > 0;
    }
  };
  input.addEventListener("input", update);
  filter.addEventListener("change", update);
  return update;
}

function replaceFilterOptions(select, values, allLabel) {
  const previous = select.value;
  select.replaceChildren();
  const all = document.createElement("option");
  all.value = "all";
  all.textContent = allLabel;
  select.append(all);
  values.filter(Boolean).sort((left, right) => left.localeCompare(right, "zh-CN")).forEach((value) => {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = value;
    select.append(option);
  });
  select.value = [...select.options].some((option) => option.value === previous) ? previous : "all";
}

function populateDocumentAdvancedFilters() {
  const rows = qsa("[data-document-row]");
  replaceFilterOptions(qs("#document-category-filter"), [...new Set(rows.map((row) => row.dataset.category))], "全部分类");
  replaceFilterOptions(qs("#document-owner-filter"), [...new Set(rows.map((row) => row.dataset.owner))], "全部部门");
}

function applyDocumentFilters() {
  const query = qs("#document-search").value.trim().toLocaleLowerCase("zh-CN");
  const status = qs("#document-filter").value;
  const category = qs("#document-category-filter").value;
  const owner = qs("#document-owner-filter").value;
  const actionOnly = qs("#document-action-filter").checked;
  const actionPattern = /分析中|待复核|失败|等待恢复|安全拦截|已入队|安全检查|解析|分析/;
  let visible = 0;
  const rows = qsa("[data-document-row]");
  for (const row of rows) {
    const haystack = `${row.dataset.name || ""} ${row.dataset.owner || ""}`.toLocaleLowerCase("zh-CN");
    const hidden = Boolean(query && !haystack.includes(query))
      || Boolean(status !== "all" && row.dataset.status !== status)
      || Boolean(category !== "all" && row.dataset.category !== category)
      || Boolean(owner !== "all" && row.dataset.owner !== owner)
      || Boolean(actionOnly && !actionPattern.test(row.dataset.status || ""));
    row.hidden = hidden;
    if (!hidden) visible += 1;
  }
  setText("#document-filter-status", `${visible} / ${rows.length} 份文档`);
}

populateDocumentAdvancedFilters();
applyDocumentFilters();
const updateAssetTableFilter = bindTableFilter("#asset-search", "#asset-filter", "#asset-table-body [data-asset-id]", ["name", "owner", "tags"]);

qsa("[data-asset-id]").forEach((row) => bindAssetRow(row));
qsa("#asset-table-body [data-asset-id]").forEach((row) => { row.dataset.type = row.children[1]?.textContent.trim() || "知识资产"; });

function refreshAssetTabs() {
  const rows = qsa("#asset-table-body [data-asset-id]");
  qsa(".asset-tabs button").forEach((button, index) => {
    const type = index === 0 ? "all" : button.childNodes[0]?.textContent.trim();
    button.dataset.assetType = type;
    const count = type === "all" ? rows.length : rows.filter((row) => row.dataset.type === type).length;
    const badge = qs("span", button);
    if (badge) badge.textContent = String(count);
    if (button.dataset.assetTabBound === "true") return;
    button.dataset.assetTabBound = "true";
    button.addEventListener("click", () => {
      activeAssetType = type;
      qsa(".asset-tabs button").forEach((item) => item.classList.toggle("is-active", item === button));
      qs("#asset-search").dispatchEvent(new Event("input"));
    });
  });
}
refreshAssetTabs();
renderAssetTagFilters(currentAssetPresentation.assets);

qsa("[data-open-provenance]").forEach((button) => {
  button.addEventListener("click", (event) => {
    event.stopPropagation();
    const evidenceId = event.currentTarget.dataset.evidenceId;
    const evidence = evidenceId && currentPublishedAssets.flatMap((asset) => asset.evidence).find((item) => item.id === evidenceId);
    if (evidence) { renderEvidence(evidence, event.currentTarget); return; }
    if (evidenceId === DEMO_EVIDENCE.id && ["static-demo", "loopback-demo"].includes(currentSession?.mode)) { renderEvidence(DEMO_EVIDENCE, event.currentTarget); return; }
    activeEvidence = null;
    closeDrawers(false);
    showToast("无法打开证据", "该入口未绑定当前工作空间内可验证的证据，已阻止沿用上一次查看内容。", "error");
  });
});

qs("#asset-view-evidence").addEventListener("click", (event) => {
  const evidence = selectedPublishedAsset?.evidence?.[0];
  if (evidence) renderEvidence(evidence, event.currentTarget);
});

qs("#asset-open-wiki").addEventListener("click", () => {
  if (selectedPublishedAsset) renderDynamicWiki(selectedPublishedAsset);
});

qs("#copy-evidence-link").addEventListener("click", async () => {
  if (!activeEvidence) { showToast("演示证据", "当前展示为离线演示证据，未生成可复制的真实编号", "error"); return; }
  try {
    await navigator.clipboard.writeText(activeEvidence.id);
    showToast("证据编号已复制", activeEvidence.id);
  } catch {
    showToast("复制失败", `请手动复制 ${activeEvidence.id}`, "error");
  }
});

function updateWikiSearch() {
  const query = qs("#wiki-search").value.trim().toLocaleLowerCase("zh-CN");
  let matches = 0;
  qsa("[data-wiki-search-section]").forEach((section) => {
    const match = !query || section.textContent.toLocaleLowerCase("zh-CN").includes(query);
    section.hidden = !match;
    section.classList.toggle("is-search-match", Boolean(query && match));
    if (match) matches += 1;
  });
  setText("#wiki-search-status", query ? `找到 ${matches} 个匹配章节` : "输入关键词定位章节");
}

qs("#wiki-search").addEventListener("input", updateWikiSearch);
qs("#wiki-search").addEventListener("keydown", (event) => {
  if (event.key === "Enter") qs("[data-wiki-search-section].is-search-match")?.scrollIntoView({ behavior: "smooth", block: "start" });
});

qs("#wiki-focus-toggle").addEventListener("click", (event) => {
  const active = qs("#view-wiki").classList.toggle("is-focus-mode");
  event.currentTarget.setAttribute("aria-pressed", String(active));
  event.currentTarget.textContent = active ? "退出专注" : "专注阅读";
});

function wikiEditableSnapshot() {
  return {
    title: qs("#wiki-edit-title").value.trim(),
    executiveSummary: qs("#wiki-edit-summary").value.trim(),
    keyMechanism: qs("#wiki-edit-mechanism").value.trim(),
  };
}

function updateWikiChangePreview() {
  const preview = qs("#wiki-change-preview");
  const list = qs("#wiki-change-list");
  const save = qs("[data-testid='wiki-save']");
  if (!wikiEditBaseline) {
    preview.hidden = true;
    save.disabled = true;
    return;
  }
  const current = wikiEditableSnapshot();
  const labels = { title: "标题", executiveSummary: "核心摘要", keyMechanism: "核心机制" };
  const changed = Object.keys(labels).filter((key) => current[key] !== wikiEditBaseline[key]);
  list.replaceChildren(...changed.map((key) => {
    const item = document.createElement("li");
    item.textContent = `${labels[key]}已修改`;
    return item;
  }));
  if (!changed.length) {
    const item = document.createElement("li");
    item.textContent = "尚未修改内容";
    list.append(item);
  }
  setText("#wiki-change-count", changed.length ? `${changed.length} 个内容区域将形成新版本` : "没有变化，不会创建新版本");
  preview.hidden = false;
  save.disabled = changed.length === 0;
}

qsa("#wiki-edit-title, #wiki-edit-summary, #wiki-edit-mechanism").forEach((field) => field.addEventListener("input", updateWikiChangePreview));

qs("#wiki-edit").addEventListener("click", () => {
  if (!selectedPublishedAsset?.id?.startsWith("IP-REAL-")) { showToast("请选择真实 Wiki", "演示内容保持只读；发布真实分析结果后可形成版本链", "error"); return; }
  const wiki = selectedPublishedAsset.wiki || {};
  qs("#wiki-edit-base-version").value = selectedPublishedAsset.version;
  qs("#wiki-edit-title").value = wiki.title || selectedPublishedAsset.title;
  qs("#wiki-edit-summary").value = wiki.executiveSummary || selectedPublishedAsset.summary;
  qs("#wiki-edit-mechanism").value = wiki.keyMechanism || "";
  qs("#wiki-edit-note").value = "";
  wikiEditBaseline = wikiEditableSnapshot();
  setText("#wiki-edit-error", "");
  setText("#wiki-edit-version-note", ["owner", "admin"].includes(currentSession?.user?.role) ? `基于 ${selectedPublishedAsset.version} 创建新版本` : `基于 ${selectedPublishedAsset.version} 提交审批`);
  updateWikiChangePreview();
  showDialog(qs("#wiki-edit-dialog"));
  qs("#wiki-edit-title").focus();
});

qs("#wiki-edit-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!selectedPublishedAsset) return;
  const form = event.currentTarget;
  const submit = qs("[type='submit']", form);
  const input = Object.fromEntries(new FormData(form));
  setText("#wiki-edit-error", "");
  if (!String(input.title).trim() || !String(input.executiveSummary).trim() || !String(input.keyMechanism).trim()) { setText("#wiki-edit-error", "标题、核心摘要和核心机制均不能为空"); return; }
  const currentSnapshot = wikiEditableSnapshot();
  const unchanged = !wikiEditBaseline || Object.keys(currentSnapshot).every((key) => currentSnapshot[key] === wikiEditBaseline[key]);
  if (unchanged) { setText("#wiki-edit-error", "内容没有变化，不会创建新版本"); return; }
  submit.disabled = true;
  submit.setAttribute("aria-busy", "true");
  try {
    const needsApproval = !["owner", "admin"].includes(currentSession?.user?.role);
    const endpoint = needsApproval ? `/api/wiki/${encodeURIComponent(selectedPublishedAsset.id)}/reviews` : `/api/wiki/${encodeURIComponent(selectedPublishedAsset.id)}`;
    const response = await fetch(endpoint, { method: needsApproval ? "POST" : "PATCH", headers: { "content-type": "application/json" }, body: JSON.stringify(input) });
    const payload = await response.json().catch(() => ({}));
    if (response.status === 401) { form.closest("dialog").close(); lockWorkspace(); return; }
    if (needsApproval) {
      if (!response.ok || !payload.review) {
        if (response.status === 409) throw new Error("该 Wiki 已有一项待审批更新，请先等待管理员处理");
        throw new Error(payload.error || "发布审批提交失败");
      }
      form.closest("dialog").close();
      await loadWikiReviews();
      navigate("tasks");
      showToast("Wiki 更新已提交审批", `${payload.review.assetTitle} · 已发布版本保持 ${payload.review.baseVersion}`);
      return;
    }
    if (!response.ok || !payload.wiki) {
      if (response.status === 409 && /not changed/i.test(String(payload.error || ""))) throw new Error("内容没有变化，不会创建新版本");
      throw new Error(response.status === 409 ? `版本已更新到 ${payload.currentVersion || "最新版本"}，请重新打开后编辑` : payload.error || "Wiki 保存失败");
    }
    const updatedAsset = {
      ...selectedPublishedAsset,
      title: payload.wiki.title,
      version: payload.wiki.version,
      wiki: {
        ...selectedPublishedAsset.wiki,
        title: payload.wiki.title,
        executiveSummary: payload.wiki.executiveSummary,
        keyMechanism: payload.wiki.keyMechanism,
        metrics: payload.wiki.metrics,
        relationships: payload.wiki.relationships,
        updatedAt: payload.wiki.updatedAt,
      },
    };
    const index = currentPublishedAssets.findIndex((asset) => asset.id === updatedAsset.id);
    if (index >= 0) currentPublishedAssets[index] = updatedAsset;
    renderDynamicWiki(updatedAsset);
    form.closest("dialog").close();
    showToast("Wiki 新版本已保存", `${payload.wiki.version} · ${input.changeNote || "内容更新"}`);
    await Promise.all([loadPublishedAssets(), loadAuditLedger(), loadDashboard()]);
  } catch (error) {
    setText("#wiki-edit-error", String(error.message || "Wiki 保存失败"));
  } finally {
    submit.disabled = false;
    submit.removeAttribute("aria-busy");
  }
});

qs("#wiki-version-history").addEventListener("click", async (event) => {
  if (!selectedPublishedAsset?.id?.startsWith("IP-REAL-")) { showToast("暂无真实版本链", "发布真实分析结果后，每次人工更新都会生成不可覆盖的版本", "error"); return; }
  const list = qs("#wiki-version-list");
  list.replaceChildren();
  const loading = document.createElement("div"); loading.className = "version-empty"; loading.textContent = "正在读取版本账本…"; list.append(loading);
  openDrawer(qs("#wiki-history-drawer"), event.currentTarget);
  try {
    const response = await fetch(`/api/wiki/${encodeURIComponent(selectedPublishedAsset.id)}/versions`, { headers: { accept: "application/json" } });
    const payload = await response.json().catch(() => ({}));
    if (response.status === 401) { closeDrawers(); lockWorkspace(); return; }
    if (!response.ok) throw new Error(payload.error || "无法读取版本历史");
    list.replaceChildren();
    payload.versions.forEach((version, index) => {
      const card = document.createElement("article"); card.className = `version-card${index === 0 ? " is-current" : ""}`;
      const number = document.createElement("span"); number.className = "version-number"; number.textContent = version.version;
      const copy = document.createElement("div"); const title = document.createElement("strong"); const note = document.createElement("p");
      title.textContent = version.title; note.textContent = `${version.changeNote || "内容更新"} · ${version.editor?.name || "系统生成"}`; copy.append(title, note);
      const time = document.createElement("small"); time.textContent = new Date(version.updatedAt).toLocaleString("zh-CN");
      card.append(number, copy, time); list.append(card);
    });
  } catch (error) {
    list.replaceChildren(); const failure = document.createElement("div"); failure.className = "version-empty"; failure.textContent = String(error.message || "无法读取版本历史"); list.append(failure);
  }
});
qs("#asset-tag-filter").addEventListener("click", (event) => {
  const panel = qs("#asset-tag-filter-panel");
  panel.hidden = !panel.hidden;
  event.currentTarget.setAttribute("aria-expanded", String(!panel.hidden));
  if (!panel.hidden) qs("[data-asset-tag]", panel)?.focus();
});
qs("#asset-tag-options").addEventListener("click", (event) => {
  const button = event.target.closest("[data-asset-tag]");
  if (!button) return;
  activeAssetTag = button.dataset.assetTag;
  qsa("[data-asset-tag]", qs("#asset-tag-options")).forEach((item) => item.classList.toggle("is-active", item === button));
  updateAssetTableFilter();
  setText("#asset-total", `已按标签“${button.textContent}”筛选`);
});
qs("#asset-tag-reset").addEventListener("click", () => {
  activeAssetTag = "all";
  qsa("[data-asset-tag]", qs("#asset-tag-options")).forEach((item) => item.classList.remove("is-active"));
  updateAssetTableFilter();
  setText("#asset-total", `${currentAssetPresentation.uniqueCount} 项独立资产 · ${currentAssetPresentation.rawCount} 条记录`);
});

qs("#graph-search").addEventListener("input", renderAssetGraph);
qs("#graph-type-filter").addEventListener("change", renderAssetGraph);
qs("#graph-relation-filter").addEventListener("change", renderAssetGraph);
qs("#graph-include-proposed").addEventListener("change", loadAssetGraph);
qs("#graph-reset").addEventListener("click", () => {
  graphFocusId = null;
  selectedGraphNode = null;
  selectedGraphRelationship = null;
  graphCamera = defaultGraphCamera();
  qs("#graph-search").value = "";
  qs("#graph-type-filter").value = "all";
  qs("#graph-relation-filter").value = "all";
  qs("#graph-include-proposed").checked = !qs("#graph-include-proposed").disabled;
  qs("#graph-inspector").hidden = true;
  qs("#relationship-inspector").hidden = true;
  renderAssetGraph();
});
qs("#graph-inspector-close").addEventListener("click", () => {
  selectedGraphNode = null;
  qs("#graph-inspector").hidden = true;
  renderAssetGraph();
});
qs("#relationship-inspector-close").addEventListener("click", () => {
  selectedGraphRelationship = null;
  qs("#relationship-inspector").hidden = true;
  renderAssetGraph();
});
qs("#relationship-confirm").addEventListener("click", () => reviewSelectedRelationship("confirmed"));
qs("#relationship-reject").addEventListener("click", () => reviewSelectedRelationship("rejected"));
qs("#relationship-open-evidence").addEventListener("click", (event) => {
  const evidenceIds = new Set(selectedGraphRelationship?.evidenceIds || []);
  const evidence = currentPublishedAssets.flatMap((asset) => asset.evidence || []).find((item) => evidenceIds.has(item.id));
  if (evidence) renderEvidence(evidence, event.currentTarget);
});
qs("#graph-focus-neighborhood").addEventListener("click", () => {
  if (!selectedGraphNode) return;
  graphFocusId = selectedGraphNode.id;
  graphCamera = defaultGraphCamera();
  qs("#graph-inspector").hidden = true;
  selectedGraphNode = null;
  renderAssetGraph();
  qs(`[data-graph-node-id="${CSS.escape(graphFocusId)}"]`)?.focus();
});
qs("#graph-open-asset").addEventListener("click", (event) => {
  if (selectedGraphNode) populateAssetDrawer(graphAsset(selectedGraphNode), event.currentTarget);
});
qs("#asset-graph").addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  if (graphFocusId) {
    graphFocusId = null;
    graphCamera = defaultGraphCamera();
    renderAssetGraph();
  } else {
    selectedGraphNode = null;
    selectedGraphRelationship = null;
    qs("#graph-inspector").hidden = true;
    qs("#relationship-inspector").hidden = true;
    renderAssetGraph();
  }
});
function updateGraphScale(next, anchor = { x: 540, y: 310 }) {
  graphCamera = zoomGraphCameraAt(graphCamera, next, anchor);
  applyGraphCamera();
}

function graphPointerSnapshot() {
  return [...graphPointers.values()];
}

function graphDistance([left, right]) {
  return Math.max(1, Math.hypot(right.x - left.x, right.y - left.y));
}

function graphMidpoint([left, right]) {
  return { x: (left.x + right.x) / 2, y: (left.y + right.y) / 2 };
}

function beginGraphGesture() {
  const points = graphPointerSnapshot();
  if (!points.length) { graphGesture = null; return; }
  if (points.length === 1) {
    graphGesture = { mode: "pan", camera: { ...graphCamera }, point: { ...points[0] } };
    return;
  }
  const pair = points.slice(0, 2);
  graphGesture = {
    mode: "pinch",
    camera: { ...graphCamera },
    distance: graphDistance(pair),
    midpoint: graphMidpoint(pair),
  };
}

const graphSvg = qs("#asset-graph");
qs("#graph-zoom-in").addEventListener("click", () => updateGraphScale(graphCamera.scale * 1.2));
qs("#graph-zoom-out").addEventListener("click", () => updateGraphScale(graphCamera.scale / 1.2));
qs("#graph-zoom-reset").addEventListener("click", resetGraphCamera);
graphSvg.addEventListener("wheel", (event) => {
  event.preventDefault();
  updateGraphScale(graphCamera.scale * (event.deltaY < 0 ? 1.14 : 1 / 1.14), graphCameraPoint(event));
}, { passive: false });
graphSvg.addEventListener("pointerdown", (event) => {
  if (event.target.closest(".graph-node, .graph-edge-hit")) return;
  graphSvg.setPointerCapture(event.pointerId);
  graphPointers.set(event.pointerId, graphCameraPoint(event));
  beginGraphGesture();
  graphSvg.classList.add("is-panning");
});
graphSvg.addEventListener("pointermove", (event) => {
  if (!graphPointers.has(event.pointerId) || !graphGesture) return;
  graphPointers.set(event.pointerId, graphCameraPoint(event));
  const points = graphPointerSnapshot();
  if (points.length >= 2 && graphGesture.mode === "pinch") {
    const pair = points.slice(0, 2);
    const midpoint = graphMidpoint(pair);
    const scale = normalizeGraphCamera({ scale: graphGesture.camera.scale * graphDistance(pair) / graphGesture.distance }).scale;
    const worldX = (graphGesture.midpoint.x - graphGesture.camera.x) / graphGesture.camera.scale;
    const worldY = (graphGesture.midpoint.y - graphGesture.camera.y) / graphGesture.camera.scale;
    graphCamera = { x: midpoint.x - worldX * scale, y: midpoint.y - worldY * scale, scale };
  } else if (points.length === 1 && graphGesture.mode === "pan") {
    graphCamera = panGraphCamera(graphGesture.camera, { x: points[0].x - graphGesture.point.x, y: points[0].y - graphGesture.point.y });
  } else {
    beginGraphGesture();
    return;
  }
  applyGraphCamera({ announce: false });
});
function finishGraphPointer(event) {
  if (!graphPointers.has(event.pointerId)) return;
  graphPointers.delete(event.pointerId);
  if (graphPointers.size) beginGraphGesture();
  else {
    graphGesture = null;
    graphSvg.classList.remove("is-panning");
  }
}
graphSvg.addEventListener("pointerup", finishGraphPointer);
graphSvg.addEventListener("pointercancel", finishGraphPointer);
graphSvg.addEventListener("dblclick", (event) => {
  if (!event.target.closest(".graph-node, .graph-edge-hit")) resetGraphCamera();
});
qs("#asset-mode-graph").addEventListener("click", () => {
  qs("#view-assets").classList.remove("asset-list-only");
  qs("#asset-mode-graph").classList.add("is-active");
  qs("#asset-mode-list").classList.remove("is-active");
  qs("#asset-mode-graph").setAttribute("aria-pressed", "true");
  qs("#asset-mode-list").setAttribute("aria-pressed", "false");
  qs("#asset-graph-panel").scrollIntoView({ behavior: "smooth", block: "start" });
});
qs("#asset-mode-list").addEventListener("click", () => {
  qs("#view-assets").classList.add("asset-list-only");
  qs("#asset-mode-list").classList.add("is-active");
  qs("#asset-mode-graph").classList.remove("is-active");
  qs("#asset-mode-list").setAttribute("aria-pressed", "true");
  qs("#asset-mode-graph").setAttribute("aria-pressed", "false");
  qs("#asset-list-panel").scrollIntoView({ behavior: "smooth", block: "start" });
});

function findButton(root, label) {
  return qsa("button", root).find((button) => button.textContent.trim() === label);
}

qs("#document-advanced-filter")?.addEventListener("click", (event) => {
  const panel = qs("#document-advanced-filters");
  panel.hidden = !panel.hidden;
  event.currentTarget.setAttribute("aria-expanded", String(!panel.hidden));
  if (!panel.hidden) qs("select", panel)?.focus();
});
qsa("#document-search, #document-filter, #document-category-filter, #document-owner-filter, #document-action-filter").forEach((control) => {
  control.addEventListener(control.matches("select, [type='checkbox']") ? "change" : "input", applyDocumentFilters);
});
qs("#document-filter-reset")?.addEventListener("click", () => {
  qs("#document-search").value = "";
  qs("#document-filter").value = "all";
  qs("#document-category-filter").value = "all";
  qs("#document-owner-filter").value = "all";
  qs("#document-action-filter").checked = false;
  applyDocumentFilters();
});
qs("#view-documents [aria-label='列表视图']")?.addEventListener("click", () => showToast("列表视图", "当前已使用适合批量复核的紧凑列表视图"));
qs("#view-documents [aria-label='刷新文档列表']")?.addEventListener("click", async (event) => {
  event.currentTarget.disabled = true;
  await Promise.all([loadRecentRealJobs(), loadPublishedAssets()]);
  event.currentTarget.disabled = false;
  showToast("文档列表已刷新", "任务、迁移记录和资产来源已经同步");
});
qsa("#view-documents .data-table .icon-btn").forEach((button) => button.addEventListener("click", () => showToast("文档操作", "真实服务可从“接入文档”启动分析；历史样本保持只读")));
qs("#analysis-run-log")?.addEventListener("click", (event) => {
  if (!currentRealJob && state.analysis.mode !== "real") renderAnalysisRunLog({ id: state.analysis.id, state: state.analysis.status, updatedAt: state.analysis.updatedAt, document: { name: state.analysis.document } });
  const panel = qs("#analysis-run-log-panel");
  panel.hidden = !panel.hidden;
  event.currentTarget.setAttribute("aria-expanded", String(!panel.hidden));
});

findButton(qs("#view-redaction .page-intro"), "导出演示预览")?.addEventListener("click", () => {
  const content = qs(".redacted-paper").innerText;
  const blob = new Blob(["intelifar 脱敏预览副本\n\n", content], { type: "text/plain;charset=utf-8" });
  const link = document.createElement("a");
  link.href = URL.createObjectURL(blob);
  link.download = `intelifar-redacted-preview-${new Date().toISOString().slice(0, 10)}.txt`;
  link.click();
  URL.revokeObjectURL(link.href);
  const audit = makeAuditEvent("导出脱敏预览", "导出已移除敏感参数的文本副本");
  state.auditEvents = appendAudit(state.auditEvents, audit);
  appendAuditToDom(audit);
  saveState();
  showToast("脱敏副本已导出", "文本副本不包含被涂黑的原始参数");
});
qsa("#view-redaction .page-nav button").forEach((button) => { button.disabled = true; button.title = "当前验收样本仅包含第 114 页"; });
findButton(qs("#view-lifecycle"), "编辑策略")?.addEventListener("click", () => showToast("策略只读", "生产部署需由企业 OIDC / RBAC 管理员在策略中心修改"));
qsa("#view-lifecycle .share-list .icon-btn").forEach((button) => button.addEventListener("click", () => showToast("分享治理", "可在生产策略中心执行续期、撤销或查看访问审计")));

const auditFilter = qs("#audit-category-filter");
const auditStatus = document.createElement("span");
auditStatus.id = "audit-filter-status";
auditStatus.className = "muted-copy";
qs("#view-audit .table-toolbar").append(auditStatus);
function updateAuditFilter() {
  const query = qs("#audit-search").value.trim().toLocaleLowerCase("zh-CN");
  const category = auditFilter.value;
  let visible = 0;
  qsa("#audit-log [data-audit-entry]").forEach((entry) => {
    const text = entry.textContent.toLocaleLowerCase("zh-CN");
    const match = (!query || text.includes(query)) && (category === "all" || entry.dataset.auditCategory === category);
    entry.hidden = !match;
    if (match) visible += 1;
  });
  auditStatus.textContent = `${visible} 条匹配记录`;
}
qs("#audit-search").addEventListener("input", updateAuditFilter);
auditFilter.addEventListener("change", updateAuditFilter);
updateAuditFilter();

qsa("[data-action-filter]").forEach((button) => button.addEventListener("click", () => {
  activeActionFilter = button.dataset.actionFilter;
  qsa("[data-action-filter]").forEach((item) => { item.classList.toggle("is-active", item === button); item.setAttribute("aria-pressed", String(item === button)); });
  applyWorkspaceActionFilter();
}));
qs("#workspace-action-list").addEventListener("click", async (event) => {
  const button = event.target.closest(".wiki-review-decision");
  if (!button) return;
  const decision = button.dataset.decision;
  button.disabled = true;
  button.setAttribute("aria-busy", "true");
  try {
    const response = await fetch(`/api/wiki/reviews/${encodeURIComponent(button.dataset.reviewId)}/decision`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ decision, reviewNote: decision === "approved" ? "管理员已核验发布条件" : "请补充原文依据或变更说明" }) });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(response.status === 409 ? `审批基于的版本已变化，请打开最新 Wiki 后重新提交` : payload.error || "审批未完成");
    showToast(decision === "approved" ? "Wiki 更新已批准发布" : "Wiki 更新已退回", decision === "approved" ? `${payload.wiki?.version || "新版本"} 已成为当前版本` : "已保留审批记录，已发布内容没有变化");
    await Promise.all([loadWikiReviews(), loadPublishedAssets(), loadAuditLedger(), loadDashboard()]);
  } catch (error) {
    showToast("审批未完成", String(error.message || "请稍后重试"), "error");
    button.disabled = false;
  } finally {
    button.removeAttribute("aria-busy");
  }
});
qs("#refresh-workspace-actions").addEventListener("click", async (event) => {
  event.currentTarget.disabled = true;
  await Promise.all([loadPublishedAssets(), loadAssetGraph(), loadRecentRealJobs(), loadWikiReviews(), loadMembers()]);
  event.currentTarget.disabled = false;
  showToast("待办已刷新", `${currentWorkspaceActions.length} 项当前岗位待办`);
});
qs(".notification")?.addEventListener("click", () => navigate("tasks"));
qs("#logout-button")?.addEventListener("click", async () => {
  if (["loopback-demo", "loopback-persistent", "static-demo"].includes(currentSession?.mode)) { showToast("当前为本机工作空间", "启用 SMB 账号模式后可使用真实登录与退出会话"); return; }
  const response = await fetch("/api/auth/logout", { method: "POST" }).catch(() => null);
  if (response && response.status !== 204) { showToast("暂时无法退出", "请稍后重试", "error"); return; }
  currentPublishedAssets = [];
  currentWikiReviews = [];
  currentWorkspaceActions = [];
  selectedPublishedAsset = null;
  qsa("#asset-table-body .published-row").forEach((row) => row.remove());
  lockWorkspace();
});

qsa("[data-open-redaction-source]").forEach((button) => {
  button.addEventListener("click", async (event) => {
    const evidenceId = event.currentTarget.dataset.redactionEvidenceId || REDACTION_DEMO_EVIDENCE.id;
    if (evidenceId !== REDACTION_DEMO_EVIDENCE.id) {
      activeEvidence = null;
      closeDrawers(false);
      showToast("无法打开证据", "脱敏片段与证据上下文不匹配。", "error");
      return;
    }
    renderEvidence(REDACTION_DEMO_EVIDENCE, event.currentTarget);
    try {
      await recordUiAuditEvent("redaction_evidence_view", evidenceId);
      showToast("权限校验通过", "已记录本次演示样本的 S1 溯源访问；未返回涂黑原值");
    } catch (error) {
      showToast("证据已隔离展示", `${String(error.message || "审计写入失败")}；请在系统状态中检查审计链。`, "error");
    }
  });
});

function openShareDialog() {
  clearErrors(qs("#share-form"));
  const asset = selectedPublishedAsset || currentPublishedAssets[0] || null;
  setText("#share-asset-title", asset?.wiki?.title || asset?.title || (currentSession?.mode === "static-demo" ? "离线演示 Wiki" : "尚无可分享的真实 Wiki"));
  setText("#share-asset-id", asset?.id || "离线演示，不生成服务端凭证");
  qs("#share-secret-result").hidden = true;
  qs("#share-result-link").value = "";
  qs("#share-result-code").value = "";
  qs("[data-testid='create-share']").disabled = false;
  showDialog(qs("#share-dialog"));
}
qs("#open-share").addEventListener("click", openShareDialog);
qs("#share-wiki").addEventListener("click", openShareDialog);

function renderLocalDemoShare(result) {
  const item = document.createElement("article");
  const avatar = document.createElement("div");
  const copy = document.createElement("span");
  const recipient = document.createElement("strong");
  const expiry = document.createElement("small");
  const actions = document.createElement("button");
  avatar.className = "share-avatar purple";
  avatar.textContent = "演";
  recipient.textContent = result.value.recipient;
  expiry.textContent = `离线演示 · 对外只读 Wiki · ${result.value.expires}`;
  actions.className = "icon-btn small";
  actions.textContent = "•••";
  copy.append(recipient, expiry);
  item.append(avatar, copy, actions);
  const list = qs("#demo-share-list");
  qs(".ledger-empty", list)?.remove();
  list.prepend(item);
}

function secureShareItem(share) {
  const item = document.createElement("article");
  item.dataset.shareId = share.id;
  const avatar = document.createElement("div");
  const copy = document.createElement("span");
  const recipient = document.createElement("strong");
  const expiry = document.createElement("small");
  const action = document.createElement("button");
  avatar.className = "share-avatar purple";
  avatar.textContent = share.status === "active" ? "安" : "止";
  recipient.textContent = share.recipientEmail;
  expiry.textContent = `${share.status === "active" ? "有效" : share.status === "revoked" ? "已撤销" : "已过期"} · 访问 ${share.accessCount} 次 · ${formatDate(share.expiresAt)} 到期`;
  action.type = "button";
  action.className = "secondary-btn compact revoke-share";
  action.textContent = share.status === "active" ? "撤销" : "已结束";
  action.disabled = share.status !== "active";
  action.dataset.shareId = share.id;
  copy.append(recipient, expiry);
  item.append(avatar, copy, action);
  return item;
}

const lifecycleRoleCapabilities = Object.freeze({
  owner: ["查看 Wiki 与原文依据", "复核并更新 Wiki", "确认资产关系", "创建与撤销安全分享", "管理空间成员", "查看全空间审计与运维状态"],
  admin: ["查看 Wiki 与原文依据", "复核并更新 Wiki", "确认资产关系", "创建与撤销安全分享", "管理空间成员", "查看全空间审计与运维状态"],
  editor: ["查看 Wiki 与原文依据", "复核并更新 Wiki", "提出或确认资产关系", "创建与撤销安全分享"],
  viewer: ["查看已授权 Wiki", "查看权限范围内的原文依据", "执行只读 IP 分析任务"],
});

function renderLifecycleGovernance() {
  const root = qs("#lifecycle-real-governance");
  if (!root || root.hidden) return;
  const role = String(currentSession?.user?.role || "viewer");
  const capabilities = lifecycleRoleCapabilities[role] || lifecycleRoleCapabilities.viewer;
  const assetReviewCount = currentPublishedAssets.filter((asset) => [asset.owner, asset.sensitivity, asset.status].some((value) => /待确权|待复核|needs_review/i.test(String(value || "")))).length;
  const relationshipReviewCount = Number(currentDashboard?.graph?.proposed ?? currentAssetGraph.meta?.proposed ?? 0);
  const reviewCount = assetReviewCount + relationshipReviewCount;
  const activeShareCount = currentShares.filter((share) => share.status === "active").length;
  setText("#lifecycle-asset-count", String(currentDashboard?.assets?.total ?? currentPublishedAssets.length));
  setText("#lifecycle-review-count", String(reviewCount));
  setText("#lifecycle-review-detail", `资产 ${assetReviewCount} · 关系 ${relationshipReviewCount}`);
  setText("#lifecycle-share-count", ["owner", "admin", "editor"].includes(role) ? String(activeShareCount) : "受权限保护");
  setText("#lifecycle-share-pill", ["owner", "admin", "editor"].includes(role) ? `${activeShareCount} 个` : "仅管理员可见");
  setText("#lifecycle-role-label", roleLabels[role] || role);
  setText("#lifecycle-role-capability", `${capabilities.length} 类授权操作`);
  const capabilityList = qs("#lifecycle-capabilities");
  capabilityList.replaceChildren();
  for (const capability of capabilities) {
    const item = document.createElement("div");
    const marker = document.createElement("span");
    const copy = document.createElement("p");
    marker.textContent = "✓";
    copy.textContent = capability;
    item.append(marker, copy);
    capabilityList.append(item);
  }
  const governanceEvents = currentAuditEvents.filter((event) => /^(?:publication|wiki|relationship|share|member)\./i.test(String(event.action))).slice(0, 6);
  const eventList = qs("#lifecycle-event-list");
  eventList.replaceChildren();
  for (const event of governanceEvents) {
    const item = document.createElement("article");
    const marker = document.createElement("i");
    const copy = document.createElement("span");
    const title = document.createElement("strong");
    const detail = document.createElement("small");
    const time = document.createElement("time");
    marker.textContent = "✓";
    title.textContent = auditActionLabels[event.action] || "治理操作";
    detail.textContent = auditDetailText(event);
    time.textContent = formatDate(event.createdAt);
    copy.append(title, detail);
    item.append(marker, copy, time);
    eventList.append(item);
  }
  if (!governanceEvents.length) {
    const empty = document.createElement("div");
    empty.className = "ledger-empty";
    empty.textContent = "当前工作空间尚无治理事件";
    eventList.append(empty);
  }
}

async function loadShares() {
  if (!["owner", "admin", "editor"].includes(currentSession?.user?.role) || ["static-demo", "loopback-demo"].includes(currentSession?.mode)) { renderLifecycleGovernance(); return; }
  try {
    const response = await fetch("/api/shares", { headers: { accept: "application/json" } });
    if (!response.ok) return;
    const { shares = [] } = await response.json();
    currentShares = shares;
    const list = qs("#share-list");
    list.replaceChildren(...(shares.length ? shares.map(secureShareItem) : [Object.assign(document.createElement("div"), { className: "ledger-empty", textContent: "暂无服务端安全分享" })]));
    renderLifecycleGovernance();
  } catch {
    // Keep the lifecycle surface available when the gateway is temporarily offline.
  }
}

qs("#share-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  clearErrors(form);
  const result = validateShare(Object.fromEntries(new FormData(form)));
  if (!result.valid) {
    applyErrors(form, result.errors);
    form.querySelector("[aria-invalid='true']")?.focus();
    showToast("无法创建分享", "请检查接收方和有效期", "error");
    return;
  }
  if (["static-demo", "loopback-demo"].includes(currentSession?.mode)) {
    renderLocalDemoShare(result);
    form.reset();
    qs("#share-dialog").close();
    showToast("离线分享演示已创建", `${result.value.recipient} · 未生成真实访问凭证`);
    return;
  }
  const asset = selectedPublishedAsset || currentPublishedAssets[0];
  if (!asset?.id?.startsWith("IP-REAL-")) { showToast("没有可分享的真实 Wiki", "请先完成真实分析并发布资产", "error"); return; }
  const button = qs("[data-testid='create-share']", form);
  button.disabled = true;
  button.setAttribute("aria-busy", "true");
  try {
    const response = await fetch("/api/shares", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ assetId: asset.id, recipient: result.value.recipient, expires: result.value.expires }) });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok || !payload.share) throw new Error(payload.error || "安全分享创建失败");
    qs("#share-result-link").value = `${location.origin}${payload.sharePath}`;
    qs("#share-result-code").value = payload.accessCode;
    qs("#share-secret-result").hidden = false;
    button.textContent = "✓ 凭证已生成";
    await loadShares();
    showToast("双凭证安全分享已创建", "请将链接与访问码通过不同渠道发送");
  } catch (error) {
    button.disabled = false;
    showToast("无法创建安全分享", String(error.message || "请稍后重试"), "error");
  } finally {
    button.removeAttribute("aria-busy");
  }
});

qs("#share-list").addEventListener("click", async (event) => {
  const button = event.target.closest(".revoke-share");
  if (!button) return;
  button.disabled = true;
  const response = await fetch(`/api/shares/${encodeURIComponent(button.dataset.shareId)}`, { method: "DELETE", headers: { accept: "application/json" } }).catch(() => null);
  if (!response?.ok) { button.disabled = false; showToast("撤销失败", "请刷新后重试", "error"); return; }
  await loadShares();
  showToast("已阻止后续访问", "链接与访问码不能再次解锁；对方已加载或复制的内容无法召回");
});

qs("#export-audit").addEventListener("click", async () => {
  if (!canReadWorkspaceAudit()) {
    showToast("操作记录导出仅限管理员", "当前岗位不会获得其他成员或运维记录。", "error");
    return;
  }
  const header = "event_id,action,actor,timestamp,detail\n";
  const sourceEvents = ["static-demo", "loopback-demo"].includes(currentSession?.mode) ? state.auditEvents : currentAuditEvents;
  const rows = sourceEvents.map((event) => [
    event.id,
    auditActionLabels[event.action] || event.action,
    event.actor?.name || event.actor || "系统",
    event.createdAt || event.timestamp,
    auditDetailText(event),
  ].map(csvCell).join(","));
  const blob = new Blob(["\uFEFF", header, rows.join("\n")], { type: "text/csv;charset=utf-8" });
  const link = document.createElement("a");
  link.href = URL.createObjectURL(blob);
  link.download = `intelifar-audit-${new Date().toISOString().slice(0, 10)}.csv`;
  link.click();
  URL.revokeObjectURL(link.href);
  if (["static-demo", "loopback-demo"].includes(currentSession?.mode)) {
    const audit = makeAuditEvent("导出操作记录", "导出当前筛选范围的 CSV 操作记录");
    state.auditEvents = appendAudit(state.auditEvents, audit);
    appendAuditToDom(audit);
    saveState();
  } else {
    try { await recordUiAuditEvent("audit_export", currentSession?.workspace?.id); }
    catch (error) { showToast("文件已导出，审计写入失败", String(error.message || error), "error"); return; }
  }
  showToast("操作记录已导出", "CSV 仅包含当前工作空间记录，并已防护表格公式注入");
});

function showGlobalSearchPanel() {
  qs("#global-search-results").hidden = false;
  qs("#global-search").setAttribute("aria-expanded", "true");
}

function hideGlobalSearchPanel() {
  qs("#global-search-results").hidden = true;
  qs("#global-search").setAttribute("aria-expanded", "false");
  qs(".global-search-shell").classList.remove("is-mobile-open");
  globalSearchActiveIndex = -1;
}

function setGlobalSearchSelection(nextIndex) {
  const rows = qsa("#global-search-results [data-search-asset-id]");
  if (!rows.length) { globalSearchActiveIndex = -1; return; }
  globalSearchActiveIndex = Math.max(0, Math.min(rows.length - 1, nextIndex));
  rows.forEach((row, index) => row.classList.toggle("is-active", index === globalSearchActiveIndex));
  rows[globalSearchActiveIndex].scrollIntoView({ block: "nearest" });
}

function renderGlobalSearchResults(records, query) {
  const panel = qs("#global-search-results");
  const results = presentWorkspaceSearchResults(records, query);
  panel.replaceChildren();
  globalSearchActiveIndex = -1;
  if (!results.length) {
    panel.append(Object.assign(document.createElement("div"), { className: "global-search-state", textContent: `没有找到与“${query}”匹配的可见知识` }));
    showGlobalSearchPanel();
    return;
  }
  for (const result of results) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "global-search-result";
    button.dataset.searchAssetId = result.assetId;
    button.setAttribute("role", "option");
    const icon = document.createElement("i"); icon.textContent = "W";
    const copy = document.createElement("span");
    const title = document.createElement("strong"); title.textContent = result.title;
    const snippet = document.createElement("span"); snippet.textContent = `${result.matchLabel}：${result.snippet}`;
    const meta = document.createElement("small"); meta.textContent = `${result.type} · 来源：${result.sourceTitle}${result.recordCount > 1 ? ` · ${result.recordCount} 条来源记录` : ""}`;
    const arrow = document.createElement("b"); arrow.textContent = "→";
    copy.append(title, snippet, meta); button.append(icon, copy, arrow); panel.append(button);
  }
  showGlobalSearchPanel();
}

async function runGlobalSearch() {
  const input = qs("#global-search");
  const query = input.value.trim();
  const panel = qs("#global-search-results");
  const requestId = ++globalSearchRequest;
  if (query.length < 2) {
    panel.replaceChildren(Object.assign(document.createElement("div"), { className: "global-search-state", textContent: "输入至少 2 个字，查找当前账号可见知识" }));
    showGlobalSearchPanel();
    return;
  }
  panel.replaceChildren(Object.assign(document.createElement("div"), { className: "global-search-state", textContent: "正在搜索 Wiki、资产与原文依据…" }));
  showGlobalSearchPanel();
  try {
    const response = await fetch(`/api/search?q=${encodeURIComponent(query)}`, { headers: { accept: "application/json" } });
    if (requestId !== globalSearchRequest) return;
    if (response.status === 401) { hideGlobalSearchPanel(); lockWorkspace(); return; }
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(payload.error || "搜索暂时不可用");
    renderGlobalSearchResults(Array.isArray(payload.results) ? payload.results : [], query);
  } catch (error) {
    if (requestId !== globalSearchRequest) return;
    panel.replaceChildren(Object.assign(document.createElement("div"), { className: "global-search-state", textContent: String(error.message || "搜索暂时不可用") }));
    showGlobalSearchPanel();
  }
}

async function openGlobalSearchAsset(assetId) {
  let asset = currentPublishedAssets.find((item) => item.id === assetId) || currentAssetPresentation.assets.find((item) => item.id === assetId);
  if (!asset) {
    const response = await fetch(`/api/assets/${encodeURIComponent(assetId)}`, { headers: { accept: "application/json" } });
    if (response.ok) asset = (await response.json()).asset;
  }
  if (!asset) { showToast("无法打开搜索结果", "该知识可能已被移除或当前账号权限已变化", "error"); return; }
  renderDynamicWiki(asset);
  navigate("wiki");
  hideGlobalSearchPanel();
  qs("#global-search").value = "";
  qs("#wiki-dynamic-title")?.focus({ preventScroll: true });
}

qs("#global-search").addEventListener("input", () => {
  clearTimeout(globalSearchTimer);
  globalSearchTimer = setTimeout(runGlobalSearch, 180);
});
qs("#mobile-global-search").addEventListener("click", () => {
  qs(".global-search-shell").classList.add("is-mobile-open");
  qs("#global-search").focus();
});
qs("#global-search").addEventListener("focus", runGlobalSearch);
qs("#global-search").addEventListener("keydown", (event) => {
  const rows = qsa("#global-search-results [data-search-asset-id]");
  if (event.key === "ArrowDown") { event.preventDefault(); setGlobalSearchSelection(globalSearchActiveIndex + 1); }
  else if (event.key === "ArrowUp") { event.preventDefault(); setGlobalSearchSelection(globalSearchActiveIndex <= 0 ? rows.length - 1 : globalSearchActiveIndex - 1); }
  else if (event.key === "Enter" && rows.length) { event.preventDefault(); openGlobalSearchAsset(rows[Math.max(0, globalSearchActiveIndex)]?.dataset.searchAssetId); }
  else if (event.key === "Escape") { event.preventDefault(); hideGlobalSearchPanel(); }
});
qs("#global-search-results").addEventListener("click", (event) => {
  const result = event.target.closest("[data-search-asset-id]");
  if (result) openGlobalSearchAsset(result.dataset.searchAssetId);
});
document.addEventListener("pointerdown", (event) => {
  if (!event.target.closest(".global-search-shell")) hideGlobalSearchPanel();
});

document.addEventListener("keydown", (event) => {
  trapDrawerFocus(event);
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    qs("#global-search").focus();
  }
  if (event.key === "Escape") closeDrawers();
});

qsa("dialog").forEach((dialog) => {
  dialog.addEventListener("click", (event) => {
    if (dialog.id === "session-dialog") return;
    const rect = dialog.getBoundingClientRect();
    const outside = event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom;
    if (outside) dialog.close();
  });
  dialog.addEventListener("close", () => {
    if (dialog.id === "share-dialog") {
      qs("#share-form").reset();
      qs("#share-secret-result").hidden = true;
      qs("#share-result-link").value = "";
      qs("#share-result-code").value = "";
      const button = qs("[data-testid='create-share']");
      button.disabled = false;
      button.removeAttribute("aria-busy");
      button.textContent = "创建分享";
    }
    if (dialog.id === "invitation-dialog") {
      qs("#invitation-form").reset();
      qs("#invitation-secret-result").hidden = true;
      qs("#invitation-result-link").value = "";
      setText("#invitation-error", "");
      const button = qs("[data-testid='create-invitation']");
      button.disabled = false;
      button.removeAttribute("aria-busy");
      button.textContent = "生成一次性邀请";
    }
  });
});

async function refreshProviderHealth() {
  const status = qs("#system-live-status");
  try {
    const response = await fetch("/api/health", { headers: { accept: "application/json" } });
    if (!response.ok) throw new Error("offline");
    const health = await response.json();
    setText("#health-gateway", "在线");
    setText("#health-mineru", health.providers?.mineru === "configured" ? "已配置" : "未配置");
    setText("#health-deepseek", health.providers?.deepseek === "configured" ? "已配置" : "未配置");
  setText("#health-registry", health.storage?.adapter === "sqlite" ? "数据持久保存 · Wiki 版本已启用" : "基础文件存储模式");
    setText("#provider-health-chip", "文档分析服务可用");
    status.innerHTML = "<i></i> 各项业务能力正常";
  } catch {
    setText("#health-gateway", "离线演示");
    setText("#health-mineru", "需启动真实网关");
    setText("#health-deepseek", "需启动真实网关");
    setText("#health-registry", "静态模式未挂载");
    setText("#provider-health-chip", "本地演示就绪");
    status.innerHTML = "<i></i> 当前为离线演示模式";
  }
}

const operationStateLabels = {
  complete: "已完成",
  failed: "失败",
  interrupted: "等待恢复",
  blocked: "安全拦截",
  queued: "已入队",
  "security-scan": "安全检查",
  "mineru-upload": "文档读取服务接收",
  "mineru-running": "读取文档内容",
  deepseek: "提取知识资产",
};

function formatBytes(value) {
  const bytes = Number(value || 0);
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function formatDate(value) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "—" : date.toLocaleString("zh-CN", { hour12: false });
}

function renderDashboard(dashboard) {
  currentDashboard = dashboard;
  const now = new Date();
  setText("#overview-date", `${now.toLocaleDateString("zh-CN", { year: "numeric", month: "long", day: "numeric" })} · 企业知识态势`);
  setText("#overview-greeting", `你好，${currentSession?.user?.name || "当前用户"}`);
  setText("#overview-summary", `当前空间已沉淀 ${dashboard.assets.total} 项 IP 资产，${dashboard.documents.processing} 份文档正在处理，${dashboard.risks.high} 项高敏风险待复核。`);
  setText("#metric-assets-total", String(dashboard.assets.total));
  setText("#metric-assets-detail", `本月新增 ${dashboard.assets.addedThisMonth} 项`);
  applyAssetPresentationMetrics();
  setText("#metric-evidence-coverage", `${dashboard.evidence.coverage}%`);
  setText("#metric-evidence-detail", `${dashboard.evidence.verified} / ${dashboard.evidence.total} 处原文依据已核验`);
  qs("#metric-evidence-meter").style.width = `${dashboard.evidence.coverage}%`;
  setText("#metric-documents-total", `正在处理 ${dashboard.documents.processing}`);
  setText("#metric-documents-processing", String(dashboard.documents.total));
  setText("#metric-documents-detail", `已完成 ${dashboard.documents.complete} · 异常 ${dashboard.documents.failed}`);
  setText("#metric-risk-high", `待复核 ${dashboard.risks.high} 项`);
  setText("#metric-risk-total", String(dashboard.risks.total));
  setText("#nav-document-count", String(dashboard.documents.total));
  setText("#nav-redaction-count", String(dashboard.risks.total));
  setText("#audit-today-count", String(dashboard.audit.today));
  setText("#audit-total-count", String(dashboard.audit.total));
  setText("#audit-blocked-count", String(dashboard.audit.blocked));
  setText("#audit-integrity", dashboard.audit.integrity ? "100%" : "失败");
  setText("#audit-integrity-detail", dashboard.audit.integrity ? "当前工作空间记录完整" : "操作记录需要管理员检查");
  qsa("[data-demo-dashboard]").forEach((surface) => { surface.hidden = !["static-demo", "loopback-demo"].includes(currentSession?.mode); });
  renderLifecycleGovernance();
}

async function loadDashboard() {
  if (["static-demo", "loopback-demo"].includes(currentSession?.mode)) return;
  try {
    const response = await fetch("/api/dashboard", { headers: { accept: "application/json" } });
    if (response.status === 401) { lockWorkspace(); return; }
    const payload = await response.json().catch(() => ({}));
    if (!response.ok || !payload.dashboard) throw new Error(payload.error || "工作空间统计暂时不可用");
    renderDashboard(payload.dashboard);
  } catch (error) {
    setText("#overview-summary", String(error.message || "工作空间统计暂时不可用"));
  }
}

function renderAuditMetrics(events, integrity) {
  const todayKey = new Date().toLocaleDateString("zh-CN");
  const today = events.filter((event) => {
    const date = new Date(event.createdAt);
    return !Number.isNaN(date.valueOf()) && date.toLocaleDateString("zh-CN") === todayKey;
  }).length;
  const blocked = events.filter((event) => /block|reject|deny|拦截|拒绝/i.test(String(event.action || ""))).length;
  const total = Number(integrity?.count ?? events.length);
  setText("#audit-today-count", String(today));
  setText("#audit-total-count", String(total));
  setText("#audit-blocked-count", String(blocked));
  setText("#audit-integrity", integrity?.valid ? "100%" : "失败");
  setText("#audit-integrity-detail", integrity?.valid ? `${total} 条操作记录完整且顺序一致` : "操作记录需要管理员检查");
}

async function loadAuditLedger() {
  if (["static-demo", "loopback-demo"].includes(currentSession?.mode)) return;
  if (!canReadWorkspaceAudit()) {
    currentAuditEvents = [];
    return;
  }
  try {
    const response = await fetch("/api/audit?limit=200", { headers: { accept: "application/json" } });
    if (response.status === 401) { lockWorkspace(); return; }
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(payload.error || "操作记录暂时不可用");
    currentAuditEvents = Array.isArray(payload.events) ? payload.events : [];
    const log = qs("#audit-log");
    log.replaceChildren();
    for (const event of currentAuditEvents.slice().reverse()) appendAuditToDom(event);
    if (!currentAuditEvents.length) {
      const empty = document.createElement("div"); empty.className = "ledger-empty"; empty.textContent = "当前工作空间尚无操作记录"; log.append(empty);
    }
    const integrity = qs("#view-audit .integrity-chip");
    integrity.textContent = payload.integrity?.valid ? `◇ 记录完整性已校验 · ${payload.integrity.count} 条` : "! 记录完整性校验失败";
    integrity.classList.toggle("danger", !payload.integrity?.valid);
    renderAuditMetrics(currentAuditEvents, payload.integrity);
    renderLifecycleGovernance();
    updateAuditFilter();
  } catch (error) {
    const log = qs("#audit-log"); log.replaceChildren();
    const failure = document.createElement("div"); failure.className = "ledger-empty"; failure.textContent = String(error.message || "操作记录暂时不可用"); log.append(failure);
  }
}

async function recordUiAuditEvent(eventType, objectId) {
  if (["static-demo", "loopback-demo"].includes(currentSession?.mode)) return null;
  const response = await fetch("/api/audit/events", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ eventType, objectId }),
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok || !payload.event) throw new Error(payload.error || "审计事件写入失败");
  await Promise.all([loadAuditLedger(), loadDashboard(), loadOperations()]);
  return payload.event;
}

function csvCell(value) {
  const normalized = String(value ?? "");
  const safe = /^[=+\-@\t\r]/u.test(normalized) ? `'${normalized}` : normalized;
  return `"${safe.replaceAll('"', '""')}"`;
}

function setOperationCard(selector, state) {
  const card = qs(selector)?.closest("section");
  if (card) card.dataset.opsState = state;
}

function backupLedgerItem(backup) {
  const article = document.createElement("article");
  const seal = document.createElement("span");
  const copy = document.createElement("div");
  const title = document.createElement("strong");
  const meta = document.createElement("small");
  const hash = document.createElement("code");
  const verify = document.createElement("button");
  seal.className = "backup-seal";
  seal.textContent = backup.integrity === "ok" ? "✓" : "!";
  title.textContent = backup.id;
  meta.textContent = `${formatDate(backup.createdAt)} · ${formatBytes(backup.size)}`;
  hash.textContent = `SHA-256 ${String(backup.sha256 || "").slice(0, 16)}…`;
  verify.type = "button";
  verify.className = "text-link verify-backup";
  verify.textContent = backup.verifiedAt ? "再次校验" : "校验备份";
  verify.dataset.backupId = backup.id;
  copy.append(title, meta, hash);
  article.append(seal, copy, verify);
  return article;
}

function operationJobItem(job) {
  const article = document.createElement("article");
  const stateMarker = document.createElement("span");
  const copy = document.createElement("div");
  const title = document.createElement("strong");
  const meta = document.createElement("small");
  const status = document.createElement("b");
  stateMarker.className = `job-state-marker ${job.state}`;
  stateMarker.textContent = ["complete"].includes(job.state) ? "✓" : ["failed", "blocked", "interrupted"].includes(job.state) ? "!" : "·";
  title.textContent = job.document?.name || job.id;
  meta.textContent = `${job.id} · ${formatDate(job.updatedAt)}`;
  status.textContent = operationStateLabels[job.state] || job.state;
  copy.append(title, meta);
  article.append(stateMarker, copy, status);
  if (job.retryable && ["failed", "interrupted"].includes(job.state)) {
    const retry = document.createElement("button");
    retry.type = "button";
    retry.className = "secondary-btn compact retry-job";
    retry.dataset.jobId = job.id;
    retry.textContent = "安全重试";
    article.append(retry);
  }
  return article;
}

function renderOperations(payload) {
  const scanner = payload.scanner || {};
  const external = scanner.mode === "external-av";
  setText("#ops-scanner-status", external ? "外部 AV 已配置" : "内置确定性预检");
  setText("#ops-scanner-detail", external ? `${scanner.engine} · 与内置预检串联` : scanner.externalRequired ? "外部扫描器缺失 · 当前故障关闭" : "EICAR、宏、伪装 PE、ZIP 风险规则");
  setOperationCard("#ops-scanner-status", external ? "ok" : scanner.externalRequired ? "danger" : "warning");
  setText("#ops-storage-status", payload.storage?.integrity === "ok" ? "运行库完整" : "需要处置");
  setText("#ops-storage-detail", `${payload.storage?.adapter || "未知存储"} · 在线备份${payload.storage?.backupsEnabled ? "已启用" : "未启用"}`);
  setOperationCard("#ops-storage-status", payload.storage?.integrity === "ok" ? "ok" : "danger");
  setText("#ops-audit-status", payload.audit?.valid ? "哈希链有效" : "校验失败");
  setText("#ops-audit-detail", `${Number(payload.audit?.count || 0)} 个工作空间事件`);
  setOperationCard("#ops-audit-status", payload.audit?.valid ? "ok" : "danger");
  const backups = Array.isArray(payload.backups) ? payload.backups : [];
  const latest = backups[0];
  setText("#ops-backup-status", latest ? formatDate(latest.createdAt) : "尚无备份");
  setText("#ops-backup-detail", latest ? `${formatBytes(latest.size)} · ${latest.integrity === "ok" ? "完整性通过" : "需要复核"}` : "创建首份上线前备份");
  setOperationCard("#ops-backup-status", latest?.integrity === "ok" ? "ok" : latest ? "danger" : "warning");
  const backupList = qs("#operations-backup-list");
  backupList.replaceChildren(...(backups.length ? backups.slice(0, 5).map(backupLedgerItem) : [Object.assign(document.createElement("div"), { className: "ledger-empty", textContent: "暂无已验证备份" })]));
  const jobs = Array.isArray(payload.jobs) ? payload.jobs : [];
  const jobList = qs("#operations-job-list");
  jobList.replaceChildren(...(jobs.length ? jobs.slice(0, 8).map(operationJobItem) : [Object.assign(document.createElement("div"), { className: "ledger-empty", textContent: "当前工作空间暂无分析任务" })]));
  setText("#operations-message", `状态更新于 ${new Date().toLocaleTimeString("zh-CN", { hour12: false })} · 当前界面不提供在线数据库恢复`);
  renderSemanticReadiness(payload.semanticEnhancement || { state: "disabled", message: "未启用可选语义增强" });
  currentSemanticReviews = Array.isArray(payload.semanticReviews) ? payload.semanticReviews : [];
  renderSemanticReviews(currentSemanticReviews);
}

function renderSemanticReadiness(status) {
  const panel = qs("#semantic-check-panel");
  const button = qs("#run-semantic-check");
  const state = ["ready", "disabled", "unavailable"].includes(status?.state) ? status.state : "unavailable";
  panel.dataset.semanticState = state;
  setText("#semantic-check-status", state === "ready" ? "可以检查" : state === "disabled" ? "未启用" : "暂不可用");
  setText("#semantic-check-detail", state === "ready" ? `本地资产体检已就绪${status.version ? ` · 版本 ${status.version}` : ""}` : status?.message || "本地资产体检暂不可用");
  button.disabled = state !== "ready";
  button.title = state === "ready" ? "只读检查已发布资产，不会修改正式内容" : state === "disabled" ? "部署负责人启用后才可运行" : "请检查本地语义增强运行环境";
}

function semanticFindingItem(kind, title, detail) {
  const article = document.createElement("article");
  const marker = document.createElement("span");
  const copy = document.createElement("div");
  const heading = document.createElement("strong");
  const explanation = document.createElement("small");
  marker.className = `semantic-finding-marker ${kind}`;
  marker.textContent = kind === "conflict" ? "!" : kind === "duplicate" ? "≈" : "✓";
  heading.textContent = title;
  explanation.textContent = detail;
  copy.append(heading, explanation);
  article.append(marker, copy);
  return article;
}

function semanticReviewPresentation(review) {
  const payload = review?.payload || {};
  const assets = Array.isArray(payload.assets) ? payload.assets : [];
  if (review.kind === "duplicate") {
    const left = assets[0] || { id: payload.assetIds?.[0], title: payload.assetIds?.[0], sourceName: "已发布记录" };
    const right = assets[1] || { id: payload.assetIds?.[1], title: payload.assetIds?.[1], sourceName: "已发布记录" };
    const heading = left.title === right.title ? `疑似重复：${left.title}` : `${left.title} ↔ ${right.title}`;
    const source = (asset) => `${asset.sourceName || "已发布记录"} · ${asset.publishedAt ? formatDate(asset.publishedAt) : "日期未记录"} · ${asset.id}`;
    const reasons = payload.reasons?.length ? payload.reasons.join("、") : "标题与属性相近";
    return { marker: "≈", title: heading, detail: `记录 A：${source(left)} ↔ 记录 B：${source(right)} · ${Math.round(Number(payload.confidence || 0) * 100)}% 建议复核 · ${reasons}` };
  }
  const fieldLabels = { owner: "责任方", sensitivity: "密级", type: "资产类型" };
  return { marker: "!", title: `${payload.title || "同名资产"}的${fieldLabels[payload.field] || payload.field || "信息"}不一致`, detail: `${(payload.values || []).join(" / ")} · 请对照来源后人工确认` };
}

function semanticReviewItem(review) {
  const presentation = semanticReviewPresentation(review);
  const article = document.createElement("article");
  article.dataset.semanticReviewId = review.id;
  const marker = document.createElement("span");
  marker.className = `semantic-finding-marker ${review.kind}`;
  marker.textContent = presentation.marker;
  const copy = document.createElement("div");
  copy.className = "semantic-review-copy";
  const heading = document.createElement("strong");
  const explanation = document.createElement("small");
  heading.textContent = presentation.title;
  explanation.textContent = presentation.detail;
  copy.append(heading, explanation);
  const resolution = document.createElement("div");
  resolution.className = "semantic-review-resolution";
  const status = document.createElement("span");
  status.className = `semantic-review-status ${review.status}`;
  status.textContent = review.status === "confirmed" ? "已确认 · 等待后续治理" : review.status === "dismissed" ? "已保留为独立记录" : "待管理员复核";
  resolution.append(status);
  if (review.status === "pending") {
    const actions = document.createElement("div");
    actions.className = "semantic-review-actions";
    const dismiss = document.createElement("button");
    dismiss.type = "button";
    dismiss.className = "secondary-btn compact semantic-review-decision";
    dismiss.dataset.reviewId = review.id;
    dismiss.dataset.decision = "dismissed";
    dismiss.textContent = "保留独立记录";
    const confirm = document.createElement("button");
    confirm.type = "button";
    confirm.className = "primary-btn compact semantic-review-decision";
    confirm.dataset.reviewId = review.id;
    confirm.dataset.decision = "confirmed";
    confirm.textContent = "确认需治理";
    actions.append(dismiss, confirm);
    resolution.append(actions);
  } else {
    const decided = document.createElement("small");
    const reviewer = review.reviewedBy?.name || "空间管理员";
    decided.textContent = `${reviewer} · ${formatDate(review.reviewedAt)}${review.reviewNote ? ` · ${review.reviewNote}` : ""}`;
    resolution.append(decided);
  }
  article.append(marker, copy, resolution);
  return article;
}

function renderSemanticReviews(reviews = currentSemanticReviews, result = lastSemanticCheckResult) {
  currentSemanticReviews = Array.isArray(reviews) ? reviews : [];
  const pending = currentSemanticReviews.filter((review) => review.status === "pending").length;
  const confirmed = currentSemanticReviews.filter((review) => review.status === "confirmed").length;
  const dismissed = currentSemanticReviews.filter((review) => review.status === "dismissed").length;
  const duplicateCount = result ? (result.duplicates || []).length : currentSemanticReviews.filter((review) => review.kind === "duplicate").length;
  const conflictCount = result ? (result.conflicts || []).length : currentSemanticReviews.filter((review) => review.kind === "conflict").length;
  setText("#semantic-checked-count", result ? String(result.checkedAssets || 0) : "—");
  setText("#semantic-duplicate-count", String(duplicateCount));
  setText("#semantic-conflict-count", String(conflictCount));
  setText("#semantic-provenance-count", result ? String(result.provenance?.evidence || 0) : "—");
  setText("#semantic-review-summary", `待复核 ${pending} 条 · 已确认需治理 ${confirmed} 条 · 已保留独立记录 ${dismissed} 条`);
  let findings = currentSemanticReviews.slice(0, 20).map(semanticReviewItem);
  if (!findings.length && result) {
    const duplicateFindings = (result.duplicates || []).map((candidate) => {
      const records = candidate.assetIds.map((id) => currentPublishedAssets.find((asset) => asset.id === id));
      const labels = records.map((asset, index) => asset?.title || candidate.assetIds[index]);
      const sources = records.map((asset, index) => `${asset?.document?.sourceName || "已发布记录"} · ${formatDate(asset?.publishedAt)} · ${candidate.assetIds[index]}`);
      const reason = candidate.reasons?.length ? candidate.reasons.join("、") : "标题与属性相近";
      const heading = labels[0] === labels[1] ? `疑似重复：${labels[0]}` : `${labels[0]} ↔ ${labels[1]}`;
      return semanticFindingItem("duplicate", heading, `记录 A：${sources[0]} ↔ 记录 B：${sources[1]} · ${Math.round(Number(candidate.confidence || 0) * 100)}% 建议复核 · ${reason}`);
    });
    const fieldLabels = { owner: "责任方", sensitivity: "密级", type: "资产类型" };
    const conflictFindings = (result.conflicts || []).map((conflict) => semanticFindingItem("conflict", `${conflict.title}的${fieldLabels[conflict.field] || conflict.field}不一致`, `${(conflict.values || []).join(" / ")} · 请对照来源后人工确认`));
    findings = [...duplicateFindings, ...conflictFindings];
    if (!findings.length) findings.push(semanticFindingItem("clear", "本次未发现需要立即复核的问题", "这是只读辅助检查结果，正式资产状态没有变化。"));
  }
  if (!findings.length && currentSemanticReviews.length === 0) {
    findings = [Object.assign(document.createElement("div"), { className: "ledger-empty", textContent: "运行检查后显示需要人工复核的建议" })];
  }
  qs("#semantic-check-findings").replaceChildren(...findings);
  qs("#semantic-check-results").hidden = !result && currentSemanticReviews.length === 0;
  renderWorkspaceActions();
}

function renderSemanticResult(result, reviews = []) {
  lastSemanticCheckResult = result;
  renderSemanticReviews(reviews, result);
}

async function runSemanticCheck() {
  const button = qs("#run-semantic-check");
  button.disabled = true;
  button.setAttribute("aria-busy", "true");
  button.textContent = "正在检查…";
  setText("#semantic-check-detail", "正在比较当前可见的已发布资产并核对来源链。");
  try {
    const response = await fetch("/api/admin/semantic/enrich", { method: "POST", headers: { accept: "application/json" } });
    const payload = await response.json().catch(() => ({}));
    if (response.status === 401) { lockWorkspace(); return; }
    if (!response.ok || !payload.result) throw new Error(payload.error || "资产体检暂时无法完成");
    renderSemanticResult(payload.result, payload.reviews);
    setText("#semantic-check-detail", `已完成只读检查 · ${payload.result.checkedAssets || 0} 项资产 · 正式内容未改变`);
    showToast("资产体检完成", `发现 ${payload.result.duplicates?.length || 0} 组疑似重复、${payload.result.conflicts?.length || 0} 项信息冲突`);
    await Promise.all([loadAuditLedger(), loadDashboard()]);
  } catch (error) {
    setText("#semantic-check-detail", String(error.message || "资产体检暂时无法完成"));
    showToast("资产体检未完成", String(error.message || "请检查本地服务后重试"), "error");
  } finally {
    button.disabled = qs("#semantic-check-panel").dataset.semanticState !== "ready";
    button.removeAttribute("aria-busy");
    button.textContent = "再次运行只读检查";
  }
}

function openSemanticReviewDialog(reviewId, decision) {
  const review = currentSemanticReviews.find((item) => item.id === reviewId && item.status === "pending");
  if (!review || !["confirmed", "dismissed"].includes(decision)) {
    showToast("这条建议已经变化", "请刷新系统状态后重新查看", "error");
    return;
  }
  const presentation = semanticReviewPresentation(review);
  pendingSemanticDecision = { reviewId, decision };
  qs("#semantic-review-form").reset();
  setText("#semantic-review-error", "");
  setText("#semantic-review-dialog-marker", presentation.marker);
  setText("#semantic-review-dialog-item", presentation.title);
  setText("#semantic-review-dialog-meta", presentation.detail);
  if (decision === "confirmed") {
    setText("#semantic-review-dialog-title", "确认需要后续治理？");
    setText("#semantic-review-dialog-summary", "保存后，这条建议会从待复核变为“已确认、等待后续治理”。");
    setText("#semantic-review-dialog-impact", "系统只保存复核结论和操作记录，不会自动合并资产或修改 Wiki；实际合并仍需后续人工治理。");
    setText("#confirm-semantic-review", "确认需治理");
  } else {
    setText("#semantic-review-dialog-title", "确认保留为独立记录？");
    setText("#semantic-review-dialog-summary", "保存后，这条建议会归档为“已保留为独立记录”。");
    setText("#semantic-review-dialog-impact", "两项资产继续分别保留，正式内容和 Wiki 均不改变；本次判断会进入操作记录。");
    setText("#confirm-semantic-review", "保留独立记录");
  }
  showDialog(qs("#semantic-review-dialog"));
  queueMicrotask(() => qs("#semantic-review-note")?.focus());
}

async function decideSemanticReview(event) {
  event.preventDefault();
  if (!pendingSemanticDecision) return;
  const form = event.currentTarget;
  const submit = qs("#confirm-semantic-review");
  const reviewNote = String(new FormData(form).get("reviewNote") || "").trim();
  setText("#semantic-review-error", "");
  submit.disabled = true;
  submit.setAttribute("aria-busy", "true");
  try {
    const response = await fetch(`/api/admin/semantic/reviews/${encodeURIComponent(pendingSemanticDecision.reviewId)}/decision`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ decision: pendingSemanticDecision.decision, reviewNote }),
    });
    const payload = await response.json().catch(() => ({}));
    if (response.status === 401) { form.closest("dialog").close(); lockWorkspace(); return; }
    if (!response.ok || !payload.review) {
      if (response.status === 409) throw new Error("该建议已由其他管理员处理，请刷新后查看最新状态");
      throw new Error(payload.error || "复核决定暂时无法保存");
    }
    currentSemanticReviews = currentSemanticReviews.map((review) => review.id === payload.review.id ? payload.review : review);
    renderSemanticReviews(currentSemanticReviews);
    form.closest("dialog").close();
    const label = payload.review.status === "confirmed" ? "已确认需要后续治理" : "已保留为独立记录";
    showToast("复核决定已保存", `${label} · 正式资产和 Wiki 未改变`);
    pendingSemanticDecision = null;
    await Promise.all([loadAuditLedger(), loadDashboard()]);
    await loadOperations();
  } catch (error) {
    setText("#semantic-review-error", String(error.message || "复核决定暂时无法保存"));
  } finally {
    submit.disabled = false;
    submit.removeAttribute("aria-busy");
  }
}

async function loadOperations() {
  const consolePanel = qs("#operations-console");
  const isAdmin = ["owner", "admin"].includes(currentSession?.user?.role);
  consolePanel.hidden = !isAdmin;
  if (!isAdmin) return;
  if (currentSession?.mode === "static-demo") {
    setText("#operations-message", "离线演示未连接管理员接口；启动真实小微网关后显示扫描、备份与任务状态。");
    renderSemanticReadiness({ state: "disabled", message: "离线演示未连接本地资产体检服务" });
    currentSemanticReviews = [];
    renderSemanticReviews([]);
    return;
  }
  try {
    const response = await fetch("/api/admin/operations", { headers: { accept: "application/json" } });
    if (response.status === 401) { lockWorkspace(); return; }
    if (!response.ok) throw new Error(response.status === 403 ? "当前角色没有管理员权限" : "管理员状态接口不可用");
    renderOperations(await response.json());
    await loadMembers();
  } catch (error) {
    setText("#operations-message", String(error.message || "管理员状态读取失败"));
    renderSemanticReadiness({ state: "unavailable", message: "暂时无法读取资产体检状态" });
  }
}

const memberRoleGuidance = Object.freeze({
  owner: { title: "空间所有者", invitation: "负责空间最终控制与管理员配置。", impact: "拥有空间最高管理权限；为避免空间失去控制，所有者不能在此处被改角色或停用。" },
  admin: { title: "空间管理员", invitation: "可以管理成员、备份和全空间审计，也能完成知识编辑工作。", impact: "将可以管理成员、查看全空间审计和运维状态，并可接入文档、更新 Wiki 与创建安全分享。" },
  editor: { title: "知识编辑者", invitation: "可以接入文档、复核分析、更新 Wiki 和创建安全分享。", impact: "将可以接入文档、更新正式 Wiki、确认资产关系和创建安全分享，但不能管理成员或运维配置。" },
  viewer: { title: "只读成员", invitation: "可以查阅已授权 Wiki 与原文依据，不能修改正式内容。", impact: "将只能查看已授权 Wiki 与原文依据，并运行不改变正式内容的只读任务；不能接入文档、更新 Wiki 或创建分享。" },
});

const memberPermissionBoundaries = Object.freeze({
  owner: "空间所有者具有最高管理权限。涉及生产部署、企业统一身份和数据库恢复时仍需由部署负责人协助。",
  admin: "空间所有权转移和生产基础设施变更需要空间所有者或部署负责人处理。",
  editor: "成员管理、全空间审计、备份与系统运维需要空间管理员处理。",
  viewer: "接入文档、修改正式 Wiki、确认关系、创建分享和成员管理需要知识编辑者或管理员处理。",
});

function memberLedgerItem(member) {
  const article = document.createElement("article");
  article.dataset.memberId = member.id;
  article.dataset.memberRoleValue = member.role;
  article.dataset.memberStatusValue = member.status;
  const avatar = document.createElement("span");
  const copy = document.createElement("div");
  const name = document.createElement("strong");
  const meta = document.createElement("small");
  const lastLogin = document.createElement("span");
  const role = document.createElement("select");
  const status = document.createElement("button");
  avatar.className = `member-avatar ${member.status}`;
  avatar.textContent = String(member.name || member.email).slice(0, 1);
  name.textContent = member.name;
  meta.textContent = `${member.email} · ${member.status === "active" ? "账号有效" : "已停用"}`;
  lastLogin.className = "member-last-login";
  const lastLoginLabel = document.createElement("small");
  const lastLoginValue = document.createElement("strong");
  lastLoginLabel.textContent = "最近登录";
  lastLoginValue.textContent = member.lastLoginAt ? formatDate(member.lastLoginAt) : member.id === currentSession?.user?.id ? "当前会话" : "尚未登录";
  lastLogin.append(lastLoginLabel, lastLoginValue);
  for (const [value, label] of Object.entries({ admin: "空间管理员", editor: "知识编辑者", viewer: "只读成员" })) {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = label;
    option.selected = member.role === value;
    role.append(option);
  }
  role.dataset.memberRole = member.id;
  role.dataset.originalRole = member.role;
  role.setAttribute("aria-label", `修改 ${member.name} 的角色`);
  role.disabled = member.role === "owner" || member.id === currentSession?.user?.id;
  if (member.role === "owner") role.replaceChildren(Object.assign(document.createElement("option"), { value: "owner", textContent: "空间所有者" }));
  status.type = "button";
  status.className = "secondary-btn compact member-status-action";
  status.dataset.memberId = member.id;
  status.textContent = member.status === "active" ? "停用账号" : "重新启用";
  status.disabled = member.role === "owner" || member.id === currentSession?.user?.id;
  status.title = member.role === "owner" ? "空间所有者受到保护" : member.id === currentSession?.user?.id ? "不能修改当前登录账号" : member.status === "active" ? "立即撤销该成员的全部登录会话" : "恢复该成员的登录能力";
  copy.append(name, meta);
  article.append(avatar, copy, lastLogin, role, status);
  return article;
}

function invitationLedgerItem(invitation) {
  const article = document.createElement("article");
  const marker = document.createElement("span");
  const copy = document.createElement("div");
  const email = document.createElement("strong");
  const meta = document.createElement("small");
  const action = document.createElement("button");
  marker.className = `invitation-marker ${invitation.status}`;
  marker.textContent = invitation.status === "pending" ? "↗" : "✓";
  email.textContent = invitation.email;
  meta.textContent = `${roleLabels[invitation.role] || invitation.role} · ${invitation.status === "pending" ? `${formatDate(invitation.expiresAt)} 到期` : invitation.status}`;
  action.type = "button";
  action.className = "text-link revoke-invitation";
  action.dataset.invitationId = invitation.id;
  action.textContent = invitation.status === "pending" ? "撤销" : "已结束";
  action.disabled = invitation.status !== "pending";
  copy.append(email, meta);
  article.append(marker, copy, action);
  return article;
}

function pendingInvitations() {
  return currentInvitations.filter((invitation) => invitation.status === "pending");
}

function renderMemberReadiness() {
  const activeMembers = currentMembers.filter((member) => member.status === "active");
  const activeAdmins = activeMembers.filter((member) => ["owner", "admin"].includes(member.role));
  const invitations = pendingInvitations();
  setText("#member-kpi-active", String(activeMembers.length));
  setText("#member-kpi-admins", String(activeAdmins.length));
  setText("#member-kpi-invitations", String(invitations.length));
  setText("#member-kpi-disabled", String(currentMembers.length - activeMembers.length));
  setText("#pending-invitation-count", `${invitations.length} 个`);
  const readiness = qs("#member-readiness");
  const ready = activeAdmins.length >= 2;
  readiness.classList.toggle("is-ready", ready);
  setText("#member-readiness-title", ready ? "管理员配置稳健" : "建议增加第二位管理员");
  setText("#member-readiness-detail", ready ? `当前有 ${activeAdmins.length} 位有效管理员，单个管理员暂时不可用时仍有人可以维护空间。` : "当前只有一位有效管理员。如果该账号暂时不可用，成员邀请、备份和权限调整会无人处理。");
  qs("#member-readiness-action").hidden = ready;
  setText("#system-member-summary-count", `${activeMembers.length} 人`);
  setText("#system-member-summary-detail", ready ? `${activeAdmins.length} 位管理员 · ${invitations.length} 个待处理邀请` : `${activeAdmins.length} 位管理员 · 建议补充第二管理员 · ${invitations.length} 个待处理邀请`);
}

function renderMemberDirectory() {
  const term = String(qs("#member-search").value || "").trim().toLocaleLowerCase("zh-CN");
  const role = qs("#member-role-filter").value;
  const status = qs("#member-status-filter").value;
  const filtered = currentMembers.filter((member) => {
    const matchesTerm = !term || `${member.name} ${member.email}`.toLocaleLowerCase("zh-CN").includes(term);
    return matchesTerm && (role === "all" || member.role === role) && (status === "all" || member.status === status);
  });
  setText("#member-count", filtered.length === currentMembers.length ? `${currentMembers.length} 位成员` : `显示 ${filtered.length} / ${currentMembers.length} 位成员`);
  qs("#member-ledger").replaceChildren(...(filtered.length ? filtered.map(memberLedgerItem) : [Object.assign(document.createElement("div"), { className: "ledger-empty", textContent: "没有符合当前条件的成员" })]));
}

function renderMembers() {
  renderMemberReadiness();
  renderMemberDirectory();
  const invitations = pendingInvitations();
  qs("#invitation-ledger").replaceChildren(...(invitations.length ? invitations.map(invitationLedgerItem) : [Object.assign(document.createElement("div"), { className: "ledger-empty", textContent: "暂无待处理邀请" })]));
}

function renderMemberUnavailable(message) {
  setText("#member-count", message);
  qsa("#member-kpi-active, #member-kpi-admins, #member-kpi-invitations, #member-kpi-disabled").forEach((node) => { node.textContent = "—"; });
  qs("#member-ledger").replaceChildren(Object.assign(document.createElement("div"), { className: "ledger-empty", textContent: message }));
  qs("#invitation-ledger").replaceChildren(Object.assign(document.createElement("div"), { className: "ledger-empty", textContent: "连接真实工作空间后显示邀请" }));
}

async function loadMembers() {
  if (!["owner", "admin"].includes(currentSession?.user?.role)) return;
  if (["static-demo", "loopback-demo"].includes(currentSession?.mode)) {
    renderMemberUnavailable("离线演示未连接成员服务；请进入真实工作空间查看成员。");
    return;
  }
  try {
    const response = await fetch("/api/admin/members", { headers: { accept: "application/json" } });
    if (response.status === 401) { lockWorkspace(); return; }
    if (!response.ok) throw new Error(response.status === 403 ? "当前账号没有成员管理权限" : "成员服务暂时不可用");
    const payload = await response.json();
    currentMembers = payload.members || [];
    currentInvitations = payload.invitations || [];
    renderMembers();
    renderWorkspaceActions();
  } catch (error) {
    renderMemberUnavailable(String(error.message || "成员服务暂时不可用"));
  }
}

async function updateMemberFromLedger(memberId, patch) {
  const member = currentMembers.find((item) => item.id === memberId);
  if (!member) return;
  const response = await fetch(`/api/admin/members/${encodeURIComponent(memberId)}`, { method: "PATCH", headers: { "content-type": "application/json" }, body: JSON.stringify({ role: patch.role || member.role, status: patch.status || member.status }) });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.error || "成员更新失败");
  await loadMembers();
  showToast("成员权限已更新", `${payload.member.name} · ${roleLabels[payload.member.role]} · ${payload.member.status === "active" ? "有效" : "已禁用"}`);
}

function openMemberChange(member, patch, sourceControl = null) {
  pendingMemberChange = { member, patch, sourceControl, committed: false };
  setText("#member-change-name", member.name);
  setText("#member-change-email", member.email);
  setText("#member-change-avatar", String(member.name || member.email).slice(0, 1));
  setText("#member-change-error", "");
  const confirm = qs("#confirm-member-change");
  confirm.className = "primary-btn";
  if (patch.role) {
    const next = memberRoleGuidance[patch.role];
    setText("#member-change-title", `将角色改为“${next.title}”吗？`);
    setText("#member-change-summary", `${memberRoleGuidance[member.role]?.title || roleLabels[member.role]} → ${next.title}，确认后立即生效并记录到审计日志。`);
    qs("#member-change-impact").replaceChildren(Object.assign(document.createElement("strong"), { textContent: "新角色的权限" }), Object.assign(document.createElement("p"), { textContent: next.impact }));
    confirm.textContent = "确认修改角色";
  } else {
    const disabling = patch.status === "disabled";
    setText("#member-change-title", disabling ? `停用 ${member.name} 的账号吗？` : `重新启用 ${member.name} 的账号吗？`);
    setText("#member-change-summary", disabling ? "停用后该成员会立即退出所有设备，但历史内容与署名仍会保留。" : "启用后恢复登录能力；该成员需要重新登录，不会自动恢复旧会话。");
    qs("#member-change-impact").replaceChildren(Object.assign(document.createElement("strong"), { textContent: disabling ? "立即生效的影响" : "恢复后的状态" }), Object.assign(document.createElement("p"), { textContent: disabling ? "所有现有登录会话立即失效，账号不能再访问空间；历史 Wiki、任务和审计记录不会删除。" : "账号可再次登录并按当前角色访问空间；此前主动撤销的分享或邀请不会恢复。" }));
    confirm.textContent = disabling ? "确认停用账号" : "确认重新启用";
    confirm.classList.toggle("danger-action", disabling);
  }
  showDialog(qs("#member-change-dialog"));
}

qs("#member-ledger").addEventListener("change", (event) => {
  const select = event.target.closest("[data-member-role]");
  if (!select) return;
  const member = currentMembers.find((item) => item.id === select.dataset.memberRole);
  if (!member || select.value === member.role) return;
  openMemberChange(member, { role: select.value }, select);
});

qs("#member-ledger").addEventListener("click", (event) => {
  const button = event.target.closest(".member-status-action");
  if (!button) return;
  const member = currentMembers.find((item) => item.id === button.dataset.memberId);
  if (member) openMemberChange(member, { status: member.status === "active" ? "disabled" : "active" }, button);
});

qs("#member-change-dialog").addEventListener("close", () => {
  if (pendingMemberChange?.sourceControl?.matches("select") && !pendingMemberChange.committed) pendingMemberChange.sourceControl.value = pendingMemberChange.member.role;
  pendingMemberChange = null;
});

qs("#member-change-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!pendingMemberChange) return;
  const confirm = qs("#confirm-member-change");
  confirm.disabled = true;
  confirm.setAttribute("aria-busy", "true");
  setText("#member-change-error", "");
  try {
    await updateMemberFromLedger(pendingMemberChange.member.id, pendingMemberChange.patch);
    pendingMemberChange.committed = true;
    qs("#member-change-dialog").close();
  } catch (error) {
    setText("#member-change-error", String(error.message || "成员变更失败"));
  } finally {
    confirm.disabled = false;
    confirm.removeAttribute("aria-busy");
  }
});

for (const selector of ["#member-search", "#member-role-filter", "#member-status-filter"]) {
  qs(selector).addEventListener(selector === "#member-search" ? "input" : "change", renderMemberDirectory);
}

qs("#invitation-ledger").addEventListener("click", async (event) => {
  const button = event.target.closest(".revoke-invitation");
  if (!button) return;
  button.disabled = true;
  const response = await fetch(`/api/admin/invitations/${encodeURIComponent(button.dataset.invitationId)}`, { method: "DELETE", headers: { accept: "application/json" } }).catch(() => null);
  if (!response?.ok) { button.disabled = false; showToast("无法撤销邀请", "请刷新后重试", "error"); return; }
  await loadMembers();
  showToast("邀请已撤销", "一次性激活链接已立即失效");
});

function updateInvitationRoleHelp() {
  const guidance = memberRoleGuidance[qs("#invitation-role").value] || memberRoleGuidance.viewer;
  qs("#invitation-role-help strong").textContent = guidance.title;
  qs("#invitation-role-help p").textContent = guidance.invitation;
}

function openInvitationDialog(role = "viewer") {
  const form = qs("#invitation-form");
  form.reset();
  qs("#invitation-role").value = role;
  updateInvitationRoleHelp();
  setText("#invitation-error", "");
  qs("#invitation-secret-result").hidden = true;
  qs("#invitation-result-link").value = "";
  qs("[data-testid='create-invitation']").disabled = false;
  qs("[data-testid='create-invitation']").textContent = "生成邀请链接";
  showDialog(qs("#invitation-dialog"));
}

qs("#open-invitation").addEventListener("click", () => openInvitationDialog());
qs("#member-readiness-action").addEventListener("click", () => openInvitationDialog("admin"));
qs("#invitation-role").addEventListener("change", updateInvitationRoleHelp);

qs("#invitation-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const input = Object.fromEntries(new FormData(form));
  const button = qs("[data-testid='create-invitation']", form);
  setText("#invitation-error", "");
  button.disabled = true;
  button.setAttribute("aria-busy", "true");
  try {
    const response = await fetch("/api/admin/invitations", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(input) });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok || !payload.token) throw new Error(payload.error || "邀请创建失败");
    qs("#invitation-result-link").value = `${location.origin}${payload.activationPath}`;
    qs("#invitation-secret-result").hidden = false;
    button.textContent = "✓ 邀请链接已生成";
    await loadMembers();
    showToast("一次性邀请已生成", "当前未配置邮件服务，请复制链接后通过可信渠道发送");
  } catch (error) {
    button.disabled = false;
    setText("#invitation-error", String(error.message || "邀请创建失败"));
  } finally {
    button.removeAttribute("aria-busy");
  }
});

qs("#open-my-permissions").addEventListener("click", () => {
  const user = currentSession?.user || {};
  const role = String(user.role || "viewer");
  const capabilities = lifecycleRoleCapabilities[role] || lifecycleRoleCapabilities.viewer;
  setText("#my-permissions-avatar", String(user.name || user.email || "我").slice(0, 1));
  setText("#my-permissions-role", roleLabels[role] || role);
  setText("#my-permissions-account", `${user.name || "当前成员"} · ${user.email || "当前企业账号"}`);
  setText("#my-permissions-boundary", memberPermissionBoundaries[role] || memberPermissionBoundaries.viewer);
  const list = qs("#my-capability-list");
  list.replaceChildren();
  for (const capability of capabilities) {
    const item = document.createElement("div");
    const marker = document.createElement("span");
    const copy = document.createElement("p");
    marker.textContent = "✓";
    copy.textContent = capability;
    item.append(marker, copy);
    list.append(item);
  }
  showDialog(qs("#my-permissions-dialog"));
});

qs("#create-backup").addEventListener("click", async (event) => {
  const button = event.currentTarget;
  button.disabled = true;
  button.setAttribute("aria-busy", "true");
  setText("#operations-message", "正在使用 SQLite 在线备份接口生成并校验快照…");
  try {
    const response = await fetch("/api/admin/backups", { method: "POST", headers: { accept: "application/json" } });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok || !payload.backup) throw new Error(payload.error || "备份创建失败");
    showToast("已生成可验证备份", `${payload.backup.id} · ${formatBytes(payload.backup.size)}`);
    await loadOperations();
  } catch (error) {
    setText("#operations-message", String(error.message || "备份创建失败"));
    showToast("备份未完成", String(error.message || "请检查运行库"), "error");
  } finally {
    button.disabled = false;
    button.removeAttribute("aria-busy");
  }
});

qs("#operations-backup-list").addEventListener("click", async (event) => {
  const button = event.target.closest(".verify-backup");
  if (!button) return;
  button.disabled = true;
  try {
    const response = await fetch(`/api/admin/backups/${encodeURIComponent(button.dataset.backupId)}/verify`, { method: "POST", headers: { accept: "application/json" } });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(payload.error || "备份校验失败");
    showToast("备份校验通过", `${payload.backup.id} · SHA-256 未发生变化`);
    await loadOperations();
  } catch (error) {
    showToast("备份校验失败", String(error.message || "需要管理员处置"), "error");
  } finally {
    button.disabled = false;
  }
});

qs("#operations-job-list").addEventListener("click", async (event) => {
  const button = event.target.closest(".retry-job");
  if (!button) return;
  button.disabled = true;
  try {
    const response = await fetch(`/api/analysis/${encodeURIComponent(button.dataset.jobId)}/retry`, { method: "POST", headers: { accept: "application/json" } });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(payload.error || "任务重试失败");
    showToast("任务已重新入队", `${payload.job.id} · 文件将重新执行安全检查`);
    await loadOperations();
  } catch (error) {
    showToast("无法重试任务", String(error.message || "请检查保留文件"), "error");
    button.disabled = false;
  }
});

qs("#refresh-operations").addEventListener("click", loadOperations);
qs("#run-semantic-check").addEventListener("click", runSemanticCheck);
qs("#semantic-check-findings").addEventListener("click", (event) => {
  const button = event.target.closest(".semantic-review-decision");
  if (button) openSemanticReviewDialog(button.dataset.reviewId, button.dataset.decision);
});
qs("#semantic-review-form").addEventListener("submit", decideSemanticReview);
qs('[data-nav="system"]').addEventListener("click", loadOperations);
qs('[data-nav="members"]').addEventListener("click", loadMembers);
qs('[data-nav="lifecycle"]').addEventListener("click", loadShares);
qs('[data-nav="assets"]').addEventListener("click", loadAssetGraph);
qs('[data-nav="audit"]').addEventListener("click", loadAuditLedger);
qs('[data-nav="tasks"]').addEventListener("click", loadWikiReviews);
qs('[data-nav="agent"]').addEventListener("click", () => agentWorkbench?.loadTasks());

qs("#refresh-health").addEventListener("click", async (event) => {
  event.currentTarget.disabled = true;
  await refreshProviderHealth();
  await loadOperations();
  event.currentTarget.disabled = false;
  showToast("服务状态已刷新", qs("#system-live-status").textContent.trim());
});

document.documentElement.dataset.theme = state.theme;
const hashView = location.hash.slice(1);
navigate(titles[hashView] ? hashView : state.activeView, false);
updateAnalysisUI();
await refreshProviderHealth();
async function initializeAuthenticatedWorkspace() {
  if (!agentWorkbench) {
  agentWorkbench = createAgentWorkbench({
    root: document,
    modeProvider: () => currentSession?.mode || "local-session",
    onSource: openAgentSource,
    onAuthRequired: () => lockWorkspace(),
    onToast: showToast,
    onTaskSettled: () => Promise.all([loadDashboard(), loadAuditLedger(), loadOperations(), loadWikiReviews()]),
  });
  }
  await Promise.all([loadPublishedAssets(), loadAssetGraph(), loadDashboard(), loadAuditLedger(), loadOperations(), loadShares(), loadMembers(), loadWikiReviews(), agentWorkbench.loadTasks()]);
  await loadRecentRealJobs();
}
if (await initializeSession()) await initializeAuthenticatedWorkspace();
