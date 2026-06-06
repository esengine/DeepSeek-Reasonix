/** Bridges Reasonix's internal `PauseGate` requests onto ACP `session/request_permission` round-trips. */

import { resolveApprovalPrompt, toApprovalPrompt } from "@reasonix/core-utils";
import type { PauseRequest } from "../core/pause-gate.js";
import type {
  PermissionOption,
  PermissionRequestParams,
  PermissionRequestResult,
} from "./protocol.js";
import type { AcpServer } from "./server.js";

/** Map an ApprovalPrompt kind to the ACP permission kind used by the host UI. */
function acpPermissionKindFor(
  prompt: ReturnType<typeof toApprovalPrompt>,
): "execute" | "edit" | "other" {
  switch (prompt.kind) {
    case "shell":
      return "execute";
    case "path":
      return prompt.data?.intent === "write" ? "edit" : "other";
    default:
      return "other";
  }
}

/** Map an ApprovalAction kind to the ACP PermissionOptionKind.
 *  "custom" actions are treated as one-shot allows on the ACP wire. */
function acpOptionKindFor(
  kind: ReturnType<typeof toApprovalPrompt>["actions"][number]["kind"],
): PermissionOption["kind"] {
  switch (kind) {
    case "allow_once":
      return "allow_once";
    case "allow_always":
      return "allow_always";
    case "reject":
      return "reject_once";
    case "custom":
      return "allow_once";
  }
}

/** Forward a PauseGate request as an ACP `session/request_permission` call.
 *  Uses `toApprovalPrompt()` as the single source of truth for UI metadata.
 */
export async function requestPermissionForGate(
  server: AcpServer,
  sessionId: string,
  req: PauseRequest,
): Promise<
  | import("@reasonix/core-utils").ConfirmationChoice
  | import("@reasonix/core-utils").PlanVerdict
  | import("@reasonix/core-utils").CheckpointVerdict
  | import("@reasonix/core-utils").RevisionVerdict
  | import("@reasonix/core-utils").ChoiceVerdict
> {
  const prompt = toApprovalPrompt(req);

  const params: PermissionRequestParams = {
    sessionId,
    toolCall: {
      toolCallId: `gate-${req.id}`,
      title: prompt.title,
      kind: acpPermissionKindFor(prompt),
      status: "pending",
      rawInput: req.payload,
    },
    options: prompt.actions.map(
      (a): PermissionOption => ({
        optionId: a.id,
        name: a.label,
        kind: acpOptionKindFor(a.kind),
      }),
    ),
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

  if (result.outcome.outcome === "cancelled") {
    return resolveApprovalPrompt(prompt, "");
  }

  return resolveApprovalPrompt(prompt, result.outcome.optionId);
}
