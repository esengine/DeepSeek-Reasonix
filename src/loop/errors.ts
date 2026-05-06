/** Single text-layer DeepSeek-error formatter — 429/5xx never reach here (retry.ts swallows). */
export function formatLoopError(err: Error): string {
  const msg = err.message ?? "";
  if (msg.includes("maximum context length")) {
    const reqMatch = msg.match(/requested\s+(\d+)\s+tokens/);
    const requested = reqMatch
      ? `${Number(reqMatch[1]).toLocaleString()} tokens`
      : "too many tokens";
    return `Context overflow (DeepSeek 400): session history is ${requested}, past the model's prompt limit (V4: 1M tokens; legacy chat/reasoner: 131k). Usually a single tool result grew too big. Reasonix caps new tool results at 8k tokens and auto-heals oversized history on session load — a restart often clears it. If it still overflows, run /forget (delete the session) or /clear (drop the displayed history) to start fresh.`;
  }

  const m = /^DeepSeek (\d{3}):\s*([\s\S]*)$/.exec(msg);
  if (!m) return msg;
  const status = m[1] ?? "";
  const body = m[2] ?? "";
  const inner = extractDeepSeekErrorMessage(body);

  if (status === "401") {
    return `Authentication failed (DeepSeek 401): ${inner}. Your API key is rejected. Fix with \`reasonix setup\` or \`export DEEPSEEK_API_KEY=sk-...\`. Get one at https://platform.deepseek.com/api_keys.`;
  }
  if (status === "402") {
    return `Out of balance (DeepSeek 402): ${inner}. Top up at https://platform.deepseek.com/top_up — the panel header shows your balance once it's non-zero.`;
  }
  if (status === "422") {
    return `Invalid parameter (DeepSeek 422): ${inner}`;
  }
  if (status === "400") {
    return `Bad request (DeepSeek 400): ${inner}`;
  }
  return msg;
}

export function reasonPrefixFor(
  reason: "budget" | "aborted" | "context-guard" | "stuck",
  iterCap: number,
): string {
  if (reason === "aborted") return "[aborted by user (Esc) — summarizing what I found so far]";
  if (reason === "context-guard") {
    return "[context budget running low — summarizing before the next call would overflow]";
  }
  if (reason === "stuck") {
    return "[stuck on a repeated tool call — explaining what was tried and what's blocking progress]";
  }
  return `[tool-call budget (${iterCap}) reached — forcing summary from what I found]`;
}

export function errorLabelFor(
  reason: "budget" | "aborted" | "context-guard" | "stuck",
  iterCap: number,
): string {
  if (reason === "aborted") return "aborted by user";
  if (reason === "context-guard") return "context-guard triggered (prompt > 80% of window)";
  if (reason === "stuck") return "stuck (repeated tool call suppressed by storm-breaker)";
  return `tool-call budget (${iterCap}) reached`;
}

function extractDeepSeekErrorMessage(body: string): string {
  const trimmed = body.trim();
  if (!trimmed) return "(no message)";
  try {
    const parsed = JSON.parse(trimmed);
    if (parsed && typeof parsed === "object") {
      const obj = parsed as { error?: { message?: unknown }; message?: unknown };
      if (obj.error && typeof obj.error.message === "string") return obj.error.message;
      if (typeof obj.message === "string") return obj.message;
    }
  } catch {
    /* not JSON — fall through */
  }
  return trimmed;
}
