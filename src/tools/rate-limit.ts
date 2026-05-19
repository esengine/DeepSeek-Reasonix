/** Sliding-window limits for per-session tool dispatch, with aggregate and per-tool buckets. */

export interface ToolRateLimitBucketConfig {
  maxCalls?: number;
  windowSeconds?: number;
}

export interface ToolRateLimitConfig {
  enabled?: boolean;
  aggregate?: ToolRateLimitBucketConfig;
  tools?: Record<string, false | ToolRateLimitBucketConfig>;
}

export interface NormalizedToolRateLimitBucket {
  maxCalls: number;
  windowSeconds: number;
}

export interface NormalizedToolRateLimitConfig {
  aggregate: NormalizedToolRateLimitBucket;
  tools: Record<string, false | NormalizedToolRateLimitBucket>;
}

export interface RateLimitedToolResult {
  error: "rate_limited";
  tool: string;
  scope: string;
  limit: number;
  windowSeconds: number;
  retryAfterMs: number;
  message: string;
}

export type ToolRateLimitDecision =
  | { allowed: true }
  | { allowed: false; result: RateLimitedToolResult };

// Generous defaults make this a host-pressure guard, not a normal workload budget.
export const DEFAULT_TOOL_RATE_LIMIT: NormalizedToolRateLimitConfig = {
  aggregate: { maxCalls: 200, windowSeconds: 60 },
  tools: {
    run_command: { maxCalls: 60, windowSeconds: 60 },
    run_background: { maxCalls: 10, windowSeconds: 60 },
  },
};

export type ToolRateLimitOption = false | ToolRateLimitConfig;

type Clock = () => number;

export class ToolRateLimiter {
  private readonly config: false | NormalizedToolRateLimitConfig;
  private readonly clock: Clock;
  private readonly aggregate: number[] = [];
  private readonly tools = new Map<string, number[]>();

  constructor(config: ToolRateLimitOption | undefined = {}, clock: Clock = () => Date.now()) {
    this.config = normalizeToolRateLimitConfig(config);
    this.clock = clock;
  }

  get policy(): false | NormalizedToolRateLimitConfig {
    return this.config;
  }

  consume(tool: string): ToolRateLimitDecision {
    if (this.config === false) return { allowed: true };

    const now = this.clock();
    const toolBucket = this.config.tools[tool];
    if (toolBucket !== false && toolBucket !== undefined) {
      const timestamps = this.timestampsFor(tool);
      const blocked = inspectBucket(tool, timestamps, toolBucket, now);
      if (blocked) return { allowed: false, result: blocked };
    }

    const aggregateBlocked = inspectBucket(
      tool,
      this.aggregate,
      this.config.aggregate,
      now,
      "all_tools",
    );
    if (aggregateBlocked) return { allowed: false, result: aggregateBlocked };

    // Count at dispatch start so concurrent slow tools still occupy the active window.
    this.aggregate.push(now);
    if (toolBucket !== false && toolBucket !== undefined) this.timestampsFor(tool).push(now);
    return { allowed: true };
  }

  private timestampsFor(tool: string): number[] {
    const existing = this.tools.get(tool);
    if (existing) return existing;
    const created: number[] = [];
    this.tools.set(tool, created);
    return created;
  }
}

export function normalizeToolRateLimitConfig(
  config: ToolRateLimitOption | undefined,
): false | NormalizedToolRateLimitConfig {
  if (config === false || config?.enabled === false) return false;
  const aggregate = normalizeBucket(config?.aggregate, DEFAULT_TOOL_RATE_LIMIT.aggregate);
  const tools: Record<string, false | NormalizedToolRateLimitBucket> = {
    ...DEFAULT_TOOL_RATE_LIMIT.tools,
  };
  for (const [name, value] of Object.entries(config?.tools ?? {})) {
    if (value === false) {
      tools[name] = false;
      continue;
    }
    const fallback = DEFAULT_TOOL_RATE_LIMIT.tools[name];
    tools[name] = normalizeBucket(
      value,
      fallback === false || fallback === undefined ? DEFAULT_TOOL_RATE_LIMIT.aggregate : fallback,
    );
  }
  return { aggregate, tools };
}

export function parseRateLimitedToolResult(result: string): RateLimitedToolResult | null {
  try {
    const parsed = JSON.parse(result) as unknown;
    if (!parsed || typeof parsed !== "object") return null;
    const value = parsed as Partial<RateLimitedToolResult>;
    if (value.error !== "rate_limited") return null;
    if (typeof value.tool !== "string" || typeof value.scope !== "string") return null;
    if (typeof value.limit !== "number" || typeof value.windowSeconds !== "number") return null;
    if (typeof value.retryAfterMs !== "number" || typeof value.message !== "string") return null;
    return value as RateLimitedToolResult;
  } catch {
    return null;
  }
}

function normalizeBucket(
  raw: ToolRateLimitBucketConfig | undefined,
  fallback: NormalizedToolRateLimitBucket,
): NormalizedToolRateLimitBucket {
  return {
    maxCalls: positiveInteger(raw?.maxCalls) ?? fallback.maxCalls,
    windowSeconds: positiveInteger(raw?.windowSeconds) ?? fallback.windowSeconds,
  };
}

function positiveInteger(value: unknown): number | undefined {
  return typeof value === "number" && Number.isInteger(value) && value > 0 ? value : undefined;
}

function inspectBucket(
  tool: string,
  timestamps: number[],
  bucket: NormalizedToolRateLimitBucket,
  now: number,
  scope = tool,
): RateLimitedToolResult | null {
  const windowMs = bucket.windowSeconds * 1_000;
  while (timestamps.length > 0 && now - timestamps[0]! >= windowMs) timestamps.shift();
  if (timestamps.length < bucket.maxCalls) return null;
  const retryAfterMs = Math.max(0, timestamps[0]! + windowMs - now);
  return {
    error: "rate_limited",
    tool,
    scope,
    limit: bucket.maxCalls,
    windowSeconds: bucket.windowSeconds,
    retryAfterMs,
    message: `${scope} rate-limited: ${bucket.maxCalls} calls / ${bucket.windowSeconds}s. Wait ${formatWait(retryAfterMs)} or summarize what you know.`,
  };
}

function formatWait(ms: number): string {
  const seconds = ms / 1_000;
  return `${Number.isInteger(seconds) ? seconds.toFixed(0) : seconds.toFixed(1)}s`;
}
