/** GET snapshots the active modal so a fresh client paints what's already up; POST routes resolution into the same handlers the TUI uses. */

import type { DashboardContext, PickerResolution } from "../context.js";
import type { ApiResult } from "../router.js";

interface ResolveBody {
  kind?: unknown;
  choice?: unknown;
  text?: unknown;
  action?: unknown;
  id?: unknown;
  query?: unknown;
}

function parsePickerResolution(body: ResolveBody): PickerResolution | { error: string } {
  const { action, id, text, query } = body;
  if (typeof action !== "string") return { error: "picker action required" };
  switch (action) {
    case "pick":
    case "delete":
    case "install":
    case "uninstall":
      if (typeof id !== "string" || !id) return { error: `picker ${action} requires id` };
      return { action, id };
    case "rename":
      if (typeof id !== "string" || !id) return { error: "picker rename requires id" };
      if (typeof text !== "string") return { error: "picker rename requires text" };
      return { action: "rename", id, text };
    case "new":
      return typeof text === "string" && text ? { action: "new", text } : { action: "new" };
    case "load-more":
      return { action: "load-more" };
    case "refine":
      if (typeof query !== "string") return { error: "picker refine requires query" };
      return { action: "refine", query };
    case "cancel":
      return { action: "cancel" };
    default:
      return { error: `unknown picker action: ${action}` };
  }
}

function parseBody(raw: string): ResolveBody {
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    return typeof parsed === "object" && parsed !== null ? (parsed as ResolveBody) : {};
  } catch {
    return {};
  }
}

export async function handleModal(
  method: string,
  rest: string[],
  body: string,
  ctx: DashboardContext,
): Promise<ApiResult> {
  if (method === "GET" && rest.length === 0) {
    return {
      status: 200,
      body: { modal: ctx.getActiveModal ? ctx.getActiveModal() : null },
    };
  }

  if (method === "POST" && rest[0] === "resolve") {
    const parsed = parseBody(body);
    const { kind, choice, text } = parsed;
    if (kind === "shell") {
      if (!ctx.resolveShellConfirm) {
        return { status: 503, body: { error: "shell modal resolution not wired" } };
      }
      if (choice !== "run_once" && choice !== "always_allow" && choice !== "deny") {
        return {
          status: 400,
          body: { error: "shell choice must be run_once / always_allow / deny" },
        };
      }
      ctx.resolveShellConfirm(choice);
      return { status: 200, body: { resolved: true } };
    }
    if (kind === "choice") {
      if (!ctx.resolveChoiceConfirm) {
        return { status: 503, body: { error: "choice modal resolution not wired" } };
      }
      // The wire shape mirrors ChoiceResolution: { kind: "pick"|"custom"|"cancel", ... }.
      const c = choice as Record<string, unknown> | undefined;
      if (!c || typeof c !== "object") {
        return { status: 400, body: { error: "choice must be an object with a kind field" } };
      }
      if (c.kind === "pick" && typeof c.optionId === "string") {
        ctx.resolveChoiceConfirm({ kind: "pick", optionId: c.optionId });
        return { status: 200, body: { resolved: true } };
      }
      if (c.kind === "custom" && typeof c.text === "string") {
        ctx.resolveChoiceConfirm({ kind: "custom", text: c.text });
        return { status: 200, body: { resolved: true } };
      }
      if (c.kind === "cancel") {
        ctx.resolveChoiceConfirm({ kind: "cancel" });
        return { status: 200, body: { resolved: true } };
      }
      return { status: 400, body: { error: "unknown choice resolution shape" } };
    }
    if (kind === "plan") {
      if (!ctx.resolvePlanConfirm) {
        return { status: 503, body: { error: "plan modal resolution not wired" } };
      }
      if (choice !== "approve" && choice !== "refine" && choice !== "cancel") {
        return { status: 400, body: { error: "plan choice must be approve / refine / cancel" } };
      }
      ctx.resolvePlanConfirm(choice, typeof text === "string" && text.trim() ? text : undefined);
      return { status: 200, body: { resolved: true } };
    }
    if (kind === "edit-review") {
      if (!ctx.resolveEditReview) {
        return { status: 503, body: { error: "edit-review modal resolution not wired" } };
      }
      if (
        choice !== "apply" &&
        choice !== "reject" &&
        choice !== "apply-rest-of-turn" &&
        choice !== "flip-to-auto"
      ) {
        return { status: 400, body: { error: "edit-review choice invalid" } };
      }
      ctx.resolveEditReview(choice);
      return { status: 200, body: { resolved: true } };
    }
    if (kind === "checkpoint") {
      if (!ctx.resolveCheckpointConfirm) {
        return { status: 503, body: { error: "checkpoint modal resolution not wired" } };
      }
      if (choice !== "continue" && choice !== "revise" && choice !== "stop") {
        return {
          status: 400,
          body: { error: "checkpoint choice must be continue / revise / stop" },
        };
      }
      ctx.resolveCheckpointConfirm(
        choice,
        typeof text === "string" && text.trim() ? text : undefined,
      );
      return { status: 200, body: { resolved: true } };
    }
    if (kind === "revision") {
      if (!ctx.resolveReviseConfirm) {
        return { status: 503, body: { error: "revision modal resolution not wired" } };
      }
      if (choice !== "accept" && choice !== "reject") {
        return { status: 400, body: { error: "revision choice must be accept / reject" } };
      }
      ctx.resolveReviseConfirm(choice);
      return { status: 200, body: { resolved: true } };
    }
    if (kind === "picker") {
      if (!ctx.resolvePicker) {
        return { status: 503, body: { error: "picker modal resolution not wired" } };
      }
      const resolution = parsePickerResolution(parsed);
      if ("error" in resolution) {
        return { status: 400, body: { error: resolution.error } };
      }
      ctx.resolvePicker(resolution);
      return { status: 200, body: { resolved: true } };
    }
    if (kind === "viewer") {
      if (!ctx.resolveViewer) {
        return { status: 503, body: { error: "viewer modal resolution not wired" } };
      }
      if (parsed.action !== "close") {
        return { status: 400, body: { error: "viewer action must be close" } };
      }
      ctx.resolveViewer({ action: "close" });
      return { status: 200, body: { resolved: true } };
    }
    return { status: 400, body: { error: `unknown modal kind: ${String(kind)}` } };
  }

  return { status: 405, body: { error: `method ${method} not supported on this path` } };
}
