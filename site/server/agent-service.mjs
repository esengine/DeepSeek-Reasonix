import { randomUUID } from "node:crypto";
import { collectSourceIds, normalizeAgentResult, validateAgentPlan } from "./agent-contract.mjs";
import { evaluateAgentRequest, normalizeAgentRequest } from "./agent-policy.mjs";

const TERMINAL_STATES = new Set(["complete", "needs_review", "failed", "interrupted", "cancelled", "blocked"]);
const EDIT_ROLES = new Set(["owner", "admin", "editor"]);
const EXCLUDED_ACTIONS = Object.freeze([
  "未执行代码、命令、文件系统或任意网络操作",
  "未保存、发布、删除、分享或覆盖任何正式知识",
  "未修改成员、角色、权限或关系确认状态",
]);

function isoNow() { return new Date().toISOString(); }

function safeFailure(error) {
  if (error?.code === "INVALID_AGENT_PLAN") return "任务未通过安全计划校验，未执行任何领域工具";
  if (error?.code === "AGENT_MODEL_ERROR") return "DeepSeek 任务服务暂不可用，请稍后重试";
  if (error?.code === "AGENT_IDENTITY_INACTIVE") return "当前账号或权限状态不允许继续任务";
  if (error?.code === "AGENT_ROLE_REQUIRED") return "当前角色不具备该任务步骤所需的知识编辑权限";
  if (error?.code === "AGENT_SOURCE_NOT_FOUND") return "当前权限范围内没有找到任务需要的资产或证据";
  return "任务执行失败；未执行任何正式知识变更";
}

function resultCount(output) {
  for (const key of ["assets", "nodes", "edges", "results"]) if (Array.isArray(output?.[key])) return output[key].length;
  if (output?.graph) return Number(output.graph.nodes?.length ?? 0) + Number(output.graph.edges?.length ?? 0);
  return output && typeof output === "object" ? 1 : 0;
}

export function createAgentService(options = {}) {
  const { store, modelClient, tools } = options;
  if (!store?.saveAgentTask || !store?.appendAgentTaskEvent) throw new Error("Persistent Agent task storage is required");
  if (!modelClient?.planTask || !modelClient?.synthesizeTask) throw new Error("Agent model client is required");
  if (!tools?.execute) throw new Error("Agent domain tools are required");
  const resolveContext = options.resolveContext ?? (async (context) => ({ ...context, active: true }));
  const onAudit = typeof options.onAudit === "function" ? options.onAudit : () => {};
  const idFactory = options.idFactory ?? ((prefix) => `${prefix}-${randomUUID()}`);
  const now = options.now ?? isoNow;
  const running = new Map();
  const controllers = new Map();
  store.markInterruptedAgentTasks?.();

  function event(workspaceId, taskId, type, detail = {}, stepId = null) {
    return store.appendAgentTaskEvent(workspaceId, taskId, { id: idFactory("AGE"), type, stepId, detail, createdAt: now() });
  }

  function save(workspaceId, task, changes) {
    Object.assign(task, changes, { updatedAt: now() });
    store.saveAgentTask(workspaceId, task);
    return task;
  }

  function audit(task, action, detail = {}) {
    try { onAudit({ workspaceId: task.workspaceId, actorUserId: task.createdBy, action, objectType: "agent_task", objectId: task.id, detail }); } catch { /* Audit transport failure must not broaden Agent capability. */ }
  }

  function publicTask(task, includeEvents = true) {
    if (!task) return null;
    const copy = structuredClone(task);
    if (includeEvents) copy.events = store.listAgentTaskEvents(task.workspaceId, task.id);
    return copy;
  }

  async function currentContext(task) {
    const resolved = await resolveContext({ workspaceId: task.workspaceId, userId: task.createdBy, role: task.createdRole });
    if (!resolved || resolved.active === false || String(resolved.workspaceId) !== task.workspaceId || String(resolved.userId) !== task.createdBy) {
      const error = new Error("Agent identity is no longer active in this workspace");
      error.code = "AGENT_IDENTITY_INACTIVE";
      throw error;
    }
    return { workspaceId: task.workspaceId, userId: task.createdBy, role: String(resolved.role) };
  }

  function isCancelled(task) {
    return store.getAgentTask(task.workspaceId, task.id, task.createdBy)?.state === "cancelled";
  }

  async function run(task, request, controller) {
    const receipts = [];
    let planUsage = { totalTokens: 0 };
    let resultUsage = { totalTokens: 0 };
    try {
      const planned = await modelClient.planTask({ request, role: task.createdRole, signal: controller.signal });
      if (isCancelled(task)) return;
      planUsage = planned.usage ?? planUsage;
      const plan = validateAgentPlan(planned.value);
      save(task.workspaceId, task, { state: "running", stageLabel: planned.fallback ? "已采用安全预案，正在执行只读步骤" : "正在执行只读领域步骤", plan, model: planned.model ?? null, planningMode: planned.fallback ? "deterministic_fallback" : "model" });
      event(task.workspaceId, task.id, "plan.ready", { intent: plan.intent, outputType: plan.outputType, stepCount: plan.steps.length, model: planned.model ?? null, fallback: Boolean(planned.fallback), fallbackReason: planned.fallbackReason ?? null });

      for (const step of plan.steps) {
        if (isCancelled(task)) return;
        const context = await currentContext(task);
        event(task.workspaceId, task.id, "step.started", { title: step.title, tool: step.tool }, step.id);
        const output = await tools.execute(step.tool, step.arguments, context);
        const receipt = { stepId: step.id, title: step.title, tool: step.tool, arguments: step.arguments, output };
        receipts.push(receipt);
        event(task.workspaceId, task.id, "step.complete", { title: step.title, tool: step.tool, resultCount: resultCount(output), sourceIds: [...collectSourceIds([receipt])].slice(0, 30) }, step.id);
      }

      if (isCancelled(task)) return;
      save(task.workspaceId, task, { state: "synthesizing", stageLabel: "正在检查结论与原文依据" });
      const synthesized = await modelClient.synthesizeTask({ request, plan, receipts, signal: controller.signal });
      if (isCancelled(task)) return;
      resultUsage = synthesized.usage ?? resultUsage;
      const allowedSourceIds = collectSourceIds(receipts);
      const visibleAssetCount = [...allowedSourceIds].filter((id) => /^IP-/.test(id)).length;
      const result = normalizeAgentResult(synthesized.value, { allowedSourceIds, visibleAssetCount, excludedActions: EXCLUDED_ACTIONS });
      const usage = { planTokens: Number(planUsage.totalTokens ?? 0), resultTokens: Number(resultUsage.totalTokens ?? 0), totalTokens: Number(planUsage.totalTokens ?? 0) + Number(resultUsage.totalTokens ?? 0) };
      const deliveryMode = synthesized.fallback ? "deterministic_receipt_review" : "model";
      save(task.workspaceId, task, { state: result.status, stageLabel: synthesized.fallback ? "服务暂不可用：已生成待人工复核的只读清单" : result.status === "complete" ? "任务结果已完成原文依据检查" : "任务结果需要人工复核", result, usage, deliveryMode, completedAt: now(), error: null });
      event(task.workspaceId, task.id, result.status === "complete" ? "delivery.complete" : "delivery.needs_review", { status: result.status, findingCount: result.findings.length, evidenceCoverage: result.quality.evidenceCoverage, usage, fallback: Boolean(synthesized.fallback), fallbackReason: synthesized.fallbackReason ?? null });
      audit(task, result.status === "complete" ? "agent.task_complete" : "agent.task_needs_review", { intent: plan.intent, stepCount: plan.steps.length, evidenceCoverage: result.quality.evidenceCoverage, totalTokens: usage.totalTokens, deliveryMode });
    } catch (error) {
      if (isCancelled(task) || controller.signal.aborted && store.getAgentTask(task.workspaceId, task.id, task.createdBy)?.state === "cancelled") return;
      const message = safeFailure(error);
      save(task.workspaceId, task, { state: "failed", stageLabel: "任务已安全停止", error: message, completedAt: now() });
      event(task.workspaceId, task.id, "task.failed", { code: String(error?.code || "AGENT_EXECUTION_ERROR"), message });
      audit(task, "agent.task_failed", { code: String(error?.code || "AGENT_EXECUTION_ERROR") });
    }
  }

  return {
    async submit(input, actor) {
      const request = normalizeAgentRequest(input);
      const workspaceId = String(actor.workspaceId);
      const createdBy = String(actor.userId);
      const createdAt = now();
      const task = {
        id: idFactory("AGT"),
        workspaceId,
        createdBy,
        createdRole: String(actor.role),
        prompt: request.prompt,
        templateId: request.templateId,
        assetIds: request.assetIds,
        state: "planning",
        stageLabel: "正在生成受控任务计划",
        createdAt,
        updatedAt: createdAt,
      };
      const policy = request.templateId === "wiki_draft" && !EDIT_ROLES.has(task.createdRole)
        ? { allowed: false, code: "role_required", message: "生成 Wiki 草案需要知识编辑者、空间管理员或空间所有者权限；当前账号仍可执行只读分析任务。" }
        : evaluateAgentRequest(request.prompt);
      if (!policy.allowed) {
        task.state = "blocked";
        task.stageLabel = "请求超出 IP 任务助手边界";
        task.boundary = { code: policy.code, message: policy.message };
        task.completedAt = createdAt;
        store.saveAgentTask(workspaceId, task);
        event(workspaceId, task.id, "policy.blocked", { code: policy.code, message: policy.message });
        audit(task, "agent.task_blocked", { code: policy.code });
        return publicTask(task);
      }
      store.saveAgentTask(workspaceId, task);
      event(workspaceId, task.id, "task.created", { templateId: request.templateId || null, assetCount: request.assetIds.length, boundary: "document-ip-wiki-readonly" });
      audit(task, "agent.task_create", { templateId: request.templateId || null });
      const controller = new AbortController();
      controllers.set(task.id, controller);
      const promise = run(task, request, controller).finally(() => { running.delete(task.id); controllers.delete(task.id); });
      running.set(task.id, promise);
      return publicTask(task);
    },

    get(id, actor) {
      const task = store.getAgentTask(String(actor.workspaceId), String(id), String(actor.userId));
      return publicTask(task);
    },

    list(actor, limit = 30) {
      return store.listAgentTasks(String(actor.workspaceId), String(actor.userId), limit).map((task) => publicTask(task, false));
    },

    cancel(id, actor) {
      const task = store.getAgentTask(String(actor.workspaceId), String(id), String(actor.userId));
      if (!task) return null;
      if (TERMINAL_STATES.has(task.state)) return publicTask(task);
      save(task.workspaceId, task, { state: "cancelled", stageLabel: "任务已由用户取消", completedAt: now(), error: null });
      event(task.workspaceId, task.id, "task.cancelled", { reason: "user_request" });
      controllers.get(task.id)?.abort();
      audit(task, "agent.task_cancel", {});
      return publicTask(task);
    },

    async whenSettled(id, actor) {
      await running.get(String(id));
      return this.get(id, actor);
    },
  };
}
