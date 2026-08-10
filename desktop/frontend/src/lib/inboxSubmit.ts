import type { AppBindings } from "./bridge";
import type { StructuredInvocationSubmit } from "./invocationDisplay";

type InboxEnqueueBindings = Pick<AppBindings, "EnqueueInboxFollowup" | "EnqueueInboxFollowupWithInvocations">;

export function enqueueInboxGuidance(
  binding: InboxEnqueueBindings,
  tabId: string,
  display: string,
  submit: string,
  structured?: StructuredInvocationSubmit,
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
  return binding.EnqueueInboxFollowup(tabId, display, submit || display, "");
}
