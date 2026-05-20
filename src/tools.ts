import type { PauseGate } from "./core/pause-gate.js";
import { truncateForModel, truncateForModelByTokens } from "./mcp/registry.js";
import { analyzeSchema, flattenSchema, nestArguments } from "./repair/flatten.js";
import type { JSONSchema, ToolSpec } from "./types.js";

export interface ToolCallContext {
  signal?: AbortSignal;
  /** Inject a mock PauseGate for tests. When absent, tools use the singleton. */
  confirmationGate?: PauseGate;
}

export interface ToolDefinition<A = any, R = any> {
  name: string;
  description?: string;
  parameters?: JSONSchema;
  /** Safe in plan mode — registry refuses non-readonly calls when `planMode` is on. */
  readOnly?: boolean;
  /** Per-args check; takes precedence over `readOnly`. e.g. `run_command` + allowlisted argv. */
  readOnlyCheck?: (args: A) => boolean;
  /** Safe to dispatch concurrently with other parallel-safe calls in the same turn. Default false — opt-in only. */
  parallelSafe?: boolean;
  /** Excluded from repeat-loop storm accounting; use only for cheap, state-inspection tools. */
  stormExempt?: boolean;
  fn: (args: A, ctx?: ToolCallContext) => R | Promise<R>;
}

interface InternalTool extends ToolDefinition {
  /** Set when schema is deep (>2 levels) or wide (>10 leaves) — DeepSeek V3/R1 drop args otherwise. */
  flatSchema?: JSONSchema;
}

export interface ToolRegistryOptions {
  /** Auto-flatten + re-nest at dispatch; default true. */
  autoFlatten?: boolean;
}

export type ToolCallAuditEvent = {
  name: string;
  args: Record<string, unknown>;
};

export type ToolCallAuditListener = (event: ToolCallAuditEvent) => void;

/** String return short-circuits dispatch; null/undefined falls through to the tool fn. */
export type ToolInterceptor = (
  name: string,
  args: Record<string, unknown>,
) => string | null | undefined | Promise<string | null | undefined>;

/** Final-stage post-processor — runs on every dispatch return (success and error paths) so callers can append context like a remaining-budget hint. Whatever it returns becomes the dispatch result. */
export type ToolResultAugmenter = (
  name: string,
  args: Record<string, unknown>,
  result: string,
) => string;

export class ToolRegistry {
  private readonly _tools = new Map<string, InternalTool>();
  private readonly _autoFlatten: boolean;
  private _planMode = false;
  private _interceptor: ToolInterceptor | null = null;
  private readonly _interceptors: Array<{ id: string; fn: ToolInterceptor }> = [];
  private _auditListener: ToolCallAuditListener | null = null;
  private _resultAugmenter: ToolResultAugmenter | null = null;
  /** Per-tool fingerprint of the last call that failed schema validation. Cleared by any successful validation for that tool. */
  private readonly _lastMalformed = new Map<string, string>();
  /** Per-tool fingerprint of the last host-side gate rejection. */
  private readonly _lastGateRejection = new Map<string, string>();

  constructor(opts: ToolRegistryOptions = {}) {
    this._autoFlatten = opts.autoFlatten !== false;
  }

  /** Enable / disable plan-mode enforcement at dispatch. */
  setPlanMode(on: boolean): void {
    this._planMode = Boolean(on);
  }

  /** True when the registry is currently refusing non-readonly calls. */
  get planMode(): boolean {
    return this._planMode;
  }

  /** At most one interceptor active; calling twice replaces. */
  setToolInterceptor(fn: ToolInterceptor | null): void {
    this._interceptor = fn;
  }

  /** Ordered host-side interceptors. They run before the legacy single interceptor. */
  addToolInterceptor(id: string, fn: ToolInterceptor): () => void {
    const normalized = id.trim();
    if (!normalized) throw new Error("tool interceptor requires a non-empty id");
    const existing = this._interceptors.findIndex((entry) => entry.id === normalized);
    if (existing >= 0) this._interceptors.splice(existing, 1);
    this._interceptors.push({ id: normalized, fn });
    return () => {
      const idx = this._interceptors.findIndex((entry) => entry.id === normalized);
      if (idx >= 0) this._interceptors.splice(idx, 1);
    };
  }

  setAuditListener(fn: ToolCallAuditListener | null): void {
    this._auditListener = fn;
  }

  /** Final-stage post-processor; replaces previous augmenter when called twice. Pass null to clear. */
  setResultAugmenter(fn: ToolResultAugmenter | null): void {
    this._resultAugmenter = fn;
  }

  /** True when an augmenter is already wired — lets late-installing callers skip clobbering an earlier one. */
  get hasResultAugmenter(): boolean {
    return this._resultAugmenter !== null;
  }

  register<A, R>(def: ToolDefinition<A, R>): this {
    if (!def.name) throw new Error("tool requires a name");
    const internal: InternalTool = { ...(def as ToolDefinition) };
    if (this._autoFlatten && def.parameters) {
      const decision = analyzeSchema(def.parameters);
      if (decision.shouldFlatten) {
        internal.flatSchema = flattenSchema(def.parameters);
      }
    }
    this._tools.set(def.name, internal);
    return this;
  }

  /** Drop a registered tool. Returns true if the name was present. Used by MCP hot-unbridge. */
  unregister(name: string): boolean {
    return this._tools.delete(name);
  }

  has(name: string): boolean {
    return this._tools.has(name);
  }

  get(name: string): ToolDefinition | undefined {
    return this._tools.get(name);
  }

  get size(): number {
    return this._tools.size;
  }

  /** True if a registered tool's schema was flattened for the model. */
  wasFlattened(name: string): boolean {
    return Boolean(this._tools.get(name)?.flatSchema);
  }

  /** Unknown / unannotated tools default to false — third-party MCP tools must opt in. */
  isParallelSafe(name: string): boolean {
    return this._tools.get(name)?.parallelSafe === true;
  }

  specs(): ToolSpec[] {
    return [...this._tools.values()].map((t) => ({
      type: "function",
      function: {
        name: t.name,
        description: t.description ?? "",
        parameters: t.flatSchema ?? t.parameters ?? { type: "object", properties: {} },
      },
    }));
  }

  async dispatch(
    name: string,
    argumentsRaw: string | Record<string, unknown>,
    opts: {
      signal?: AbortSignal;
      maxResultChars?: number;
      maxResultTokens?: number;
      /** Inject a mock PauseGate for tests. */
      confirmationGate?: PauseGate;
    } = {},
  ): Promise<string> {
    const tool = this._tools.get(name);
    if (!tool) {
      return JSON.stringify({ error: `unknown tool: ${name}` });
    }
    const rawFingerprint = rawFingerprintArgs(argumentsRaw);
    let args: Record<string, unknown>;
    try {
      args =
        typeof argumentsRaw === "string"
          ? argumentsRaw.trim()
            ? (JSON.parse(argumentsRaw) ?? {})
            : {}
          : (argumentsRaw ?? {});
    } catch (err) {
      return this._noteMalformed(
        name,
        rawFingerprint,
        `invalid tool arguments JSON: ${(err as Error).message}`,
      );
    }

    // Re-nest dot-notation args back to the original shape, but only when
    // (a) we flattened this tool's schema, AND
    // (b) the incoming args actually use dot keys.
    // The second condition handles the case where a model ignores the flat
    // spec and emits nested args anyway — we shouldn't double-process them.
    if (tool.flatSchema && args && typeof args === "object" && hasDotKey(args)) {
      args = nestArguments(args);
    }
    const fingerprint = fingerprintArgs(args);

    const missing = tool.parameters ? missingRequiredParam(tool.parameters, args) : null;
    if (missing) {
      return this._noteMalformed(
        name,
        fingerprint,
        `missing required parameter "${missing}". Retry with all required parameters filled.`,
      );
    }
    // Validation passed — this tool's malformed-args streak is broken.
    this._lastMalformed.delete(name);

    // Plan-mode enforcement — runs AFTER arg parsing so a tool with a
    // runtime `readOnlyCheck` can inspect the actual args (e.g.
    // `run_command` is read-only iff the command matches its allowlist).
    if (this._planMode && !isReadOnlyCall(tool, args)) {
      return JSON.stringify({
        error: `${name}: unavailable in plan mode — this is a read-only exploration phase. Use read_file / list_directory / search_files / directory_tree / web_search / allowlisted shell commands to investigate. Call submit_plan with your proposed plan when you're ready for the user's review.`,
        rejectedReason: "plan-mode",
      });
    }

    // Interceptors run after plan-mode (so a plan-mode refusal still
    // wins) but before the real tool fn. A string return is treated as
    // the full tool result; null / undefined means "not my concern,
    // fall through." Uncaught throws are surfaced through the same
    // structured error path as the legacy single interceptor.
    const chain = this._interceptor
      ? [...this._interceptors.map((entry) => entry.fn), this._interceptor]
      : this._interceptors.map((entry) => entry.fn);
    for (const interceptor of chain) {
      try {
        const short = await interceptor(name, args);
        if (typeof short === "string") {
          const guarded = this._noteGateRejection(name, fingerprint, short);
          return this._augmentResult(name, args, guarded);
        }
      } catch (err) {
        return JSON.stringify({
          error: `${name}: interceptor failed — ${(err as Error).message}`,
        });
      }
    }

    // Pre-dispatch abort gate: if ESC fired while this tool was queued,
    // refuse to start it. Tools that already check `ctx.signal` mid-run
    // still own their own interrupt path; this just stops a queue of
    // pending calls from running to completion after the user gave up.
    if (opts.signal?.aborted) {
      return JSON.stringify({
        error: `${name}: aborted before dispatch (user interrupt)`,
        rejectedReason: "aborted",
      });
    }

    let finalResult: string;
    try {
      try {
        this._auditListener?.({ name, args });
      } catch {
        /* audit path must never break tool execution */
      }
      const result = await tool.fn(args, {
        signal: opts.signal,
        confirmationGate: opts.confirmationGate,
      });
      const str = typeof result === "string" ? result : JSON.stringify(result);
      // Pre-clip at dispatch so a single fat result can't balloon the
      // log (and disk session file) on its way in. Healing at load time
      // still catches pre-existing oversize entries; this closes the
      // door on new ones.
      //
      // Two caps available: `maxResultTokens` (preferred — bounds the
      // real context footprint, so CJK doesn't slip past at 2× density)
      // and `maxResultChars` (legacy). If both are set, apply both and
      // the tighter one wins; char-only callers keep their old behavior.
      let clipped = str;
      if (opts.maxResultTokens !== undefined) {
        clipped = truncateForModelByTokens(clipped, opts.maxResultTokens);
      }
      if (opts.maxResultChars !== undefined) {
        clipped = truncateForModel(clipped, opts.maxResultChars);
      }
      finalResult = clipped;
    } catch (err) {
      const e = err as Error & { toToolResult?: () => unknown };
      // Errors may opt into a richer tool-result shape by implementing
      // `toToolResult()`. Used by `PlanProposedError` to smuggle the
      // submitted plan text out to the UI without stuffing it into the
      // error message (which the dispatcher truncates at no fixed limit,
      // but keeping payloads structured is cleaner for UI parsing).
      if (typeof e.toToolResult === "function") {
        try {
          finalResult = JSON.stringify(e.toToolResult());
        } catch {
          finalResult = JSON.stringify({ error: `${e.name}: ${e.message}` });
        }
      } else {
        finalResult = JSON.stringify({ error: `${e.name}: ${e.message}` });
      }
    }

    finalResult = this._noteGateRejection(name, fingerprint, finalResult);
    return this._augmentResult(name, args, finalResult);
  }

  private _augmentResult(name: string, args: Record<string, unknown>, result: string): string {
    if (this._resultAugmenter) {
      try {
        return this._resultAugmenter(name, args, result);
      } catch {
        /* augmenter must never break the tool result */
      }
    }
    return result;
  }

  /** Records the failed call's fingerprint; on the 2nd consecutive identical malformed call to the same tool, returns a sharper error that tells the model to stop retrying. */
  private _noteMalformed(name: string, fingerprint: string, detail: string): string {
    const prev = this._lastMalformed.get(name);
    this._lastMalformed.set(name, fingerprint);
    if (prev === fingerprint) {
      return JSON.stringify({
        error: `${name}: same call just failed validation (${detail}) — DO NOT retry with identical args. Either fix the call (read the schema in the tool spec) or pick a different tool.`,
        consecutiveMalformed: true,
      });
    }
    return JSON.stringify({ error: `${name}: ${detail}` });
  }

  private _noteGateRejection(name: string, fingerprint: string, result: string): string {
    const reason = rejectedReason(name, result);
    if (!reason) {
      this._lastGateRejection.delete(name);
      return result;
    }
    const key = `${reason}:${fingerprint}`;
    const prev = this._lastGateRejection.get(name);
    this._lastGateRejection.set(name, key);
    if (prev === key) {
      return JSON.stringify({
        error: `${name}: same call was just rejected by ${reason} — do not retry identical args. ${rejectionRecoveryHint(reason)}`,
        rejectedReason: reason,
        consecutiveInterceptorRejection: true,
      });
    }
    return result;
  }
}

function rejectedReason(name: string, result: string): string | null {
  const textReason = plainTextRejectedReason(name, result);
  if (textReason) return textReason;
  try {
    const parsed = JSON.parse(result) as unknown;
    if (!parsed || typeof parsed !== "object") return null;
    const reason = (parsed as { rejectedReason?: unknown }).rejectedReason;
    if (typeof reason === "string" && reason) return reason;
    const error = (parsed as { error?: unknown }).error;
    if (typeof error === "string") return plainTextRejectedReason(name, error);
    return null;
  } catch {
    return null;
  }
}

function plainTextRejectedReason(name: string, result: string): string | null {
  if ((name === "edit_file" || name === "write_file") && /rejected this edit/i.test(result)) {
    return "edit-gate";
  }
  if ((name === "run_command" || name === "run_background") && /\buser denied:/i.test(result)) {
    return "shell-gate";
  }
  return null;
}

function rejectionRecoveryHint(reason: string): string {
  switch (reason) {
    case "edit-gate":
      return "Do not re-emit the same edit. Try a genuinely different edit or ask the user how to proceed.";
    case "shell-gate":
      return "Do not retry the same command. Use an allowlisted/read-only command, wait for approval, or ask the user how to proceed.";
    case "engineering-lifecycle":
      return "Switch to read-only exploration, submit or revise the plan, or choose a different tool call.";
    case "engineering-lifecycle-evidence":
      return "Submit completion evidence or revise/checkpoint the plan before marking the step complete.";
    default:
      return "Choose a different tool call or ask the user how to proceed.";
  }
}

function isReadOnlyCall(tool: InternalTool, args: Record<string, unknown>): boolean {
  if (tool.readOnlyCheck) {
    try {
      return Boolean(tool.readOnlyCheck(args as never));
    } catch (err) {
      // A buggy readOnlyCheck silently downgrades to "may mutate" — log it so
      // the bug doesn't hide behind plan-mode refusals or storm-breaker noise.
      process.stderr.write(`readOnlyCheck for ${tool.name} threw: ${(err as Error).message}\n`);
      return false;
    }
  }
  return tool.readOnly === true;
}

function hasDotKey(obj: Record<string, unknown>): boolean {
  for (const k of Object.keys(obj)) {
    if (k.includes(".")) return true;
  }
  return false;
}

/** Raw key for invalid JSON, where there is no parsed argument object to normalize. */
function rawFingerprintArgs(argumentsRaw: string | Record<string, unknown>): string {
  if (typeof argumentsRaw === "string") return argumentsRaw;
  return fingerprintArgs(argumentsRaw);
}

/** Stable per-call key for parsed tool args; object key order should not affect repeat detection. */
function fingerprintArgs(args: Record<string, unknown>): string {
  try {
    return JSON.stringify(sortJson(args));
  } catch {
    return "";
  }
}

function sortJson(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortJson);
  if (!value || typeof value !== "object") return value;
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(value).sort()) {
    const item = (value as Record<string, unknown>)[key];
    if (item !== undefined) out[key] = sortJson(item);
  }
  return out;
}

/** If the schema declares required params, return the first one that's missing. */
function missingRequiredParam(schema: JSONSchema, args: Record<string, unknown>): string | null {
  const required = schema.required;
  if (!required || required.length === 0) return null;
  for (const key of required) {
    if (args[key] === undefined) return key;
  }
  return null;
}
