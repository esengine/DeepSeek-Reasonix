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
  const tab = knownTurnId ? undefined : (await binding.ListTabs()).find((candidate) => candidate.id === tabId);
  const turnId = knownTurnId ?? tab?.turnId;
  if (!binding.ResolvePromptForTab || !turnId) throw new Error("active turn identity is unavailable");
  await binding.ResolvePromptForTab(tabId, promptId, turnId, knownRuntimeEpoch ?? tab?.runtime?.epoch ?? "", kind, answer);
}
