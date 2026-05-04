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

/** String return short-circuits dispatch; null/undefined falls through to the tool fn. */
export type ToolInterceptor = (
  name: string,
  args: Record<string, unknown>,
) => string | null | undefined | Promise<string | null | undefined>;

export class ToolRegistry {
  private readonly _tools = new Map<string, InternalTool>();
  private readonly _autoFlatten: boolean;
  private _planMode = false;
  private _interceptor: ToolInterceptor | null = null;

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
    let args: Record<string, unknown>;
    try {
      args =
        typeof argumentsRaw === "string"
          ? argumentsRaw.trim()
            ? (JSON.parse(argumentsRaw) ?? {})
            : {}
          : (argumentsRaw ?? {});
    } catch (err) {
      return JSON.stringify({
        error: `invalid tool arguments JSON: ${(err as Error).message}`,
      });
    }

    // Re-nest dot-notation args back to the original shape, but only when
    // (a) we flattened this tool's schema, AND
    // (b) the incoming args actually use dot keys.
    // The second condition handles the case where a model ignores the flat
    // spec and emits nested args anyway — we shouldn't double-process them.
    if (tool.flatSchema && args && typeof args === "object" && hasDotKey(args)) {
      args = nestArguments(args);
    }

    // Plan-mode enforcement — runs AFTER arg parsing so a tool with a
    // runtime `readOnlyCheck` can inspect the actual args (e.g.
    // `run_command` is read-only iff the command matches its allowlist).
    if (this._planMode && !isReadOnlyCall(tool, args)) {
      return JSON.stringify({
        error: `${name}: unavailable in plan mode — this is a read-only exploration phase. Use read_file / list_directory / search_files / directory_tree / web_search / allowlisted shell commands to investigate. Call submit_plan with your proposed plan when you're ready for the user's review.`,
        rejectedReason: "plan-mode",
      });
    }

    // Interceptor runs after plan-mode (so a plan-mode refusal still
    // wins) but before the real tool fn. A string return is treated as
    // the full tool result; null / undefined means "not my concern,
    // fall through." Uncaught throws from the interceptor are surfaced
    // through the same error path as a failed tool fn below.
    if (this._interceptor) {
      try {
        const short = await this._interceptor(name, args);
        if (typeof short === "string") return short;
      } catch (err) {
        return JSON.stringify({
          error: `${name}: interceptor failed — ${(err as Error).message}`,
        });
      }
    }

    try {
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
      return clipped;
    } catch (err) {
      const e = err as Error & { toToolResult?: () => unknown };
      // Errors may opt into a richer tool-result shape by implementing
      // `toToolResult()`. Used by `PlanProposedError` to smuggle the
      // submitted plan text out to the UI without stuffing it into the
      // error message (which the dispatcher truncates at no fixed limit,
      // but keeping payloads structured is cleaner for UI parsing).
      if (typeof e.toToolResult === "function") {
        try {
          return JSON.stringify(e.toToolResult());
        } catch {
          /* fall through to the default shape */
        }
      }
      return JSON.stringify({
        error: `${e.name}: ${e.message}`,
      });
    }
  }
}

function isReadOnlyCall(tool: InternalTool, args: Record<string, unknown>): boolean {
  if (tool.readOnlyCheck) {
    try {
      return Boolean(tool.readOnlyCheck(args as never));
    } catch {
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
