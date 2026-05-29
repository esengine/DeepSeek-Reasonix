import { Script, createContext } from "node:vm";
import { parseWorkflowScript } from "./parser.js";
import type {
  WorkflowAgentOptions,
  WorkflowAgentResult,
  WorkflowAgentRunner,
  WorkflowRunOptions,
  WorkflowRunResult,
} from "./types.js";

interface RuntimeState {
  currentPhase?: string;
  logs: string[];
  phases: string[];
  agentCount: number;
  plannedAgents: Array<{ label: string; phase?: string; promptPreview: string }>;
  spent: number;
}

interface AgentInputOptions {
  name?: unknown;
  instruction?: unknown;
  prompt?: unknown;
  task?: unknown;
  label?: unknown;
  phase?: unknown;
  type?: unknown;
  model?: unknown;
  allowedTools?: unknown;
}

const DEFAULT_CONCURRENCY = 3;
const HARD_CONCURRENCY_CAP = 16;
const DEFAULT_MAX_AGENTS = 8;
const HARD_MAX_AGENTS = 32;
const SCRIPT_TIMEOUT_MS = 10_000;

export async function runWorkflow<T = unknown>(
  script: string,
  options: WorkflowRunOptions = {},
): Promise<WorkflowRunResult<T>> {
  const startedAt = Date.now();
  const parsed = parseWorkflowScript(script);
  const state: RuntimeState = {
    logs: [],
    phases: [],
    agentCount: 0,
    plannedAgents: [],
    spent: 0,
  };

  if (options.mode === "validate_only") {
    return {
      success: true,
      meta: parsed.meta,
      logs: [],
      phases: parsed.meta.phases?.map((phase) => phase.title) ?? [],
      agentCount: 0,
      durationMs: Date.now() - startedAt,
    };
  }

  const maxAgents = clampPositiveInt(options.maxAgents, DEFAULT_MAX_AGENTS, HARD_MAX_AGENTS);
  const concurrency = clampPositiveInt(
    options.concurrency,
    DEFAULT_CONCURRENCY,
    HARD_CONCURRENCY_CAP,
  );
  const limiter = createLimiter(concurrency);
  const runner =
    options.mode === "dry_run" ? dryRunRunner(state) : (options.runner ?? missingRunner());

  const throwIfAborted = (): void => {
    if (options.signal?.aborted) throw new Error("workflow aborted");
  };

  const log = (message: unknown): void => {
    const text = String(message);
    state.logs.push(text);
    options.onLog?.(text);
  };

  const phase = (title: unknown): void => {
    const text = String(title).trim();
    if (!text) throw new Error("phase() requires a non-empty title");
    state.currentPhase = text;
    if (!state.phases.includes(text)) state.phases.push(text);
    options.onPhase?.(text);
  };

  const agent = async (
    promptInput: unknown,
    agentOptions: AgentInputOptions = {},
  ): Promise<string | null> => {
    throwIfAborted();
    if (state.agentCount >= maxAgents) {
      throw new Error(`workflow maxAgents limit reached (${maxAgents})`);
    }
    const input = normalizeAgentInput(promptInput, agentOptions);
    const prompt = input.prompt;
    state.agentCount++;
    const phaseName = stringOption(input.options.phase) ?? state.currentPhase;
    const label =
      stringOption(input.options.label) ??
      stringOption(input.options.name) ??
      defaultAgentLabel(phaseName, state.agentCount);
    const opts: WorkflowAgentOptions = {
      workflowName: parsed.meta.name,
      label,
      signal: options.signal,
    };
    if (phaseName) opts.phase = phaseName;
    const type = workflowAgentType(input.options.type);
    if (type) opts.type = type;
    const model = stringOption(input.options.model);
    if (model) opts.model = model;
    const allowedTools = stringArrayOption(input.options.allowedTools);
    if (allowedTools) opts.allowedTools = allowedTools;

    if (options.mode === "dry_run") {
      state.plannedAgents.push({
        label,
        phase: phaseName,
        promptPreview: prompt.length > 80 ? `${prompt.slice(0, 79)}…` : prompt,
      });
    }

    return limiter(async () => {
      options.onAgentStart?.({ label, phase: phaseName, prompt });
      try {
        throwIfAborted();
        const result = await runner.run(prompt, opts);
        throwIfAborted();
        state.spent += estimateTokens(result.output);
        options.onAgentEnd?.({ label, phase: phaseName, result });
        if (!result.ok) {
          log(`agent ${label} failed: ${result.error ?? "unknown error"}`);
          return null;
        }
        return result.output;
      } catch (error) {
        if (options.signal?.aborted) throw error;
        const message = error instanceof Error ? error.message : String(error);
        log(`agent ${label} failed: ${message}`);
        options.onAgentEnd?.({ label, phase: phaseName, result: null });
        return null;
      }
    });
  };

  const parallel = async (thunks: unknown): Promise<unknown[]> => {
    throwIfAborted();
    if (!Array.isArray(thunks) || thunks.some((thunk) => typeof thunk !== "function")) {
      throw new TypeError(
        "parallel() expects an array of functions, not promises. Wrap each call: () => agent(...)",
      );
    }
    return Promise.all(
      thunks.map(async (thunk, index) => {
        try {
          return await (thunk as () => unknown)();
        } catch (error) {
          if (options.signal?.aborted) throw error;
          log(`parallel[${index}] failed: ${errorMessage(error)}`);
          return null;
        }
      }),
    );
  };

  const pipeline = async (
    items: unknown,
    ...stages: Array<(prev: unknown, original: unknown, index: number) => unknown>
  ): Promise<unknown[]> => {
    throwIfAborted();
    if (!Array.isArray(items))
      throw new TypeError("pipeline() expects an array as the first argument");
    if (stages.some((stage) => typeof stage !== "function")) {
      throw new TypeError("pipeline() stages must be functions");
    }
    return Promise.all(
      items.map(async (item, index) => {
        let value: unknown = item;
        for (const stage of stages) {
          try {
            throwIfAborted();
            value = await stage(value, item, index);
            throwIfAborted();
          } catch (error) {
            if (options.signal?.aborted) throw error;
            log(`pipeline[${index}] failed: ${errorMessage(error)}`);
            return null;
          }
        }
        return value;
      }),
    );
  };

  const verifyFindings = async (findings: unknown): Promise<unknown[]> => {
    if (!Array.isArray(findings)) {
      throw new TypeError("verifyFindings() expects an array of findings");
    }
    return parallel(
      findings.map((finding, index) => {
        const item = normalizeFinding(finding, index);
        return async () => ({
          ...item,
          verification: await agent({
            label: `verify-${item.id}`,
            type: "verify",
            instruction: `Verify this finding independently. Return concise evidence-backed judgment.\n\nFinding: ${item.finding}\nEvidence: ${item.evidence ?? "not provided"}`,
          }),
        });
      }),
    );
  };

  const adversarialReview = async (
    subject: unknown,
    opts: AgentInputOptions = {},
  ): Promise<string | null> =>
    agent({
      label: stringOption(opts.label) ?? "adversarial-review",
      type: "verify",
      instruction: `Try to refute, falsify, or weaken the following workflow result. Return only concrete objections with evidence.\n\n${String(subject)}`,
    });

  const synthesize = async (
    inputs: unknown,
    opts: AgentInputOptions = {},
  ): Promise<string | null> =>
    agent({
      label: stringOption(opts.label) ?? "synthesis",
      type: "synthesis",
      instruction: `Synthesize these workflow results into one final answer. Preserve uncertainty and cite which inputs support each conclusion.\n\n${JSON.stringify(inputs)}`,
    });

  const budget = Object.freeze({
    total: options.tokenBudget ?? null,
    spent: () => state.spent,
    remaining: () =>
      options.tokenBudget == null
        ? Number.POSITIVE_INFINITY
        : Math.max(0, options.tokenBudget - state.spent),
  });

  try {
    const context = createContext(
      {
        agent,
        parallel,
        pipeline,
        verifyFindings,
        adversarialReview,
        synthesize,
        phase,
        log,
        args: options.args,
        cwd: options.cwd ?? process.cwd(),
        budget,
        console: {
          log,
          info: log,
          warn: (message: unknown) => log(`[warn] ${String(message)}`),
          error: (message: unknown) => log(`[error] ${String(message)}`),
        },
        JSON,
        Math: safeMath(),
        Array,
        Object,
        String,
        Number,
        Boolean,
        Set,
        Map,
        Promise,
      },
      { codeGeneration: { strings: false, wasm: false } },
    );
    const wrapped = `(async () => {\n${parsed.body}\n})()`;
    const result = await new Script(wrapped, {
      filename: `${parsed.meta.name || "workflow"}.js`,
    }).runInContext(context, { timeout: SCRIPT_TIMEOUT_MS });

    return {
      success: true,
      meta: parsed.meta,
      result: result as T,
      logs: state.logs,
      phases: state.phases,
      agentCount: state.agentCount,
      plannedAgents: state.plannedAgents.length ? state.plannedAgents : undefined,
      durationMs: Date.now() - startedAt,
    };
  } catch (error) {
    return {
      success: false,
      meta: parsed.meta,
      error: errorMessage(error),
      logs: state.logs,
      phases: state.phases,
      agentCount: state.agentCount,
      plannedAgents: state.plannedAgents.length ? state.plannedAgents : undefined,
      durationMs: Date.now() - startedAt,
    };
  }
}

function dryRunRunner(state: RuntimeState): WorkflowAgentRunner {
  return {
    async run(_prompt, opts) {
      const label = opts.label ?? defaultAgentLabel(opts.phase, state.agentCount);
      return { ok: true, output: `dry-run:${label}` };
    },
  };
}

function missingRunner(): WorkflowAgentRunner {
  return {
    async run() {
      throw new Error("workflow runner is required for mode=run");
    },
  };
}

function normalizeAgentInput(
  promptInput: unknown,
  agentOptions: AgentInputOptions,
): { prompt: string; options: AgentInputOptions } {
  if (!isPlainRecord(promptInput)) return { prompt: String(promptInput), options: agentOptions };
  const objectOptions = promptInput as AgentInputOptions;
  const prompt =
    stringOption(objectOptions.instruction) ??
    stringOption(objectOptions.prompt) ??
    stringOption(objectOptions.task);
  if (!prompt) {
    throw new TypeError("agent() object input requires instruction, prompt, or task string");
  }
  return { prompt, options: { ...objectOptions, ...agentOptions } };
}

function isPlainRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function createLimiter(limit: number): <T>(fn: () => Promise<T>) => Promise<T> {
  let active = 0;
  const queue: Array<() => void> = [];
  const next = (): void => {
    active--;
    queue.shift()?.();
  };
  return async (fn) => {
    if (active >= limit) await new Promise<void>((resolve) => queue.push(resolve));
    active++;
    try {
      return await fn();
    } finally {
      next();
    }
  };
}

function clampPositiveInt(value: number | undefined, fallback: number, cap: number): number {
  if (typeof value !== "number" || !Number.isFinite(value) || value < 1) return fallback;
  return Math.min(Math.floor(value), cap);
}

function defaultAgentLabel(phase: string | undefined, index: number): string {
  return phase ? `${phase} agent ${index}` : `agent ${index}`;
}

function stringOption(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function stringArrayOption(value: unknown): readonly string[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const items = value
    .filter((item): item is string => typeof item === "string")
    .map((item) => item.trim())
    .filter(Boolean);
  return items.length ? items : undefined;
}

function normalizeFinding(
  value: unknown,
  index: number,
): { id: string; finding: string; evidence?: string } {
  if (typeof value === "string") return { id: String(index + 1), finding: value };
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new TypeError(`finding[${index}] must be a string or object`);
  }
  const record = value as Record<string, unknown>;
  const finding = typeof record.finding === "string" ? record.finding.trim() : "";
  if (!finding) throw new TypeError(`finding[${index}].finding must be a non-empty string`);
  const id =
    typeof record.id === "string" && record.id.trim() ? record.id.trim() : String(index + 1);
  const evidence =
    typeof record.evidence === "string" && record.evidence.trim()
      ? record.evidence.trim()
      : undefined;
  return evidence ? { id, finding, evidence } : { id, finding };
}

function workflowAgentType(value: unknown): WorkflowAgentOptions["type"] | undefined {
  return value === "explore" || value === "verify" || value === "synthesis" ? value : undefined;
}

function estimateTokens(value: unknown): number {
  return Math.ceil(JSON.stringify(value ?? "").length / 4);
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function safeMath(): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const key of Object.getOwnPropertyNames(Math)) {
    if (key === "random") continue;
    out[key] = (Math as unknown as Record<string, unknown>)[key];
  }
  return Object.freeze(out);
}
