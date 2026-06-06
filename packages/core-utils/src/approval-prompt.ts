/** Unified UI description for PauseGate permission prompts.
 *
 *  Every surface (CLI TUI, Desktop, Dashboard, ACP) converts a PauseGate
 *  PauseRequest into this structure for rendering, then maps the user's
 *  selection back through resolveApprovalPrompt() into the verdict shape
 *  PauseGate expects.
 *
 *  This is the single source of truth for:
 *  - title / subtitle / preview / meta assembly
 *  - option list ordering and IDs
 *  - tone selection
 *  - derivePrefix computation for "always allow"
 */

import { derivePrefix } from "./derive-prefix.js";
import type {
  CheckpointVerdict,
  ChoiceVerdict,
  ConfirmationChoice,
  PlanVerdict,
  RevisionVerdict,
} from "./permission-types.js";

export type ApprovalPromptKind =
  | "shell"
  | "path"
  | "plan"
  | "checkpoint"
  | "revision"
  | "choice";

export type ApprovalTone = "warn" | "error" | "info" | "accent";

export interface ApprovalAction {
  id: string;
  label: string;
  kind: "allow_once" | "allow_always" | "reject" | "custom";
  /** If present, selecting this action requires secondary input (e.g., deny reason). */
  secondaryInput?: { hint: string; required: boolean };
}

export interface ApprovalPrompt {
  id: number;
  kind: ApprovalPromptKind;
  tone: ApprovalTone;
  title: string;
  subtitle?: string;
  preview?: string;
  meta?: Record<string, string>;
  actions: ApprovalAction[];
  /** Opaque data consumed by resolveApprovalPrompt (e.g. prefix for always_allow). */
  data?: Record<string, unknown>;
}

/** Convert any PauseRequest-like input into a platform-agnostic UI description.
 *  Labels are English fallbacks so the ACP surface (which has no i18n layer)
 *  can render immediately.  CLI and Desktop may override labels with their
 *  own translation systems if desired.
 */
export function toApprovalPrompt(req: {
  id: number;
  kind: string;
  payload: unknown;
}): ApprovalPrompt {
  const payload = req.payload as Record<string, unknown>;
  switch (req.kind) {
    case "run_command":
      return shellPrompt(req.id, payload, false);
    case "run_background":
      return shellPrompt(req.id, payload, true);
    case "path_access":
      return pathPrompt(req.id, payload);
    case "plan_proposed":
      return planPrompt(req.id, payload);
    case "plan_checkpoint":
      return checkpointPrompt(req.id, payload);
    case "plan_revision":
      return revisionPrompt(req.id, payload);
    case "choice":
      return choicePrompt(req.id, payload);
    default:
      // Defensive: unknown kinds render as a generic warn prompt with a
      // single dismiss action so the gate never hangs.
      return {
        id: req.id,
        kind: "shell",
        tone: "warn",
        title: "Unrecognized request",
        subtitle: String(req.kind),
        actions: [{ id: "deny", label: "Dismiss", kind: "reject" }],
      };
  }
}

function shellPrompt(
  id: number,
  payload: Record<string, unknown>,
  isBackground: boolean,
): ApprovalPrompt {
  const command = String(payload.command ?? "");
  const cwd = payload.cwd ? String(payload.cwd) : undefined;
  const timeoutSec =
    !isBackground && typeof payload.timeoutSec === "number"
      ? payload.timeoutSec
      : undefined;
  const waitSec =
    isBackground && typeof payload.waitSec === "number"
      ? payload.waitSec
      : undefined;
  const prefix = derivePrefix(command);

  const meta: Record<string, string> = {};
  if (cwd) meta.cwd = cwd;
  if (timeoutSec !== undefined) meta.timeout = `${timeoutSec}s`;
  if (waitSec !== undefined) meta.wait = `${waitSec}s`;

  return {
    id,
    kind: "shell",
    tone: "warn",
    title: isBackground ? "Run background command" : "Run command",
    subtitle: command,
    preview: command,
    meta: Object.keys(meta).length > 0 ? meta : undefined,
    actions: [
      {
        id: "run_once",
        label: "Run once",
        kind: "allow_once",
      },
      {
        id: "always_allow",
        label: `Always allow — ${prefix}`,
        kind: "allow_always",
      },
      {
        id: "deny",
        label: "Deny",
        kind: "reject",
        secondaryInput: {
          hint: "Reason for denial (optional)",
          required: false,
        },
      },
    ],
    data: { prefix },
  };
}

function pathPrompt(
  id: number,
  payload: Record<string, unknown>,
): ApprovalPrompt {
  const path = String(payload.path ?? "");
  const intent = payload.intent === "write" ? "write" : "read";
  const toolName = String(payload.toolName ?? "");
  const sandboxRoot = String(payload.sandboxRoot ?? "");
  const allowPrefix = String(payload.allowPrefix ?? "");

  const meta: Record<string, string> = {};
  if (sandboxRoot) meta.sandboxRoot = sandboxRoot;

  return {
    id,
    kind: "path",
    tone: "warn",
    title: `Access path — ${intent}`,
    subtitle: path,
    preview: `${toolName} → ${path}`,
    meta: Object.keys(meta).length > 0 ? meta : undefined,
    actions: [
      {
        id: "run_once",
        label: intent === "write" ? "Allow write" : "Allow read",
        kind: "allow_once",
      },
      {
        id: "always_allow",
        label: `Always allow — ${allowPrefix}`,
        kind: "allow_always",
      },
      {
        id: "deny",
        label: "Deny",
        kind: "reject",
        secondaryInput: {
          hint: "Reason for denial (optional)",
          required: false,
        },
      },
    ],
    data: { prefix: allowPrefix, intent },
  };
}

function planPrompt(
  id: number,
  payload: Record<string, unknown>,
): ApprovalPrompt {
  const plan = String(payload.plan ?? "");
  const summary = payload.summary ? String(payload.summary) : undefined;
  const steps = Array.isArray(payload.steps) ? payload.steps : [];
  const subtitle = summary ?? (plan.length > 80 ? `${plan.slice(0, 80)}…` : plan);

  const meta: Record<string, string> = {};
  if (steps.length > 0) meta.steps = String(steps.length);

  return {
    id,
    kind: "plan",
    tone: "accent",
    title: "Approve plan",
    subtitle,
    preview: plan,
    meta: Object.keys(meta).length > 0 ? meta : undefined,
    actions: [
      { id: "approve", label: "Approve", kind: "allow_once" },
      { id: "refine", label: "Refine", kind: "custom" },
      { id: "cancel", label: "Cancel", kind: "reject" },
    ],
  };
}

function checkpointPrompt(
  id: number,
  payload: Record<string, unknown>,
): ApprovalPrompt {
  const titleText = String(payload.title ?? "step complete");
  const result = String(payload.result ?? "");
  const notes = payload.notes ? String(payload.notes) : undefined;
  const completed =
    typeof payload.completed === "number" ? payload.completed : 0;
  const total = typeof payload.total === "number" ? payload.total : 0;

  const meta: Record<string, string> = {};
  if (total > 0) meta.progress = `${completed}/${total}`;

  let preview = result;
  if (notes) preview += `\n${notes}`;

  return {
    id,
    kind: "checkpoint",
    tone: "info",
    title: `Checkpoint — ${titleText}`,
    subtitle: result,
    preview: preview || undefined,
    meta: Object.keys(meta).length > 0 ? meta : undefined,
    actions: [
      { id: "continue", label: "Continue", kind: "allow_once" },
      { id: "revise", label: "Revise", kind: "custom" },
      { id: "stop", label: "Stop", kind: "reject" },
    ],
  };
}

function revisionPrompt(
  id: number,
  payload: Record<string, unknown>,
): ApprovalPrompt {
  const reason = String(payload.reason ?? "");
  const remainingSteps = Array.isArray(payload.remainingSteps)
    ? payload.remainingSteps
    : [];
  const summary = payload.summary ? String(payload.summary) : undefined;
  const subtitle = summary ?? (reason.length > 80 ? `${reason.slice(0, 80)}…` : reason);

  const meta: Record<string, string> = {};
  if (remainingSteps.length > 0) meta.steps = String(remainingSteps.length);

  return {
    id,
    kind: "revision",
    tone: "warn",
    title: "Approve plan revision",
    subtitle,
    preview: reason,
    meta: Object.keys(meta).length > 0 ? meta : undefined,
    actions: [
      { id: "accept", label: "Accept", kind: "allow_once" },
      { id: "reject", label: "Keep original", kind: "reject" },
    ],
  };
}

function choicePrompt(
  id: number,
  payload: Record<string, unknown>,
): ApprovalPrompt {
  const question = String(payload.question ?? "Choose an option");
  const rawOptions = Array.isArray(payload.options) ? payload.options : [];
  const allowCustom = payload.allowCustom === true;

  const actions: ApprovalAction[] = rawOptions.map((o: unknown) => {
    const opt = o as Record<string, unknown>;
    const optId = String(opt.id ?? "");
    const optTitle = String(opt.title ?? optId);
    return { id: optId, label: optTitle, kind: "custom" };
  });

  actions.push({ id: "cancel", label: "Cancel", kind: "reject" });

  return {
    id,
    kind: "choice",
    tone: "info",
    title: question,
    actions,
    data: allowCustom ? { allowCustom: true } : undefined,
  };
}

/** Convert a user's action selection into the verdict PauseGate expects.
 *  The returned shape is a union; callers should narrow by prompt.kind.
 */
export function resolveApprovalPrompt(
  prompt: ApprovalPrompt,
  actionId: string,
  secondaryInput?: string,
):
  | ConfirmationChoice
  | PlanVerdict
  | CheckpointVerdict
  | RevisionVerdict
  | ChoiceVerdict {
  const action = prompt.actions.find((a) => a.id === actionId);

  // Fallback when the action is missing or unrecognised — return the safest
  // default for the prompt kind so the gate never hangs.
  if (!action) {
    return safeDefaultForKind(prompt.kind);
  }

  switch (prompt.kind) {
    case "shell":
    case "path": {
      if (action.kind === "reject") {
        return { type: "deny", denyContext: secondaryInput };
      }
      if (action.kind === "allow_always") {
        return {
          type: "always_allow",
          prefix: String(prompt.data?.prefix ?? ""),
        };
      }
      return { type: "run_once" };
    }
    case "plan": {
      if (action.id === "approve") return { type: "approve" };
      if (action.id === "refine") return { type: "refine" };
      return { type: "cancel" };
    }
    case "checkpoint": {
      if (action.id === "continue") return { type: "continue" };
      if (action.id === "revise") return { type: "revise" };
      return { type: "stop" };
    }
    case "revision": {
      if (action.id === "accept") return { type: "accepted" };
      return { type: "rejected" };
    }
    case "choice": {
      if (action.kind === "reject") return { type: "cancel" };
      return { type: "pick", optionId: action.id };
    }
  }
}

function safeDefaultForKind(
  kind: ApprovalPromptKind,
):
  | ConfirmationChoice
  | PlanVerdict
  | CheckpointVerdict
  | RevisionVerdict
  | ChoiceVerdict {
  switch (kind) {
    case "shell":
    case "path":
      return { type: "deny" };
    case "plan":
      return { type: "cancel" };
    case "checkpoint":
      return { type: "stop" };
    case "revision":
      return { type: "rejected" };
    case "choice":
      return { type: "cancel" };
  }
}
