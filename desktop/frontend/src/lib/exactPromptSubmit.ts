import type { AppBindings } from "./bridge";

type ExactPromptBinding = Pick<AppBindings, "ListTabs" | "ResolvePromptForTab">;

export async function resolvePromptForTab(
  binding: ExactPromptBinding,
  tabId: string,
  promptId: string,
  kind: string,
  answer: Record<string, unknown>,
  knownTurnId?: string,
  knownRuntimeEpoch?: string,
): Promise<void> {
  // Ask cards must refresh the active turn from the tab owner immediately
  // before submission. A locally captured turn is useful for the identity
  // fence, but it is not authoritative during startup/rebind races.
  // Other prompt kinds keep their immutable card turn and only consult
  // ListTabs when the card arrived before local turn state was available.
  const tab = kind === "ask" || !knownTurnId
    ? (await binding.ListTabs()).find((candidate) => candidate.id === tabId)
    : undefined;
  const turnId = kind === "ask" ? tab?.turnId : (knownTurnId ?? tab?.turnId);
  if (!binding.ResolvePromptForTab || !turnId) throw new Error("active turn identity is unavailable");
  await binding.ResolvePromptForTab(tabId, promptId, turnId, knownRuntimeEpoch ?? tab?.runtime?.epoch ?? "", kind, answer);
}
