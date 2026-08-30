import type { AppBindings } from "./bridge";
import type { StructuredInvocationSubmit } from "./invocationDisplay";
import { asArray } from "./array";

type InboxEnqueueBindings = Pick<AppBindings, "EnqueueInboxFollowup" | "EnqueueInboxFollowupWithInvocations" | "EnqueueInboxSteer" | "EnqueueInboxSteerForTurn">;
type ActiveTurnBindings = Pick<AppBindings, "ListTabs" | "SteerInboxItem" | "SteerInboxItemForTurn">;

export async function resolveActiveTurnId(binding: Pick<AppBindings, "ListTabs">, tabId: string, known?: string): Promise<string | undefined> {
  if (known) return known;
  return asArray(await binding.ListTabs()).find((tab) => tab.id === tabId)?.turnId;
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
