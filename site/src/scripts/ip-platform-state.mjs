export const PIPELINE_STAGES = [
  { id: "parse", label: "文档解析", threshold: 18 },
  { id: "classify", label: "智能分类", threshold: 36 },
  { id: "extract", label: "IP 提取", threshold: 62 },
  { id: "verify", label: "交叉校验", threshold: 82 },
  { id: "wiki", label: "Wiki 生成", threshold: 100 },
];

export function createInitialState() {
  return {
    activeView: "overview",
    theme: "light",
    analysis: {
      id: "JOB-240809-018",
      document: "星穹推理引擎技术白皮书 V3.2.pdf",
      category: "技术白皮书",
      progress: 72,
      status: "running",
      updatedAt: "刚刚",
    },
    documents: [],
    auditEvents: [],
  };
}

export function validateIntake(input) {
  const errors = {};
  const name = String(input?.name ?? "").trim();
  const category = String(input?.category ?? "").trim();
  if (!name) errors.name = "请输入文档名称";
  if (!category) errors.category = "请选择文档分类";
  return { valid: Object.keys(errors).length === 0, errors, value: { name, category } };
}

export function stageState(progress, threshold) {
  if (progress >= threshold) return "complete";
  const previous = PIPELINE_STAGES.findLast((stage) => stage.threshold < threshold)?.threshold ?? 0;
  if (progress > previous) return "active";
  return "pending";
}

export function advanceAnalysis(analysis, step = 16) {
  const progress = Math.min(100, Math.max(0, analysis.progress + step));
  return {
    ...analysis,
    progress,
    status: progress === 100 ? "complete" : "running",
    updatedAt: "刚刚",
  };
}

export function filterRows(rows, query = "", category = "all") {
  const normalized = query.trim().toLocaleLowerCase("zh-CN");
  return rows.filter((row) => {
    const matchesCategory = category === "all" || row.category === category || row.status === category;
    const haystack = `${row.name} ${row.owner ?? ""} ${row.id ?? ""}`.toLocaleLowerCase("zh-CN");
    return matchesCategory && (!normalized || haystack.includes(normalized));
  });
}

export function validateShare(input) {
  const errors = {};
  const recipient = String(input?.recipient ?? "").trim();
  const expires = String(input?.expires ?? "").trim();
  if (!recipient || !recipient.includes("@")) errors.recipient = "请输入有效的企业邮箱";
  if (!expires) errors.expires = "请选择访问有效期";
  return { valid: Object.keys(errors).length === 0, errors, value: { recipient, expires } };
}

export function makeAuditEvent(action, detail, actor = "林越") {
  return {
    id: `AUD-${Date.now()}`,
    action,
    detail,
    actor,
    timestamp: new Intl.DateTimeFormat("zh-CN", {
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    }).format(new Date()),
  };
}

export function appendAudit(events, event) {
  return [event, ...events].slice(0, 50);
}
