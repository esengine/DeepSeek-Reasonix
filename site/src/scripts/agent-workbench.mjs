const TERMINAL_STATES = new Set(["complete", "needs_review", "failed", "interrupted", "cancelled", "blocked"]);
const STATUS_LABELS = {
  planning: "正在规划",
  running: "执行中",
  synthesizing: "正在整理结果",
  complete: "已完成",
  needs_review: "需要复核",
  blocked: "已拦截",
  failed: "已安全停止",
  interrupted: "已中断",
  cancelled: "已取消",
};
const TOOL_LABELS = {
  search_assets: "搜索授权资产",
  read_asset: "读取资产档案",
  read_wiki: "读取正式 Wiki",
  read_evidence: "核验原文证据",
  inspect_neighborhood: "分析关系邻域",
  compare_assets: "比较资产",
  compose_wiki_draft: "准备 Wiki 草案",
};

export function sourceButtonLabel(sourceId, index = 0) {
  const number = Number(index) + 1;
  if (String(sourceId).startsWith("EV-")) return `查看原文依据 ${number}`;
  if (String(sourceId).startsWith("REL-")) return `查看关联关系 ${number}`;
  return `查看相关资产 ${number}`;
}

export function isAgentTerminal(state) { return TERMINAL_STATES.has(String(state)); }

export function retryTaskDraft(task) {
  if (!task || !["failed", "interrupted", "cancelled", "blocked", "needs_review"].includes(String(task.state))) return null;
  const value = String(task.prompt || "").trim();
  if (!value) return null;
  return { prompt: value, templateId: String(task.templateId || "") };
}

export function taskViewModel(task) {
  const state = String(task?.state || "planning");
  const steps = Array.isArray(task?.plan?.steps) ? task.plan.steps : [];
  const events = Array.isArray(task?.events) ? task.events : [];
  const complete = new Set(events.filter((event) => event.type === "step.complete").map((event) => event.stepId));
  const active = events.findLast?.((event) => event.type === "step.started" && !complete.has(event.stepId))?.stepId ?? null;
  return {
    state,
    statusLabel: STATUS_LABELS[state] || state,
    terminal: isAgentTerminal(state),
    steps: steps.map((step) => ({ ...step, status: complete.has(step.id) ? "complete" : active === step.id ? "active" : "pending" })),
    progress: steps.length ? Math.round((complete.size / steps.length) * 100) : state === "complete" ? 100 : 0,
  };
}

function nowIso() { return new Date().toISOString(); }

export function createStaticAgentTask(prompt, templateId = "") {
  const id = `AGT-DEMO-${Date.now().toString(36).toUpperCase()}`;
  const createdAt = nowIso();
  if (/(?:写|运行|执行).{0,8}(?:代码|脚本|shell|命令)|(?:部署|删除|发布|发邮件)/iu.test(prompt)) {
    return { id, prompt, templateId, state: "blocked", stageLabel: "请求超出 IP 任务助手边界", boundary: { code: "demo_boundary", message: "IP 任务助手只处理文档、IP 资产、证据和 Wiki 分析，不执行代码、系统操作或正式知识变更。" }, createdAt, updatedAt: createdAt, completedAt: createdAt, events: [{ type: "policy.blocked", stepId: null, createdAt }] };
  }
  const plan = {
    title: templateId === "wiki_draft" ? "Wiki 草案准备" : templateId === "asset_inventory" ? "IP 资产盘点" : "资产关系影响分析",
    intent: templateId || "impact_analysis",
    outputType: templateId === "wiki_draft" ? "wiki_draft" : "analysis_report",
    steps: [
      { id: "S1", title: "搜索当前账号可见资产", tool: "search_assets", arguments: { query: "目标资产", limit: 10 } },
      { id: "S2", title: "读取已确认关系与证据", tool: "inspect_neighborhood", arguments: { assetId: "IP-2026-0841", depth: 1 } },
      { id: "S3", title: "检查结论是否有原文依据", tool: "read_evidence", arguments: { evidenceId: "EV-DEMO-048-03" } },
    ],
  };
  const events = [{ type: "task.created", stepId: null, createdAt }, { type: "plan.ready", stepId: null, createdAt }, ...plan.steps.flatMap((step) => [{ type: "step.started", stepId: step.id, createdAt }, { type: "step.complete", stepId: step.id, createdAt }]), { type: "delivery.complete", stepId: null, createdAt }];
  return {
    id, prompt, templateId, state: "complete", stageLabel: "任务结果已通过证据门禁", plan, createdAt, updatedAt: createdAt, completedAt: createdAt, events,
    usage: { totalTokens: 0 },
    result: {
      status: "complete", title: plan.title, summary: "已在当前账号可见范围内完成受控分析。结果只使用已发布资产和已确认关系；演示数据不会写入正式 Wiki。",
      findings: [
        { id: "F1", title: "核心路由方案依赖运行时与缓存能力", detail: "关系全景显示该资产与长上下文缓存、异构运行时存在已确认依赖或实现关系。", sourceIds: ["IP-2026-0841"], confidence: .93 },
        { id: "F2", title: "变更前需要复核性能结论", detail: "现有 Wiki 的性能指标应继续由原文证据约束，不能仅根据关系网络自动更新。", sourceIds: ["IP-2026-0841"], confidence: .89 },
      ],
      uncertainties: ["演示空间未连接真实任务存储；正式使用时会按当前账号权限重新检索。"],
      deliverables: [{ type: plan.outputType, title: templateId === "wiki_draft" ? "Wiki 更新建议（草案）" : "资产影响清单", content: "1. 复核上游依赖的版本变化\n2. 核对关键指标的原文证据\n3. 由知识编辑者在正式 Wiki 页面决定是否保存" }],
      nextActions: ["打开资产详情复核负责人和证据", "如需正式更新，进入 Wiki 编辑流程人工保存"],
      excludedActions: ["未执行代码、命令或外网访问", "未保存、发布或分享正式知识", "未修改成员权限或关系状态"],
      quality: { totalClaims: 2, groundedClaims: 2, downgradedClaims: 0, evidenceCoverage: 1 },
    },
  };
}

function element(name, className, text) {
  const node = document.createElement(name);
  if (className) node.className = className;
  if (text != null) node.textContent = String(text);
  return node;
}

function formatTime(value) {
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? "刚刚" : date.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
}

export function createAgentWorkbench(options = {}) {
  const root = options.root ?? document;
  const fetchImpl = options.fetchImpl ?? fetch;
  const modeProvider = options.modeProvider ?? (() => "local-session");
  const onSource = options.onSource ?? (() => {});
  const onAuthRequired = options.onAuthRequired ?? (() => {});
  const onToast = options.onToast ?? (() => {});
  const onTaskSettled = options.onTaskSettled ?? (() => {});
  const form = root.querySelector("#agent-task-form");
  const prompt = root.querySelector("#agent-prompt");
  const templateInput = root.querySelector("#agent-template-id");
  const detail = root.querySelector("#agent-task-detail");
  let tasks = [];
  let currentTask = null;
  let pollTimer = null;
  const notifiedTerminalTasks = new Set();

  function staticMode() { return ["static-demo", "loopback-demo"].includes(modeProvider()); }

  function renderHistory() {
    const list = root.querySelector("#agent-history-list");
    list.replaceChildren();
    if (!tasks.length) {
      const empty = element("div", "agent-history-empty");
      empty.append(element("span", "", "✦"), element("strong", "", "还没有任务记录"), element("small", "", "选择模板或直接描述业务任务。"));
      list.append(empty);
      return;
    }
    for (const task of tasks) {
      const button = element("button", "agent-history-item");
      button.type = "button";
      button.dataset.state = task.state;
      button.classList.toggle("is-active", task.id === currentTask?.id);
      const marker = element("i");
      const copy = element("span");
      copy.append(element("strong", "", task.plan?.title || task.prompt || "IP 智能任务"), element("small", "", `${STATUS_LABELS[task.state] || task.state} · ${task.id}`));
      button.append(marker, copy, element("time", "", formatTime(task.updatedAt || task.createdAt)));
      button.addEventListener("click", () => selectTask(task.id));
      list.append(button);
    }
  }

  function renderSteps(task) {
    const view = taskViewModel(task);
    const list = root.querySelector("#agent-step-list");
    list.replaceChildren();
    if (!view.steps.length) {
      const item = element("li", task.state === "blocked" || task.state === "failed" ? "is-pending" : "is-active");
      const copy = element("div");
      copy.append(element("strong", "", task.state === "blocked" ? "请求策略检查" : "生成领域计划"), element("small", "", task.stageLabel || "只会选择允许的 IP/Wiki 工具"));
      item.replaceChildren(element("span", "", "01"), copy, element("b", "", task.state === "blocked" ? "×" : "…"));
      list.append(item);
      root.querySelector("#agent-plan-count").textContent = task.state === "blocked" ? "模型调用前已拦截" : "等待计划";
      return;
    }
    view.steps.forEach((step, index) => {
      const item = element("li", `is-${step.status}`);
      const copy = element("div");
      copy.append(element("strong", "", step.title), element("small", "", TOOL_LABELS[step.tool] || step.tool));
      item.append(element("span", "", String(index + 1).padStart(2, "0")), copy, element("b", "", step.status === "complete" ? "完成" : step.status === "active" ? "执行中" : "等待"));
      list.append(item);
    });
    root.querySelector("#agent-plan-count").textContent = `${view.steps.length} 步 · ${view.progress}%`;
  }

  function renderStringList(target, values, className, emptyText) {
    target.replaceChildren();
    const list = Array.isArray(values) ? values : [];
    if (!list.length) { target.append(element("div", className, emptyText)); return; }
    for (const value of list) target.append(element("div", className, value));
  }

  function renderResult(result) {
    const section = root.querySelector("#agent-result");
    section.hidden = !result;
    if (!result) return;
    root.querySelector("#agent-result-title").textContent = result.title || "任务结果包";
    root.querySelector("#agent-result-summary").textContent = result.summary || "";
    root.querySelector("#agent-evidence-coverage").textContent = `${Math.round(Number(result.quality?.evidenceCoverage || 0) * 100)}%`;
    root.querySelector("#agent-grounded-count").textContent = `${Number(result.quality?.groundedClaims || 0)} 项`;
    const findings = root.querySelector("#agent-findings");
    findings.replaceChildren();
    for (const finding of result.findings ?? []) {
      const article = element("article", "agent-finding");
      const header = element("header");
      header.append(element("strong", "", finding.title), element("span", "", `${Math.round(Number(finding.confidence || 0) * 100)}%`));
      const sources = element("div", "agent-source-list");
      for (const [sourceIndex, sourceId] of (finding.sourceIds ?? []).entries()) {
        const button = element("button", "", sourceButtonLabel(sourceId, sourceIndex));
        button.type = "button";
        button.title = `内部来源编号：${sourceId}`;
        button.addEventListener("click", () => onSource(sourceId, button));
        sources.append(button);
      }
      article.append(header, element("p", "", finding.detail), sources);
      findings.append(article);
    }
    if (!findings.children.length) findings.append(element("div", "agent-uncertainty", "当前没有足够原文依据形成可交付条目。"));
    renderStringList(root.querySelector("#agent-uncertainties"), result.uncertainties, "agent-uncertainty", "没有新增待核实项。 ");
    renderStringList(root.querySelector("#agent-next-actions"), result.nextActions, "agent-next", "暂无建议动作。 ");
    const deliverables = root.querySelector("#agent-deliverable-list");
    deliverables.replaceChildren();
    for (const deliverable of result.deliverables ?? []) {
      const card = element("article", "agent-deliverable");
      card.append(element("span", "", deliverable.type), element("strong", "", deliverable.title), element("p", "", deliverable.content));
      deliverables.append(card);
    }
    if (!deliverables.children.length) deliverables.append(element("article", "agent-deliverable", "当前任务没有生成独立草案交付物。"));
    root.querySelector("#agent-excluded-actions").textContent = (result.excludedActions ?? ["未改变任何正式知识"]).join("；");
  }

  function renderTask(task) {
    const shouldReveal = detail.hidden || currentTask?.id !== task.id;
    currentTask = task;
    detail.hidden = false;
    const view = taskViewModel(task);
    const status = root.querySelector("#agent-task-state");
    status.dataset.state = view.state;
    status.querySelector("b").textContent = view.statusLabel;
    root.querySelector("#agent-task-title").textContent = task.plan?.title || task.result?.title || "IP 智能任务";
    root.querySelector("#agent-task-summary").textContent = task.boundary?.message || task.error || task.result?.summary || task.stageLabel || "任务正在执行。";
    root.querySelector("#agent-task-id").textContent = task.id;
    root.querySelector("#agent-cancel").hidden = view.terminal;
    root.querySelector("#agent-retry").hidden = !retryTaskDraft(task);
    renderSteps(task);
    renderResult(task.result);
    renderHistory();
    if (view.terminal && !notifiedTerminalTasks.has(task.id)) {
      notifiedTerminalTasks.add(task.id);
      onTaskSettled(task);
    }
    if (shouldReveal) detail.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  async function readTask(id) {
    if (staticMode()) return tasks.find((task) => task.id === id) ?? null;
    const response = await fetchImpl(`/api/agent/tasks/${encodeURIComponent(id)}`, { headers: { accept: "application/json" } });
    if (response.status === 401) { onAuthRequired(); return null; }
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(payload.error || "无法读取任务");
    return payload.task;
  }

  async function selectTask(id) {
    try {
      const task = await readTask(id);
      if (!task) return;
      const index = tasks.findIndex((item) => item.id === task.id);
      if (index >= 0) tasks[index] = task; else tasks.unshift(task);
      renderTask(task);
      if (!isAgentTerminal(task.state)) schedulePoll(task.id);
    } catch (error) { onToast("无法读取任务", String(error.message || error), "error"); }
  }

  function schedulePoll(id) {
    clearTimeout(pollTimer);
    pollTimer = setTimeout(async () => {
      await selectTask(id);
    }, 700);
  }

  async function loadTasks() {
    if (staticMode()) { renderHistory(); return tasks; }
    try {
      const response = await fetchImpl("/api/agent/tasks?limit=30", { headers: { accept: "application/json" } });
      if (response.status === 401) { onAuthRequired(); return []; }
      if (!response.ok) throw new Error("任务记录暂不可用");
      tasks = (await response.json()).tasks ?? [];
      renderHistory();
      return tasks;
    } catch (error) { onToast("任务记录未加载", String(error.message || error), "error"); return []; }
  }

  async function submitTask() {
    const value = prompt.value.trim();
    if (!value) { root.querySelector("#agent-form-message").textContent = "请先描述一个文档 IP 或 Wiki 任务。"; prompt.focus(); return; }
    const button = form.querySelector("[type='submit']");
    button.disabled = true;
    button.setAttribute("aria-busy", "true");
    root.querySelector("#agent-form-message").textContent = "正在建立受控任务契约…";
    try {
      let task;
      if (staticMode()) task = createStaticAgentTask(value, templateInput.value);
      else {
        const response = await fetchImpl("/api/agent/tasks", { method: "POST", headers: { "content-type": "application/json", accept: "application/json" }, body: JSON.stringify({ prompt: value, templateId: templateInput.value }) });
        if (response.status === 401) { onAuthRequired(); return; }
        const payload = await response.json().catch(() => ({}));
        if (!response.ok) throw new Error(payload.error || "任务提交失败");
        task = payload.task;
      }
      tasks = [task, ...tasks.filter((item) => item.id !== task.id)];
      renderTask(task);
      root.querySelector("#agent-form-message").textContent = task.state === "blocked" ? "请求已在模型调用前被边界策略拦截。" : "任务已进入受控执行队列。";
      onToast(task.state === "blocked" ? "请求已安全拦截" : "IP 任务已建立", task.stageLabel, task.state === "blocked" ? "error" : "success");
      if (!isAgentTerminal(task.state)) schedulePoll(task.id);
    } catch (error) { root.querySelector("#agent-form-message").textContent = String(error.message || error); onToast("任务未提交", String(error.message || error), "error"); }
    finally { button.disabled = false; button.removeAttribute("aria-busy"); }
  }

  root.querySelectorAll("[data-agent-template]").forEach((button) => button.addEventListener("click", () => {
    root.querySelectorAll("[data-agent-template]").forEach((item) => item.classList.toggle("is-selected", item === button));
    templateInput.value = button.dataset.agentTemplate;
    prompt.value = button.dataset.agentPrompt;
    prompt.dispatchEvent(new Event("input"));
    prompt.focus();
  }));
  prompt.addEventListener("input", () => { root.querySelector("#agent-prompt-count").textContent = `${prompt.value.length.toLocaleString("zh-CN")} / 4,000`; root.querySelector("#agent-form-message").textContent = ""; });
  form.addEventListener("submit", (event) => { event.preventDefault(); submitTask(); });
  root.querySelector("#agent-refresh").addEventListener("click", loadTasks);
  root.querySelector("#agent-retry").addEventListener("click", () => {
    const draft = retryTaskDraft(currentTask);
    if (!draft) return;
    prompt.value = draft.prompt;
    templateInput.value = draft.templateId;
    root.querySelectorAll("[data-agent-template]").forEach((item) => item.classList.toggle("is-selected", item.dataset.agentTemplate === draft.templateId));
    prompt.dispatchEvent(new Event("input"));
    root.querySelector("#agent-form-message").textContent = "已保留原任务内容，请调整后再次委托。";
    form.scrollIntoView({ behavior: "smooth", block: "center" });
    prompt.focus();
  });
  root.querySelector("#agent-cancel").addEventListener("click", async () => {
    if (!currentTask || isAgentTerminal(currentTask.state)) return;
    if (staticMode()) {
      currentTask = { ...currentTask, state: "cancelled", stageLabel: "任务已由用户取消", updatedAt: nowIso() };
      tasks = tasks.map((task) => task.id === currentTask.id ? currentTask : task);
      renderTask(currentTask);
      return;
    }
    const response = await fetchImpl(`/api/agent/tasks/${encodeURIComponent(currentTask.id)}/cancel`, { method: "POST", headers: { accept: "application/json" } });
    const payload = await response.json().catch(() => ({}));
    if (response.ok) { tasks = tasks.map((task) => task.id === payload.task.id ? payload.task : task); renderTask(payload.task); }
    else onToast("无法取消任务", payload.error || "请稍后重试", "error");
  });

  return { loadTasks, selectTask, submitTask, destroy() { clearTimeout(pollTimer); } };
}
