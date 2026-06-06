/** Verdict shapes returned by the permission gate (PauseGate → UI modal → user choice).
 *  These types are shared between the core loop, the ACP bridge, and any
 *  future UI surface that needs to render or resolve a permission prompt. */

export type ConfirmationChoice =
  | { type: "deny"; denyContext?: string }
  | { type: "run_once" }
  | { type: "always_allow"; prefix: string };

export type PlanVerdict =
  | { type: "approve"; feedback?: string }
  | { type: "refine"; feedback?: string }
  | { type: "cancel"; feedback?: string };

export type CheckpointVerdict =
  | { type: "continue" }
  | { type: "revise"; feedback?: string }
  | { type: "stop" };

export type RevisionVerdict = { type: "accepted" } | { type: "rejected" } | { type: "cancelled" };

export type ChoiceVerdict =
  | { type: "pick"; optionId: string }
  | { type: "text"; text: string }
  | { type: "cancel" };
