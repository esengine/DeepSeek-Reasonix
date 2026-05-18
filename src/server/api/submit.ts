import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

interface SubmitBody {
  prompt?: unknown;
}

function parseBody(raw: string): SubmitBody {
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    return typeof parsed === "object" && parsed !== null ? (parsed as SubmitBody) : {};
  } catch {
    return {};
  }
}

export async function handleSubmit(
  method: string,
  _rest: string[],
  body: string,
  ctx: DashboardContext,
): Promise<ApiResult> {
  if (method !== "POST") {
    return { status: 405, body: { error: "POST only" } };
  }
  if (!ctx.submitPrompt) {
    return {
      status: 503,
      body: {
        error:
          "submit requires an attached dashboard session — open `/dashboard` from inside `reasonix code` or `reasonix chat`.",
      },
    };
  }
  const { prompt } = parseBody(body);
  if (typeof prompt !== "string" || !prompt.trim()) {
    return { status: 400, body: { error: "prompt (non-empty string) required" } };
  }
  const result = ctx.submitPrompt(prompt);
  if (!result.accepted) {
    return {
      status: 409,
      body: { accepted: false, reason: result.reason ?? "loop is busy" },
    };
  }
  ctx.audit?.({
    ts: Date.now(),
    action: "submit-prompt",
    payload: { length: prompt.length },
  });
  return {
    status: 202,
    body: { accepted: true, ...(result.queued ? { queued: true } : {}) },
  };
}
