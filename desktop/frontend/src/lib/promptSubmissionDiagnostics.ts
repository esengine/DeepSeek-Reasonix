import { noteSessionObservation } from "./sessionObservationDiagnostics";
import type { InteractionTarget } from "./interactionTarget";

export type PromptDiagnosticTarget = Pick<InteractionTarget, "tabId" | "sessionId" | "sessionGeneration"> &
  Partial<Pick<InteractionTarget, "promptId" | "turnId" | "runtimeEpoch" | "kind">>;

/** Closed labels only: host errors may include private paths or tool contents. */
export function promptFailureClass(error: unknown): string {
  const text = (error instanceof Error ? error.message : String(error)).toLowerCase();
  if (/session binding is stale|host binding is stale/.test(text)) return "stale-binding";
  if (/stale runtime|runtime changed/.test(text)) return "stale-runtime";
  if (/stale turn|active turn/.test(text)) return "stale-turn";
  if (/already resolved/.test(text)) return "already-resolved";
  if (/not pending/.test(text)) return "not-pending";
  if (/identity.*(?:unavailable|required)/.test(text)) return "missing-identity";
  if (/unavailable|upgrade/.test(text)) return "unavailable";
  return "other";
}

export function notePromptSubmission(target: PromptDiagnosticTarget, stage: "command" | "transport", status: string): void {
  if (!target.sessionId) return;
  noteSessionObservation(`session-id:${target.sessionId}`, {
    action: `prompt-${stage}`, tabId: target.tabId, generation: target.sessionGeneration ?? 0,
    sequence: 0, status,
    prompt: { id: target.promptId, turnId: target.turnId, runtimeEpoch: target.runtimeEpoch,
      kind: target.kind, bindingGeneration: target.sessionGeneration ?? null },
  });
}
