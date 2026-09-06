import type { AppBindings } from "./bridge";
import type { StructuredInvocationSubmit } from "./invocationDisplay";
import { asArray } from "./array";
import type { QuestionAnswer } from "./types";

type InboxEnqueueBindings = Pick<AppBindings, "EnqueueInboxFollowup" | "EnqueueInboxFollowupWithInvocations" | "EnqueueInboxSteer" | "EnqueueInboxSteerForTurn">;
type ActiveTurnBindings = Pick<AppBindings, "ListTabs" | "SteerInboxItem" | "SteerInboxItemForTurn">;

export async function resolveActiveTurnId(binding: Pick<AppBindings, "ListTabs">, tabId: string, _known?: string): Promise<string | undefined> {
  // The cached id can outlive the controller turn during startup, recovery,
  // or a runtime rebuild. Always refresh from the tab owner before crossing
  // the exact-turn answer/steer boundary; the optional value is only a hint
  // for callers that have not received local state yet.
  const authoritative = asArray(await binding.ListTabs()).find((tab) => tab.id === tabId)?.turnId;
  return authoritative;
}

type AskAnswerBindings = Pick<AppBindings, "ListTabs" | "ResolvePromptForTab">;

export async function resolvePromptForTab(
  binding: Pick<AppBindings, "ListTabs" | "ResolvePromptForTab">,
  tabId: string,
  promptId: string,
  kind: string,
  answer: Record<string, unknown>,
  knownTurnId?: string,
  knownRuntimeEpoch?: string,
): Promise<void> {
  const submit = await import("./exactPromptSubmit");
  return submit.resolvePromptForTab(binding, tabId, promptId, kind, answer, knownTurnId, knownRuntimeEpoch);
}

// Final frontend boundary before optimistic transcript state is created.
export function normalizeTurnSubmit(displayText: string, submitText: string) {
  const display = displayText.trim();
  const submit = submitText.trim();
  if (!submit) throw new Error("Message cannot be empty.");
  return { display, submit };
}

export async function answerPromptForActiveTurn(
  binding: AskAnswerBindings,
  tabId: string,
  promptId: string,
  answers: QuestionAnswer[],
  knownTurnId?: string,
  knownRuntimeEpoch?: string,
): Promise<void> {
  await resolvePromptForTab(binding, tabId, promptId, "ask", { questions: answers }, knownTurnId, knownRuntimeEpoch);
}

export async function steerInboxItemForActiveTurn(binding: ActiveTurnBindings, tabId: string, itemId: string, knownTurnId?: string) {
  if (typeof binding.SteerInboxItemForTurn !== "function") return binding.SteerInboxItem(tabId, itemId);
  const turnId = await resolveActiveTurnId(binding, tabId, knownTurnId);
  if (!turnId) throw new Error("active turn id is unavailable; refresh and try again");
  return binding.SteerInboxItemForTurn(tabId, turnId, itemId);
}

export async function enqueueInboxGuidanceForActiveTurn(
  binding: InboxEnqueueBindings & Pick<AppBindings, "ListTabs">,
  tabId: string,
  display: string,
  submit: string,
  structured?: StructuredInvocationSubmit,
  knownTurnId?: string,
) {
  const turnId = !structured && typeof binding.EnqueueInboxSteerForTurn === "function"
    ? await resolveActiveTurnId(binding, tabId, knownTurnId)
    : knownTurnId;
  return enqueueInboxGuidance(binding, tabId, display, submit, structured, { steer: true, turnId });
}

export function enqueueInboxGuidance(
  binding: InboxEnqueueBindings,
  tabId: string,
  display: string,
  submit: string,
  structured?: StructuredInvocationSubmit,
  opts?: { steer?: boolean; turnId?: string },
) {
  if (structured) {
    return binding.EnqueueInboxFollowupWithInvocations(
      tabId,
      structured.display.trim() || display,
      structured.input.trim(),
      structured.invocations,
      "",
    );
  }
  if (opts?.steer && typeof binding.EnqueueInboxSteer === "function") {
    if (typeof binding.EnqueueInboxSteerForTurn === "function") {
      if (!opts.turnId) return Promise.reject(new Error("active turn id is unavailable; refresh and try again"));
      return binding.EnqueueInboxSteerForTurn(tabId, opts.turnId, display, submit || display, "");
    }
    return binding.EnqueueInboxSteer(tabId, display, submit || display, "");
  }
  return binding.EnqueueInboxFollowup(tabId, display, submit || display, "");
}
