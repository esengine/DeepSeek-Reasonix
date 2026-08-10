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

const storageKey = "intelifar-ip-platform-v1";
const titles = {
  overview: "企业 IP 指挥台",
  documents: "文档中心",
  analysis: "智能分析工作室",
  assets: "IP 资产库",
  wiki: "IP Wiki",
  redaction: "脱敏与溯源",
  lifecycle: "IP 全生命周期",
  audit: "审计日志",
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
let currentRealJob = null;
let currentPublishedAssets = [];
let selectedPublishedAsset = null;
let activeEvidence = null;
let lastDrawerTrigger = null;
let activeAssetType = "all";
let currentSession = null;
let currentMembers = [];
const qs = (selector, root = document) => root.querySelector(selector);
const qsa = (selector, root = document) => [...root.querySelectorAll(selector)];

const roleLabels = { owner: "空间所有者", admin: "空间管理员", editor: "知识编辑者", viewer: "只读成员" };

function applySession(session, mode = "local-session") {
  currentSession = { ...session, mode: session.mode || mode };
  document.body.dataset.sessionState = "ready";
  setText("#workspace-name", session.workspace?.name || "intelifar 工作空间");
  setText("#workspace-avatar", (session.workspace?.name || "I").trim().slice(0, 1));
  setText("#workspace-mode", currentSession.mode === "loopback-demo" || currentSession.mode === "static-demo" ? "受控演示空间" : "小微企业知识空间");
  setText("#profile-name", session.user?.name || session.user?.email || "当前用户");
  setText("#profile-avatar", (session.user?.name || session.user?.email || "U").trim().slice(0, 1));
  setText("#profile-role", roleLabels[session.user?.role] || session.user?.role || "成员");
  const canEdit = ["owner", "admin", "editor"].includes(session.user?.role);
  qs("#wiki-edit").disabled = !canEdit;
  qs("#open-share").disabled = !canEdit;
  qs("#share-wiki").disabled = !canEdit;
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
  qs("#overview-progress-text").textContent = status === "complete" ? "分析已完成" : status === "failed" ? "分析失败" : status === "blocked" ? "文件已安全拦截" : `正在${current?.label ?? "生成 Wiki"}`;
  qs("#analysis-status-label").textContent = status === "complete" ? "已完成" : status === "failed" ? "失败" : status === "blocked" ? "已拦截" : "运行中";
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

function appendAuditToDom(event, level = "secure") {
  const log = qs("#audit-log");
  const article = document.createElement("article");
  const icon = document.createElement("span");
  const copy = document.createElement("div");
  const heading = document.createElement("strong");
  const detail = document.createElement("p");
  const metadata = document.createElement("small");
  const timestamp = document.createElement("time");
  const status = document.createElement("span");
  article.dataset.auditEntry = "";
  icon.className = "audit-icon purple";
  icon.textContent = "↗";
  heading.textContent = event.action;
  detail.textContent = event.detail;
  metadata.textContent = `${event.id} · 本地安全域 · MFA 已验证`;
  timestamp.textContent = event.timestamp;
  status.className = `status-chip ${level}`;
  status.textContent = "成功";
  copy.append(heading, detail, metadata);
  article.append(icon, copy, timestamp, status);
  log.prepend(article);
}

qsa("[data-nav]").forEach((button) => button.addEventListener("click", () => navigate(button.dataset.nav)));
qsa("[data-nav-target]").forEach((button) => button.addEventListener("click", () => navigate(button.dataset.navTarget)));
qsa("[data-open-intake]").forEach((button) => button.addEventListener("click", () => showDialog(qs("#intake-dialog"))));
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
    await Promise.all([loadPublishedAssets(), loadOperations(), loadShares(), loadMembers()]);
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

function renderRealResults(result) {
  const { parser, llm, analysis } = result;
  setText("#real-provider-id", parser.batchId);
  setText("#real-parser-model", parser.model);
  setText("#real-model-name", llm.model);
  setText("#real-response-id", llm.responseId || "响应编号不可用");
  setText("#real-token-usage", `${Number(llm.usage?.totalTokens || 0).toLocaleString("zh-CN")} tokens`);
  setText("#real-markdown-count", `${Number(parser.markdownCharacters || 0).toLocaleString("zh-CN")} 字符`);
  setText("#real-markdown-hash", `SHA-256 ${parser.markdownSha256.slice(0, 12)}…`);
  setText("#real-document-title", analysis.document.title);
  setText("#real-document-summary", analysis.document.summary);
  setText("#real-wiki-mechanism", analysis.wiki.key_mechanism || analysis.wiki.executive_summary);
  setText("#real-asset-count", `${analysis.assets.length} 项`);

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
    marker.textContent = `S-${String(index + 1).padStart(3, "0")}`;
    copy.textContent = source.quote;
    citation.textContent = `${source.section} · ${source.asset}`;
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
  setText("#publication-status", "待人工复核");
}

function evidenceButton(evidence, label = "查看证据") {
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
  verified.textContent = evidence.verified ? "逐字引用与哈希已绑定" : "等待人工校验";
  quote.append(verified);
  setText("#evidence-integrity-title", evidence.verified ? "证据完整性记录已生成" : "证据等待人工校验");
  setText("#evidence-integrity-copy", evidence.verified ? "引用哈希、整文档哈希与 MinerU 任务已绑定；当前定位精度不夸大为页码。" : "此证据尚未形成完整哈希链。");
  openDrawer(qs("#provenance-drawer"), trigger);
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
  renderDynamicWiki(asset);
  openDrawer(qs("#asset-drawer"), trigger);
}

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
  if (qs(`[data-asset-id="${CSS.escape(asset.id)}"]`)) return;
  const row = document.createElement("tr");
  row.className = "clickable-row published-row";
  row.dataset.assetId = asset.id;
  row.dataset.name = `${asset.title} ${asset.id} ${asset.tags.join(" ")}`;
  row.dataset.owner = asset.owner;
  row.dataset.sensitivity = asset.sensitivity;
  row.dataset.status = asset.sensitivity;
  row.dataset.type = asset.type;
  const titleCell = document.createElement("td");
  const titleWrap = document.createElement("span");
  titleWrap.className = "asset-title";
  const icon = document.createElement("i"); icon.textContent = "LIVE";
  const titleCopy = document.createElement("span");
  const title = document.createElement("strong"); title.textContent = asset.title;
  const id = document.createElement("small"); id.textContent = asset.id;
  titleCopy.append(title, id); titleWrap.append(icon, titleCopy); titleCell.append(titleWrap);
  const cells = [asset.type, asset.owner, asset.sensitivity, `${Math.round(Number(asset.confidence || 0) * 100)}%`].map((value) => { const cell = document.createElement("td"); cell.textContent = value; return cell; });
  const evidenceCell = document.createElement("td");
  if (asset.evidence[0]) evidenceCell.append(evidenceButton(asset.evidence[0], `${asset.evidence.length} 处`));
  else evidenceCell.textContent = "待补证";
  const statusCell = document.createElement("td");
  const status = document.createElement("span"); status.className = "status-chip secure"; status.textContent = "真实发布"; statusCell.append(status);
  row.append(titleCell, ...cells, evidenceCell, statusCell);
  bindAssetRow(row, asset);
  qs("#asset-table-body").prepend(row);
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
  setText("#wiki-publication-state", "真实发布");
  setText("#wiki-updated-at", `${asset.wiki?.updatedAt ? "更新于" : "发布于"} ${new Date(asset.wiki?.updatedAt || asset.publishedAt).toLocaleString("zh-CN")}`);
  setText("#wiki-executive-summary", asset.wiki?.executiveSummary || asset.summary);
  setText("#wiki-overview-copy", asset.document?.summary || asset.summary);
  setText("#wiki-source-name", asset.document?.sourceName);
  setText("#wiki-source-meta", `主来源 · MinerU ${asset.document?.parserBatchId || "—"} · SHA-256 ${asset.document?.markdownSha256?.slice(0, 12) || "—"}…`);
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
  metricItems.forEach((item) => { const card = document.createElement("div"); const label = document.createElement("span"); const value = document.createElement("strong"); const note = document.createElement("small"); label.textContent = item.label; value.textContent = item.value; note.textContent = "来自 DeepSeek 结构化结果"; card.append(label, value, note); metrics.append(card); });
  const relationships = qs("#wiki-relationship-map");
  relationships.replaceChildren();
  relationships.classList.add("dynamic-relationship-list");
  const relationItems = asset.wiki?.relationships?.length ? asset.wiki.relationships : [{ source: asset.title, relation: "来源于", target: asset.document?.title }];
  relationItems.forEach((item) => { const card = document.createElement("article"); const source = document.createElement("strong"); const relation = document.createElement("span"); const target = document.createElement("b"); source.textContent = item.source; relation.textContent = item.relation || "关联"; target.textContent = item.target; card.append(source, relation, target); relationships.append(card); });
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
    assets.slice().reverse().forEach(renderPublishedAsset);
    refreshAssetTabs();
    setText("#asset-total", `当前列表 ${qsa("#asset-table-body [data-asset-id]").length} 项 · ${assets.length} 项真实发布`);
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
      renderPublishedAsset(asset);
    });
    const first = payload.publication.assets[0];
    if (first) renderDynamicWiki(first);
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
  state.analysis = { ...state.analysis, mode: "real", document: intake.name, category: intake.category, progress: 2, status: "running", liveText: "正在安全上传到本地网关", updatedAt: "刚刚" };
  qs("#real-analysis-results").hidden = true;
  updateAnalysisUI();
  saveState();
  const response = await fetch("/api/analysis", { method: "POST", body: payload });
  const submitted = await response.json().catch(() => ({}));
  if (!response.ok || !submitted.job?.id) throw new Error(submitted.error || "真实分析任务创建失败");
  state.analysis.id = submitted.job.id;
  const job = await pollRealJob(submitted.job.id);
  currentRealJob = job;
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
  state.analysis = {
    ...state.analysis,
    document: result.value.name,
    category: result.value.category,
    progress: 8,
    status: "running",
    mode: selectedRealFile ? "real" : "demo",
    liveText: selectedRealFile ? "正在启动真实服务链路" : "正在处理：文档解析",
    updatedAt: "刚刚",
  };
  const audit = makeAuditEvent("启动文档分析", `接入文档「${result.value.name}」，分类为${result.value.category}`);
  state.auditEvents = appendAudit(state.auditEvents, audit);
  appendAuditToDom(audit);
  saveState();
  updateAnalysisUI();
  qs("#intake-dialog").close();
  navigate("analysis");
  if (selectedRealFile) {
    submitButton.disabled = true;
    showToast("真实分析任务已启动", "文档将依次进入 MinerU 与 DeepSeek");
    try {
      await runRealAnalysis(result.value);
    } catch (error) {
      state.analysis = { ...state.analysis, status: "failed", liveText: String(error.message || "真实分析失败") };
      updateAnalysisUI();
      saveState();
      showToast("真实分析失败", state.analysis.liveText, "error");
    } finally {
      submitButton.disabled = false;
    }
  } else {
    qs("#real-analysis-results").hidden = true;
    showToast("分析任务已启动", "正在本地安全域内解析文档");
  }
});

qs("#advance-analysis").addEventListener("click", () => {
  if (state.analysis.mode === "real") return;
  const previous = state.analysis.status;
  state.analysis = advanceAnalysis(state.analysis, 28);
  updateAnalysisUI();
  saveState();
  if (state.analysis.status === "complete" && previous !== "complete") {
    const audit = makeAuditEvent("完成 IP 智能分析", "生成 18 项结构化资产、14 处溯源证据与 1 个 Wiki");
    state.auditEvents = appendAudit(state.auditEvents, audit);
    appendAuditToDom(audit);
    showToast("分析完成", "18 项 IP 资产已沉淀，Wiki 与脱敏副本可用");
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
      row.hidden = Boolean(query && !haystack.includes(query)) || Boolean(choice !== "all" && category !== choice) || typeMismatch;
    });
    if (rowSelector.includes("asset-table-body")) {
      const visible = qsa(rowSelector).filter((row) => !row.hidden).length;
      qs("#asset-empty-state").hidden = visible > 0;
    }
  };
  input.addEventListener("input", update);
  filter.addEventListener("change", update);
}

bindTableFilter("#document-search", "#document-filter", "[data-document-row]", ["name", "owner"]);
bindTableFilter("#asset-search", "#asset-filter", "#asset-table-body [data-asset-id]", ["name", "owner"]);

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

qsa("[data-open-provenance]").forEach((button) => {
  button.addEventListener("click", (event) => {
    event.stopPropagation();
    const evidenceId = event.currentTarget.dataset.evidenceId;
    const evidence = evidenceId && currentPublishedAssets.flatMap((asset) => asset.evidence).find((item) => item.id === evidenceId);
    if (evidence) renderEvidence(evidence, event.currentTarget);
    else openDrawer(qs("#provenance-drawer"), event.currentTarget);
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

qs("#wiki-edit").addEventListener("click", () => {
  if (!selectedPublishedAsset?.id?.startsWith("IP-REAL-")) { showToast("请选择真实 Wiki", "演示内容保持只读；发布真实分析结果后可形成版本链", "error"); return; }
  const wiki = selectedPublishedAsset.wiki || {};
  qs("#wiki-edit-base-version").value = selectedPublishedAsset.version;
  qs("#wiki-edit-title").value = wiki.title || selectedPublishedAsset.title;
  qs("#wiki-edit-summary").value = wiki.executiveSummary || selectedPublishedAsset.summary;
  qs("#wiki-edit-mechanism").value = wiki.keyMechanism || "";
  qs("#wiki-edit-note").value = "";
  setText("#wiki-edit-error", "");
  setText("#wiki-edit-version-note", `基于 ${selectedPublishedAsset.version} 创建新版本`);
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
  submit.disabled = true;
  submit.setAttribute("aria-busy", "true");
  try {
    const response = await fetch(`/api/wiki/${encodeURIComponent(selectedPublishedAsset.id)}`, { method: "PATCH", headers: { "content-type": "application/json" }, body: JSON.stringify(input) });
    const payload = await response.json().catch(() => ({}));
    if (response.status === 401) { form.closest("dialog").close(); lockWorkspace(); return; }
    if (!response.ok || !payload.wiki) throw new Error(response.status === 409 ? `版本已更新到 ${payload.currentVersion || "最新版本"}，请重新打开后编辑` : payload.error || "Wiki 保存失败");
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
qs("#asset-tag-filter").addEventListener("click", () => { qs("#asset-search").focus(); showToast("标签筛选", "可直接输入标签关键词，列表会实时过滤"); });

function findButton(root, label) {
  return qsa("button", root).find((button) => button.textContent.trim() === label);
}

findButton(qs("#view-documents"), "高级筛选")?.addEventListener("click", () => showToast("高级筛选", "当前版本支持状态、名称与责任部门联合过滤；更多企业字段可由 Schema 扩展"));
qs("#view-documents [aria-label='列表视图']")?.addEventListener("click", () => showToast("列表视图", "当前已使用适合批量复核的紧凑列表视图"));
qs("#view-documents [aria-label='刷新']")?.addEventListener("click", () => showToast("文档列表已刷新", "演示文档与当前任务状态已同步"));
qsa("#view-documents .data-table .icon-btn").forEach((button) => button.addEventListener("click", () => showToast("文档操作", "真实服务可从“接入文档”启动分析；历史样本保持只读")));
findButton(qs("#view-analysis"), "查看运行日志")?.addEventListener("click", () => showToast("运行日志", `${state.analysis.id} · ${state.analysis.progress}% · ${state.analysis.liveText}`));

const importAssetButton = findButton(qs("#view-assets .page-intro"), "导入资产");
importAssetButton?.addEventListener("click", () => showDialog(qs("#intake-dialog")));
findButton(qs("#view-assets .page-intro"), "＋ 新建资产")?.addEventListener("click", () => showToast("证据优先", "企业资产必须由来源文档或已有证据创建，请先接入文档完成分析"));

findButton(qs("#view-redaction .page-intro"), "导出脱敏副本")?.addEventListener("click", () => {
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

const auditFilter = qs("#view-audit .table-toolbar select");
const auditStatus = document.createElement("span");
auditStatus.id = "audit-filter-status";
auditStatus.className = "muted-copy";
qs("#view-audit .table-toolbar").append(auditStatus);
function updateAuditFilter() {
  const query = qs("#audit-search").value.trim().toLocaleLowerCase("zh-CN");
  const type = auditFilter.value;
  let visible = 0;
  qsa("#audit-log [data-audit-entry]").forEach((entry) => {
    const text = entry.textContent.toLocaleLowerCase("zh-CN");
    const match = (!query || text.includes(query)) && (type === "全部事件" || text.includes(type));
    entry.hidden = !match;
    if (match) visible += 1;
  });
  auditStatus.textContent = `${visible} 条匹配事件`;
}
qs("#audit-search").addEventListener("input", updateAuditFilter);
auditFilter.addEventListener("change", updateAuditFilter);
updateAuditFilter();

qs(".notification")?.addEventListener("click", () => showToast("通知中心", "当前有 3 项高敏内容待复核，可从指挥台进入安全工作台"));
qs("#logout-button")?.addEventListener("click", async () => {
  if (["loopback-demo", "static-demo"].includes(currentSession?.mode)) { showToast("当前为受控演示", "启用 SMB 账号模式后可使用真实登录与退出会话"); return; }
  const response = await fetch("/api/auth/logout", { method: "POST" }).catch(() => null);
  if (response && response.status !== 204) { showToast("暂时无法退出", "请稍后重试", "error"); return; }
  currentPublishedAssets = [];
  selectedPublishedAsset = null;
  qsa("#asset-table-body .published-row").forEach((row) => row.remove());
  lockWorkspace();
});

qsa("[data-open-redaction-source]").forEach((button) => {
  button.addEventListener("click", () => {
    const audit = makeAuditEvent("查看涂黑内容溯源", "访问 DOC-0318 第 114 页敏感片段 P-114-08");
    state.auditEvents = appendAudit(state.auditEvents, audit);
    appendAuditToDom(audit, "warning");
    saveState();
    openDrawer(qs("#provenance-drawer"));
    showToast("权限校验通过", "已记录本次 S1 敏感内容溯源操作");
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
  expiry.textContent = `离线演示 · 脱敏 Wiki · ${result.value.expires}`;
  actions.className = "icon-btn small";
  actions.textContent = "•••";
  copy.append(recipient, expiry);
  item.append(avatar, copy, actions);
  qs("#share-list").prepend(item);
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

async function loadShares() {
  if (!["owner", "admin", "editor"].includes(currentSession?.user?.role) || ["static-demo", "loopback-demo"].includes(currentSession?.mode)) return;
  try {
    const response = await fetch("/api/shares", { headers: { accept: "application/json" } });
    if (!response.ok) return;
    const { shares = [] } = await response.json();
    const list = qs("#share-list");
    list.replaceChildren(...(shares.length ? shares.map(secureShareItem) : [Object.assign(document.createElement("div"), { className: "ledger-empty", textContent: "暂无服务端安全分享" })]));
    const count = qs("#view-lifecycle .governance-grid .count-pill");
    if (count) count.textContent = `${shares.filter((share) => share.status === "active").length} 个`;
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
  showToast("安全分享已撤销", "原链接与访问码已立即失效");
});

qs("#export-audit").addEventListener("click", () => {
  const header = "event_id,action,actor,timestamp,detail\n";
  const rows = [
    ...state.auditEvents,
    { id: "AUD-20260809-2846", action: "创建安全分享", actor: "林越", timestamp: "2026-08-09 10:46", detail: "分享脱敏 Wiki" },
  ].map((event) => [event.id, event.action, event.actor, event.timestamp, event.detail].map((value) => `"${String(value).replaceAll('"', '""')}"`).join(","));
  const blob = new Blob(["\uFEFF", header, rows.join("\n")], { type: "text/csv;charset=utf-8" });
  const link = document.createElement("a");
  link.href = URL.createObjectURL(blob);
  link.download = `intelifar-audit-${new Date().toISOString().slice(0, 10)}.csv`;
  link.click();
  URL.revokeObjectURL(link.href);
  const audit = makeAuditEvent("导出审计日志", "导出当前筛选范围的 CSV 审计证据");
  state.auditEvents = appendAudit(state.auditEvents, audit);
  appendAuditToDom(audit);
  saveState();
  showToast("审计日志已导出", "CSV 文件包含事件编号、操作者与证据详情");
});

qs("#global-search").addEventListener("keydown", (event) => {
  if (event.key !== "Enter") return;
  const query = event.currentTarget.value;
  navigate("assets");
  qs("#asset-search").value = query;
  qs("#asset-search").dispatchEvent(new Event("input"));
  qs("#asset-search").focus();
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
    setText("#health-deepseek", health.providers?.deepseek === "configured" ? `已配置 · ${health.model}` : "未配置");
    setText("#health-registry", health.storage?.adapter === "sqlite" ? "SQLite 持久化 · Wiki 版本已启用" : "原子 JSON 参考模式");
    setText("#provider-health-chip", "2 个真实服务已配置");
    status.innerHTML = "<i></i> 真实服务网关在线";
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
  "mineru-upload": "MinerU 接收",
  "mineru-running": "MinerU 解析",
  deepseek: "DeepSeek 分析",
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
}

async function loadOperations() {
  const consolePanel = qs("#operations-console");
  const isAdmin = ["owner", "admin"].includes(currentSession?.user?.role);
  consolePanel.hidden = !isAdmin;
  if (!isAdmin) return;
  if (currentSession?.mode === "static-demo") {
    setText("#operations-message", "离线演示未连接管理员接口；启动真实小微网关后显示扫描、备份与任务状态。");
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
  }
}

function memberLedgerItem(member) {
  const article = document.createElement("article");
  article.dataset.memberId = member.id;
  const avatar = document.createElement("span");
  const copy = document.createElement("div");
  const name = document.createElement("strong");
  const meta = document.createElement("small");
  const role = document.createElement("select");
  const status = document.createElement("button");
  avatar.className = `member-avatar ${member.status}`;
  avatar.textContent = String(member.name || member.email).slice(0, 1);
  name.textContent = member.name;
  meta.textContent = `${member.email} · ${member.status === "active" ? "会话可用" : "会话已撤销"}`;
  for (const [value, label] of Object.entries({ admin: "管理员", editor: "编辑者", viewer: "只读" })) {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = label;
    option.selected = member.role === value;
    role.append(option);
  }
  role.dataset.memberRole = member.id;
  role.disabled = member.role === "owner" || member.id === currentSession?.user?.id;
  if (member.role === "owner") {
    role.replaceChildren(Object.assign(document.createElement("option"), { value: "owner", textContent: "空间所有者" }));
  }
  status.type = "button";
  status.className = "secondary-btn compact member-status-action";
  status.dataset.memberId = member.id;
  status.textContent = member.status === "active" ? "禁用" : "启用";
  status.disabled = member.role === "owner" || member.id === currentSession?.user?.id;
  copy.append(name, meta);
  article.append(avatar, copy, role, status);
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

async function loadMembers() {
  if (!["owner", "admin"].includes(currentSession?.user?.role) || ["static-demo", "loopback-demo"].includes(currentSession?.mode)) return;
  try {
    const response = await fetch("/api/admin/members", { headers: { accept: "application/json" } });
    if (!response.ok) return;
    const payload = await response.json();
    currentMembers = payload.members || [];
    setText("#member-count", `${currentMembers.filter((member) => member.status === "active").length} 位有效成员`);
    qs("#member-ledger").replaceChildren(...(currentMembers.length ? currentMembers.map(memberLedgerItem) : [Object.assign(document.createElement("div"), { className: "ledger-empty", textContent: "暂无空间成员" })]));
    const invitations = (payload.invitations || []).filter((invitation) => invitation.status === "pending").slice(0, 6);
    qs("#invitation-ledger").replaceChildren(...(invitations.length ? invitations.map(invitationLedgerItem) : [Object.assign(document.createElement("div"), { className: "ledger-empty", textContent: "暂无待处理邀请" })]));
  } catch {
    setText("#member-count", "成员接口不可用");
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

qs("#member-ledger").addEventListener("change", async (event) => {
  const select = event.target.closest("[data-member-role]");
  if (!select) return;
  select.disabled = true;
  try { await updateMemberFromLedger(select.dataset.memberRole, { role: select.value }); }
  catch (error) { showToast("无法修改角色", String(error.message), "error"); await loadMembers(); }
});

qs("#member-ledger").addEventListener("click", async (event) => {
  const button = event.target.closest(".member-status-action");
  if (!button) return;
  const member = currentMembers.find((item) => item.id === button.dataset.memberId);
  button.disabled = true;
  try { await updateMemberFromLedger(member.id, { status: member.status === "active" ? "disabled" : "active" }); }
  catch (error) { showToast("无法修改成员状态", String(error.message), "error"); button.disabled = false; }
});

qs("#invitation-ledger").addEventListener("click", async (event) => {
  const button = event.target.closest(".revoke-invitation");
  if (!button) return;
  button.disabled = true;
  const response = await fetch(`/api/admin/invitations/${encodeURIComponent(button.dataset.invitationId)}`, { method: "DELETE", headers: { accept: "application/json" } }).catch(() => null);
  if (!response?.ok) { button.disabled = false; showToast("无法撤销邀请", "请刷新后重试", "error"); return; }
  await loadMembers();
  showToast("邀请已撤销", "一次性激活链接已立即失效");
});

qs("#open-invitation").addEventListener("click", () => {
  const form = qs("#invitation-form");
  form.reset();
  setText("#invitation-error", "");
  qs("#invitation-secret-result").hidden = true;
  qs("#invitation-result-link").value = "";
  qs("[data-testid='create-invitation']").disabled = false;
  qs("[data-testid='create-invitation']").textContent = "生成一次性邀请";
  showDialog(qs("#invitation-dialog"));
});

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
    button.textContent = "✓ 邀请已生成";
    await loadMembers();
    showToast("一次性邀请已生成", "当前未配置邮件服务，请复制链接后通过可信渠道发送");
  } catch (error) {
    button.disabled = false;
    setText("#invitation-error", String(error.message || "邀请创建失败"));
  } finally {
    button.removeAttribute("aria-busy");
  }
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
qs('[data-nav="system"]').addEventListener("click", loadOperations);
qs('[data-nav="lifecycle"]').addEventListener("click", loadShares);

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
if (await initializeSession()) {
  await Promise.all([loadPublishedAssets(), loadOperations(), loadShares(), loadMembers()]);
}
