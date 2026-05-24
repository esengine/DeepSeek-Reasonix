import type { EditMode } from "../../config.js";
import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

interface ModeBody {
  mode?: unknown;
}

function parseBody(raw: string): ModeBody {
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    return typeof parsed === "object" && parsed !== null ? (parsed as ModeBody) : {};
  } catch {
    return {};
  }
}

const VALID = new Set<EditMode>(["review", "auto", "yolo", "plan"]);

export async function handleEditMode(
  method: string,
  _rest: string[],
  body: string,
  ctx: DashboardContext,
): Promise<ApiResult> {
  if (method === "GET") {
    return {
      status: 200,
      body: { mode: ctx.getEditMode?.() ?? null },
    };
  }
  if (method === "POST") {
    if (!ctx.setEditMode) {
      return {
        status: 503,
        body: { error: "edit-mode mutation requires an attached `reasonix code` session." },
      };
    }
    const { mode } = parseBody(body);
    if (typeof mode !== "string" || !VALID.has(mode as EditMode)) {
      return { status: 400, body: { error: "mode must be review | auto | yolo | plan" } };
    }
    const resolved = ctx.setEditMode(mode as EditMode);
    ctx.audit?.({ ts: Date.now(), action: "set-edit-mode", payload: { mode: resolved } });
    return { status: 200, body: { mode: resolved } };
  }
  return { status: 405, body: { error: "GET or POST only" } };
}
