/** Bridges Reasonix's internal `PauseGate` requests onto ACP `session/request_permission` round-trips. */

import type {
  CheckpointVerdict,
  ChoiceVerdict,
  ConfirmationChoice,
  PauseRequest,
  PlanVerdict,
  RevisionVerdict,
} from "../core/pause-gate.js";
import type {
  PermissionOption,
  PermissionRequestParams,
  PermissionRequestResult,
} from "./protocol.js";
import type { AcpServer } from "./server.js";

const ID_ALLOW_ONCE = "allow_once";
const ID_ALLOW_ALWAYS = "allow_always";
const ID_REJECT = "reject";
const ID_REFINE = "refine";
const ID_REVISE = "revise";
const ID_STOP = "stop";
const ID_CANCEL = "cancel";
const ID_ACCEPT = "accept";

/** Build the `options` list shown to the host for a given gate kind. The IDs are what the host echoes back in the response. */
export function permissionOptionsFor(req: PauseRequest): PermissionOption[] {
  switch (req.kind) {
    case "run_command":
    case "run_background":
    case "path_access":
      return [
        { optionId: ID_ALLOW_ONCE, name: "Allow once", kind: "allow_once" },
        { optionId: ID_ALLOW_ALWAYS, name: "Allow always", kind: "allow_always" },
        { optionId: ID_REJECT, name: "Reject", kind: "reject_once" },
      ];
    case "plan_proposed":
      return [
        { optionId: ID_ALLOW_ONCE, name: "Approve plan", kind: "allow_once" },
        { optionId: ID_REFINE, name: "Refine", kind: "allow_once" },
        { optionId: ID_CANCEL, name: "Cancel", kind: "reject_once" },
      ];
    case "plan_checkpoint":
      return [
        { optionId: ID_ALLOW_ONCE, name: "Continue", kind: "allow_once" },
        { optionId: ID_REVISE, name: "Revise", kind: "allow_once" },
        { optionId: ID_STOP, name: "Stop", kind: "reject_once" },
      ];
    case "plan_revision":
      return [
        { optionId: ID_ACCEPT, name: "Accept revision", kind: "allow_once" },
        { optionId: ID_REJECT, name: "Keep original plan", kind: "reject_once" },
      ];
    case "choice": {
      const payload = req.payload as { options: { id: string; title?: string }[] };
      const opts: PermissionOption[] = (payload.options ?? []).map((o) => ({
        optionId: o.id,
        name: o.title ?? o.id,
        kind: "allow_once",
      }));
      opts.push({ optionId: ID_CANCEL, name: "Cancel", kind: "reject_once" });
      return opts;
    }
  }
}

function commandPrefix(command: string): string {
  const first = command.trim().split(/\s+/)[0] ?? command.trim();
  return `${first} *`;
}

function pathPrefix(p: string): string {
  return p;
}

/** Map an ACP permission response back into the internal verdict shape PauseGate.resolve expects. */
export function verdictFor(
  req: PauseRequest,
  result: PermissionRequestResult,
): ConfirmationChoice | PlanVerdict | CheckpointVerdict | RevisionVerdict | ChoiceVerdict {
  const cancelled = result.outcome.outcome === "cancelled";
  const optionId = result.outcome.outcome === "selected" ? result.outcome.optionId : null;
  switch (req.kind) {
    case "run_command":
    case "run_background": {
      if (cancelled || optionId === ID_REJECT) return { type: "deny" };
      if (optionId === ID_ALLOW_ALWAYS) {
        const payload = req.payload as { command?: string };
        return { type: "always_allow", prefix: commandPrefix(payload.command ?? "") };
      }
      return { type: "run_once" };
    }
    case "path_access": {
      if (cancelled || optionId === ID_REJECT) return { type: "deny" };
      if (optionId === ID_ALLOW_ALWAYS) {
        const payload = req.payload as { allowPrefix: string };
        return { type: "always_allow", prefix: pathPrefix(payload.allowPrefix) };
      }
      return { type: "run_once" };
    }
    case "plan_proposed": {
      if (cancelled || optionId === ID_CANCEL) return { type: "cancel" };
      if (optionId === ID_REFINE) return { type: "refine" };
      return { type: "approve" };
    }
    case "plan_checkpoint": {
      if (cancelled || optionId === ID_STOP) return { type: "stop" };
      if (optionId === ID_REVISE) return { type: "revise" };
      return { type: "continue" };
    }
    case "plan_revision": {
      if (cancelled) return { type: "cancelled" };
      if (optionId === ID_ACCEPT) return { type: "accepted" };
      return { type: "rejected" };
    }
    case "choice": {
      if (cancelled || optionId === ID_CANCEL || !optionId) return { type: "cancel" };
      return { type: "pick", optionId };
    }
  }
}

function permissionTitleFor(req: PauseRequest): string {
  switch (req.kind) {
    case "run_command":
    case "run_background":
      return `Run command — ${((req.payload as { command?: string }).command ?? "").slice(0, 80)}`;
    case "path_access":
      return `Access path — ${(req.payload as { path: string }).path}`;
    case "plan_proposed":
      return "Approve plan";
    case "plan_checkpoint":
      return `Checkpoint — ${(req.payload as { title?: string }).title ?? "step complete"}`;
    case "plan_revision":
      return "Approve plan revision";
    case "choice":
      return (req.payload as { question?: string }).question ?? "Choose an option";
  }
}

function permissionKindFor(req: PauseRequest): "execute" | "edit" | "other" {
  if (req.kind === "run_command" || req.kind === "run_background") return "execute";
  if (req.kind === "path_access") {
    return (req.payload as { intent?: string }).intent === "write" ? "edit" : "other";
  }
  return "other";
}

/** Forward a PauseGate request as an ACP session/request_permission outbound call. Returns the verdict to pass into pauseGate.resolve. */
export async function requestPermissionForGate(
  server: AcpServer,
  sessionId: string,
  req: PauseRequest,
): Promise<ConfirmationChoice | PlanVerdict | CheckpointVerdict | RevisionVerdict | ChoiceVerdict> {
  const params: PermissionRequestParams = {
    sessionId,
    toolCall: {
      toolCallId: `gate-${req.id}`,
      title: permissionTitleFor(req),
      kind: permissionKindFor(req),
      status: "pending",
      rawInput: req.payload,
    },
    options: permissionOptionsFor(req),
  };
  let result: PermissionRequestResult;
  try {
    result = await server.sendRequest<PermissionRequestResult>(
      "session/request_permission",
      params,
    );
  } catch {
    result = { outcome: { outcome: "cancelled" } };
  }
  return verdictFor(req, result);
}
