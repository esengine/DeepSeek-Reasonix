import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

interface LoopStartBody {
  intervalMs?: unknown;
  prompt?: unknown;
}

function parseBody(raw: string): LoopStartBody {
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    return typeof parsed === "object" && parsed !== null ? (parsed as LoopStartBody) : {};
  } catch {
    return {};
  }
}

const MIN_INTERVAL_MS = 5_000;
const MAX_INTERVAL_MS = 6 * 60 * 60 * 1000;

export async function handleLoop(
  method: string,
  rest: string[],
  body: string,
  ctx: DashboardContext,
): Promise<ApiResult> {
  if (method === "GET" && rest[0] === "status") {
    if (!ctx.getLoopRunStatus) {
      return { status: 503, body: { error: "auto-loop not available — attach to a chat session" } };
    }
    return { status: 200, body: { status: ctx.getLoopRunStatus() } };
  }

  if (method === "POST" && rest[0] === "start") {
    if (!ctx.startAutoLoop) {
      return { status: 503, body: { error: "auto-loop start not wired" } };
    }
    const { intervalMs, prompt } = parseBody(body);
    if (typeof prompt !== "string" || !prompt.trim()) {
      return { status: 400, body: { error: "prompt must be a non-empty string" } };
    }
    if (
      typeof intervalMs !== "number" ||
      !Number.isFinite(intervalMs) ||
      intervalMs < MIN_INTERVAL_MS ||
      intervalMs > MAX_INTERVAL_MS
    ) {
      return {
        status: 400,
        body: {
          error: `intervalMs must be a number in [${MIN_INTERVAL_MS}, ${MAX_INTERVAL_MS}] (5s..6h)`,
        },
      };
    }
    ctx.startAutoLoop(intervalMs, prompt.trim());
    ctx.audit?.({ ts: Date.now(), action: "auto-loop-start", payload: { intervalMs } });
    return { status: 200, body: { started: true } };
  }

  if (method === "POST" && rest[0] === "stop") {
    if (!ctx.stopAutoLoop) {
      return { status: 503, body: { error: "auto-loop stop not wired" } };
    }
    ctx.stopAutoLoop();
    ctx.audit?.({ ts: Date.now(), action: "auto-loop-stop" });
    return { status: 200, body: { stopped: true } };
  }

  return {
    status: 405,
    body: { error: `method ${method} not supported on /api/loop/${rest[0] ?? ""}` },
  };
}
