export const AGENT_INTENTS = Object.freeze([
  "asset_inventory",
  "evidence_review",
  "impact_analysis",
  "document_comparison",
  "wiki_draft",
  "risk_gap_review",
  "due_diligence_pack",
]);

export const AGENT_TOOLS = Object.freeze([
  "search_assets",
  "read_asset",
  "read_wiki",
  "read_evidence",
  "inspect_neighborhood",
  "compare_assets",
  "compose_wiki_draft",
]);

const INTENT_SET = new Set(AGENT_INTENTS);
const TOOL_SET = new Set(AGENT_TOOLS);
const ASSET_ID = /^IP-[A-Za-z0-9-]{2,96}$/;
const EVIDENCE_ID = /^EV-[A-Za-z0-9-]{1,96}$/;
const SOURCE_ID = /^(?:IP|EV|REL)-[A-Za-z0-9-]{1,96}$/;
const MAX_STEPS = 6;
const MAX_TOOL_CALLS = 12;
const UNSAFE_ARGUMENT_KEYS = /^(?:workspaceId|workspace_id|tenantId|url|uri|path|file|filename|command|code|sql|headers|cookie|token|apiKey|secret)$/i;
const ARGUMENT_ALIASES = Object.freeze({
  asset_id: "assetId",
  evidence_id: "evidenceId",
  asset_ids: "assetIds",
  include_proposed: "includeProposed",
});

function contractError(message) {
  const error = new Error(message);
  error.code = "INVALID_AGENT_PLAN";
  return error;
}

function text(value, maxLength = 2_000, fallback = "") {
  const normalized = String(value ?? "").normalize("NFKC").replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/g, " ").replace(/\s+/g, " ").trim();
  return (normalized || fallback).slice(0, maxLength);
}

function assertPlainObject(value, label) {
  if (!value || typeof value !== "object" || Array.isArray(value) || Object.getPrototypeOf(value) !== Object.prototype) throw contractError(`${label} must be an object`);
}

function assertNoUnsafeKeys(value) {
  if (!value || typeof value !== "object") return;
  for (const [key, nested] of Object.entries(value)) {
    if (UNSAFE_ARGUMENT_KEYS.test(key)) throw contractError(`Agent plan argument ${key} is not allowed`);
    assertNoUnsafeKeys(nested);
  }
}

function exactKeys(input, allowed, tool) {
  for (const key of Object.keys(input)) if (!allowed.includes(key)) throw contractError(`${tool} argument ${key} is not allowed`);
}

function assetId(value, label = "assetId") {
  const normalized = text(value, 100);
  if (!ASSET_ID.test(normalized)) throw contractError(`${label} is invalid`);
  return normalized;
}

function evidenceId(value) {
  const normalized = text(value, 100);
  if (!EVIDENCE_ID.test(normalized)) throw contractError("evidenceId is invalid");
  return normalized;
}

function validateArguments(tool, raw) {
  const source = raw ?? {};
  assertPlainObject(source, `${tool} arguments`);
  assertNoUnsafeKeys(source);
  const input = {};
  for (const [key, value] of Object.entries(source)) {
    const canonicalKey = ARGUMENT_ALIASES[key] ?? key;
    if (Object.hasOwn(input, canonicalKey)) throw contractError(`${tool} argument ${canonicalKey} was provided more than once`);
    input[canonicalKey] = value;
  }
  if (tool === "search_assets") {
    exactKeys(input, ["query", "limit"], tool);
    const query = text(input.query, 160);
    if (!query) throw contractError("search_assets query is required");
    return { query, limit: Math.max(1, Math.min(20, Number(input.limit) || 10)) };
  }
  if (["read_asset", "read_wiki"].includes(tool)) {
    exactKeys(input, ["assetId"], tool);
    return { assetId: assetId(input.assetId) };
  }
  if (tool === "read_evidence") {
    exactKeys(input, ["evidenceId"], tool);
    return { evidenceId: evidenceId(input.evidenceId) };
  }
  if (tool === "inspect_neighborhood") {
    exactKeys(input, ["assetId", "depth", "includeProposed"], tool);
    const depth = Number(input.depth ?? 1);
    if (!Number.isInteger(depth) || depth < 1 || depth > 2) throw contractError("inspect_neighborhood depth must be 1 or 2");
    return { assetId: assetId(input.assetId), depth, includeProposed: input.includeProposed === true };
  }
  if (tool === "compare_assets") {
    exactKeys(input, ["assetIds"], tool);
    const ids = [...new Set((Array.isArray(input.assetIds) ? input.assetIds : []).map((id) => assetId(id)))];
    if (ids.length < 2 || ids.length > 5) throw contractError("compare_assets requires 2 to 5 distinct asset IDs");
    return { assetIds: ids };
  }
  if (tool === "compose_wiki_draft") {
    exactKeys(input, ["assetId", "instructions"], tool);
    const instructions = text(input.instructions, 2_001);
    if (!instructions || instructions.length > 2_000) throw contractError("compose_wiki_draft instructions must contain 1 to 2000 characters");
    return { assetId: assetId(input.assetId), instructions };
  }
  throw contractError(`Unknown agent tool: ${tool}`);
}

export function validateAgentPlan(raw) {
  assertPlainObject(raw, "Agent plan");
  const intent = text(raw.intent, 80);
  if (!INTENT_SET.has(intent)) throw contractError("Agent plan intent is not allowed");
  const steps = Array.isArray(raw.steps) ? raw.steps : [];
  if (!steps.length || steps.length > MAX_STEPS) throw contractError(`Agent plan must contain 1 to ${MAX_STEPS} steps`);
  const ids = new Set();
  const normalizedSteps = steps.map((step, index) => {
    assertPlainObject(step, `Agent step ${index + 1}`);
    const id = text(step.id, 20, `S${index + 1}`).toUpperCase();
    if (!/^S[1-6]$/.test(id) || ids.has(id)) throw contractError("Agent step IDs must be unique S1 to S6 values");
    ids.add(id);
    const tool = text(step.tool, 80);
    if (!TOOL_SET.has(tool)) throw contractError(`Agent tool ${tool || "(empty)"} is not allowed`);
    const title = text(step.title, 120);
    if (!title) throw contractError("Agent step title is required");
    return { id, title, tool, arguments: validateArguments(tool, step.arguments) };
  });
  return {
    title: text(raw.title, 160, "IP 智能任务"),
    intent,
    outputType: text(raw.outputType, 80, "analysis_report").replace(/[^a-z0-9_-]/gi, "") || "analysis_report",
    steps: normalizedSteps,
    maxToolCalls: MAX_TOOL_CALLS,
  };
}

function walkSources(value, result) {
  if (Array.isArray(value)) {
    for (const item of value) walkSources(item, result);
    return;
  }
  if (!value || typeof value !== "object") return;
  for (const [key, nested] of Object.entries(value)) {
    if ((key === "id" || key === "assetId" || key === "sourceAssetId" || key === "targetAssetId" || key === "evidenceId") && typeof nested === "string" && SOURCE_ID.test(nested)) result.add(nested);
    walkSources(nested, result);
  }
}

export function collectSourceIds(receipts) {
  const result = new Set();
  for (const receipt of Array.isArray(receipts) ? receipts : []) {
    if (!TOOL_SET.has(receipt?.tool)) continue;
    walkSources(receipt.output, result);
    if (receipt.tool === "read_wiki" && ASSET_ID.test(receipt.output?.wiki?.assetId ?? "")) result.add(`WIKI:${receipt.output.wiki.assetId}`);
  }
  return result;
}

function listOfText(value, maxItems, maxLength) {
  return (Array.isArray(value) ? value : []).map((item) => text(item, maxLength)).filter(Boolean).slice(0, maxItems);
}

function normalizeDeliverables(value) {
  return (Array.isArray(value) ? value : []).slice(0, 8).map((item) => ({
    type: text(item?.type, 80, "analysis_note").replace(/[^a-z0-9_-]/gi, "") || "analysis_note",
    title: text(item?.title, 160, "任务交付物"),
    content: text(item?.content, 8_000),
  })).filter((item) => item.content);
}

function normalizeVisibleAssetCount(value, visibleAssetCount) {
  const summary = text(value, 3_000);
  if (!Number.isInteger(visibleAssetCount) || visibleAssetCount < 0) return summary;
  return summary.replace(
    /已核查[^,，。；;]{0,80}(?:[,，]\s*)?共\s*\d+\s*(?:项|个|条)(?:\s*资产)?(?:[,，]\s*)?/g,
    `已核查本次返回的 ${visibleAssetCount} 项资产，`,
  );
}

export function normalizeAgentResult(raw, options = {}) {
  const allowed = options.allowedSourceIds instanceof Set ? options.allowedSourceIds : new Set(options.allowedSourceIds ?? []);
  const findings = [];
  const downgraded = [];
  const claims = (Array.isArray(raw?.findings) ? raw.findings : []).slice(0, 20);
  for (let index = 0; index < claims.length; index += 1) {
    const claim = claims[index] ?? {};
    const title = text(claim.title, 200, `发现 ${index + 1}`);
    const detail = text(claim.detail, 2_000);
    const rawSourceIds = claim.sourceIds ?? claim.source_ids;
    const sourceIds = [...new Set((Array.isArray(rawSourceIds) ? rawSourceIds : []).map((id) => text(id, 120)).filter(Boolean))];
    if (!detail || !sourceIds.length || sourceIds.some((id) => !allowed.has(id))) {
      downgraded.push(`待核实：${title}（模型未提供当前任务可验证的来源）`);
      continue;
    }
    findings.push({
      id: `F${findings.length + 1}`,
      title,
      detail,
      sourceIds,
      confidence: Math.max(0, Math.min(1, Number(claim.confidence) || 0)),
    });
  }
  const requestedStatus = ["complete", "needs_review", "blocked"].includes(raw?.status) ? raw.status : "needs_review";
  const needsReview = downgraded.length > 0 || findings.length === 0;
  const totalClaims = claims.length;
  const groundedClaims = findings.length;
  return {
    status: requestedStatus === "blocked" ? "blocked" : needsReview ? "needs_review" : requestedStatus,
    title: text(raw?.title, 200, "IP 智能任务结果"),
    summary: normalizeVisibleAssetCount(
      raw?.summary ?? (findings.length ? "已完成有证据约束的任务分析。" : "当前授权范围内没有足够依据形成确定结论。"),
      options.visibleAssetCount,
    ),
    findings,
    uncertainties: [...listOfText(raw?.uncertainties, 20, 1_000), ...downgraded].slice(0, 20),
    deliverables: normalizeDeliverables(raw?.deliverables),
    nextActions: listOfText(raw?.nextActions ?? raw?.next_actions, 12, 500),
    excludedActions: listOfText(options.excludedActions, 12, 500),
    quality: {
      totalClaims,
      groundedClaims,
      downgradedClaims: downgraded.length,
      evidenceCoverage: totalClaims ? Number((groundedClaims / totalClaims).toFixed(4)) : 0,
    },
  };
}

export const AGENT_LIMITS = Object.freeze({ maxSteps: MAX_STEPS, maxToolCalls: MAX_TOOL_CALLS });
