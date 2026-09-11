export interface LiveOutputBuffers {
  readonly text: string;
  readonly reasoning: string;
}

export interface TurnMetricInput {
  now: number;
  turnStartAt: number | undefined;
  turnDoneAt: number | undefined;
  running: boolean;
  /** Controller wait plus any composer-local pause, in ms. */
  waitAccumMs: number;
  lastTurnWaitAccumMs?: number;
  turnTokens?: number;
  turnOutputTokens?: number;
  lastTurnOutputTokens?: number;
  turnOutputCharsAtUsage?: number;
  turnArgChars?: number;
  turnModelActiveMs: number;
  turnModelActiveAt?: number;
  liveModelActiveAt?: number;
  live?: LiveOutputBuffers;
  turnOutputEstimated?: boolean;
  lastTurnOutputEstimated?: boolean;
}

export interface TurnMetrics {
  elapsedMs: number;
  tokens: number;
  outputTokens: number;
  tps: number | null;
  estimated: boolean;
}

/**
 * UTF-8 byte weight of `s` from `from`, in quarter-tokens. Matches the backend
 * model in internal/agent/agent.go estimateTokensFromBytes, which is CJK-aware
 * by construction: ASCII counts 1, Han/kana/hangul 3.
 */
export function outputQuarters(s: string, from = 0): number {
  let quarters = 0;
  for (let i = from; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c < 0x80) quarters += 1;
    else if (c < 0x800) quarters += 2;
    else if (c >= 0xd800 && c <= 0xdbff) {
      quarters += 4;
      i += 1;
    } else quarters += 3;
  }
  return quarters;
}

/**
 * `Math.round(quarters / 4)` is the previous flat `Math.round(chars / 4)`
 * expression verbatim: pure ASCII sums one quarter per char, so `quarters`
 * equals the char count and every existing value is reproduced exactly.
 */
export function tokensFromQuarters(quarters: number): number {
  return quarters > 0 ? Math.round(quarters / 4) : 0;
}

/**
 * Output tokens streamed since the last billed usage event.
 *
 * `billedChars` is a SUM of both buffer lengths, so the billed/unbilled
 * boundary cannot be located inside either buffer. The newest characters are
 * attributed in stream order (all of `text`, then the tail of `reasoning`) —
 * exact when the stream ended in text, locally bounded otherwise. Streaming
 * tool-call arguments arrive as a bare count, so they are charged as ASCII.
 */
export function unbilledOutputTokens(
  buffers: LiveOutputBuffers | undefined,
  billedChars: number,
  argChars: number,
): number {
  const textChars = buffers?.text.length ?? 0;
  const reasoningChars = buffers?.reasoning.length ?? 0;
  const window = Math.max(0, textChars + reasoningChars - Math.max(0, billedChars || 0));
  const extra = Math.max(0, argChars);
  if (buffers === undefined || window === 0) return tokensFromQuarters(window + extra);
  let quarters = extra;
  if (window >= textChars) quarters += outputQuarters(buffers.text);
  else quarters += outputQuarters(buffers.text, textChars - window);
  const fromReasoning = window - textChars;
  if (fromReasoning > 0) {
    quarters += outputQuarters(buffers.reasoning, Math.max(0, reasoningChars - fromReasoning));
  }
  return tokensFromQuarters(quarters);
}

/** Locale-free elapsed label shared by every surface showing a turn clock. */
export function formatElapsedMs(ms: number): string {
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  return `${Math.floor(s / 60)}m ${s % 60}s`;
}

/**
 * The single definition of the turn's elapsed, token and throughput readings.
 * Returns null when no turn is open or settled, matching the run-strip gate.
 */
export function turnMetrics(input: TurnMetricInput): TurnMetrics | null {
  if (!input.turnStartAt || (!input.running && !input.turnDoneAt)) return null;
  const metricsNow = input.turnDoneAt || input.now;
  const waitMs = input.turnDoneAt
    ? input.lastTurnWaitAccumMs ?? input.waitAccumMs
    : input.waitAccumMs;
  const elapsedMs = Math.max(0, metricsNow - input.turnStartAt - waitMs);
  const estimatedTokens = input.turnDoneAt
    ? Math.max(0, (input.lastTurnOutputTokens ?? input.turnOutputTokens ?? 0) - (input.turnOutputTokens ?? 0))
    : unbilledOutputTokens(input.live, input.turnOutputCharsAtUsage ?? 0, input.turnArgChars ?? 0);
  const outputTokens = (input.turnOutputTokens ?? 0) + estimatedTokens;
  const modelActiveAt = input.liveModelActiveAt ?? input.turnModelActiveAt;
  const modelElapsedMs = Math.max(0, input.turnModelActiveMs
    + (modelActiveAt && modelActiveAt > 0 ? Math.max(0, metricsNow - modelActiveAt) : 0));
  // Below the gate the reading is too noisy to be worth a number.
  const tps = outputTokens > 0 && modelElapsedMs >= 500
    ? Math.round(outputTokens / (modelElapsedMs / 1000))
    : null;
  const estimated = input.turnDoneAt
    ? input.lastTurnOutputEstimated === true
    : estimatedTokens > 0 || input.turnOutputEstimated === true;
  return { elapsedMs, tokens: (input.turnTokens ?? 0) + estimatedTokens, outputTokens, tps, estimated };
}
