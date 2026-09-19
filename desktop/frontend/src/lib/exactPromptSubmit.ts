import type { AppBindings } from "./bridge";
import type { InteractionTarget } from "./interactionTarget";
import { hasSessionGeneration } from "./sessionIdentity";
import { notePromptSubmission, promptFailureClass } from "./promptSubmissionDiagnostics";

type ExactPromptBinding = Pick<AppBindings, "ResolvePromptForSession" | "ResolvePromptForTab" | "PendingPromptIdentitiesForTab">;

export async function resolvePromptForSession(
  binding: ExactPromptBinding,
  target: InteractionTarget,
  answer: Record<string, unknown>,
): Promise<void> {
  if (!binding.ResolvePromptForSession) throw new Error("exact session prompt submission is unavailable; upgrade the host");
  if (!target.sessionId || !hasSessionGeneration(target.sessionGeneration)) throw new Error("prompt session identity is unavailable; refresh the card");
  let turnId = target.turnId;
  let runtimeEpoch = target.runtimeEpoch;
  if (!turnId) {
    if (!binding.PendingPromptIdentitiesForTab) throw new Error("prompt identity is unavailable; refresh or upgrade the host");
    const matches = (await binding.PendingPromptIdentitiesForTab(target.tabId)).filter((candidate) => (
      candidate.promptId === target.promptId && candidate.kind === target.kind &&
      (!runtimeEpoch || !candidate.runtimeEpoch || candidate.runtimeEpoch === runtimeEpoch)
    ));
    if (matches.length !== 1) throw new Error("prompt is stale or its exact identity is unavailable");
    turnId = matches[0].turnId;
    runtimeEpoch = matches[0].runtimeEpoch;
  }
  notePromptSubmission(target, "transport", "sent");
  try {
    await binding.ResolvePromptForSession({
      tabId: target.tabId,
      hostId: target.hostId ?? "local",
      sessionId: target.sessionId,
      sessionGeneration: target.sessionGeneration,
      promptId: target.promptId,
      turnId,
      runtimeEpoch: runtimeEpoch ?? "",
      kind: target.kind,
    }, answer);
    notePromptSubmission(target, "transport", "accepted");
  } catch (error) {
    notePromptSubmission(target, "transport", `rejected:${promptFailureClass(error)}`);
    throw error;
  }
}

export async function resolvePromptForTab(
  binding: ExactPromptBinding,
  tabId: string,
  promptId: string,
  kind: string,
  answer: Record<string, unknown>,
  knownTurnId?: string,
  knownRuntimeEpoch?: string,
): Promise<void> {
  if (!binding.ResolvePromptForTab) throw new Error("exact prompt submission is unavailable; upgrade the host");
  let turnId = knownTurnId;
  let runtimeEpoch = knownRuntimeEpoch;
  if (!turnId) {
    if (!binding.PendingPromptIdentitiesForTab) throw new Error("prompt identity is unavailable; refresh or upgrade the host");
    const matches = (await binding.PendingPromptIdentitiesForTab(tabId)).filter((candidate) => (
      candidate.promptId === promptId && candidate.kind === kind &&
      (!knownRuntimeEpoch || !candidate.runtimeEpoch || candidate.runtimeEpoch === knownRuntimeEpoch)
    ));
    if (matches.length !== 1) throw new Error("prompt is stale or its exact identity is unavailable");
    turnId = matches[0].turnId;
    runtimeEpoch = matches[0].runtimeEpoch;
  }
  await binding.ResolvePromptForTab(tabId, promptId, turnId, runtimeEpoch ?? "", kind, answer);
}
