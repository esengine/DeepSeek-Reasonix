import type { AppBindings } from "./bridge";
import type { StructuredInvocationSubmit } from "./invocationDisplay";
import { resolveActiveTurnId } from "./inboxSubmit";

type InboxEnqueueBindings = Pick<AppBindings, "EnqueueInboxFollowup" | "EnqueueInboxFollowupWithInvocations" | "EnqueueInboxSteer" | "EnqueueInboxSteerForTurn" | "EnqueueForAttachmentTarget">;

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
  opts?: { steer?: boolean; turnId?: string; idempotency?: string },
) {
	if (structured) {
		if (structured.attachments?.length) {
			if (!structured.attachmentTarget || !binding.EnqueueForAttachmentTarget) {
				return Promise.reject(new Error("unsupported: attachments-v2"));
			}
			return binding.EnqueueForAttachmentTarget(
				structured.attachmentTarget,
				structured.attachmentSubmissionId || opts?.idempotency || `image-${crypto.randomUUID()}`,
				structured.input.trim(),
				structured.display.trim() || display,
				structured.invocations,
				structured.attachments,
			);
		}
    return binding.EnqueueInboxFollowupWithInvocations(
      tabId,
      structured.display.trim() || display,
      structured.input.trim(),
      structured.invocations,
      opts?.idempotency ?? "",
    );
  }
  if (opts?.steer && typeof binding.EnqueueInboxSteer === "function") {
    if (typeof binding.EnqueueInboxSteerForTurn === "function") {
      if (!opts.turnId) return Promise.reject(new Error("active turn id is unavailable; refresh and try again"));
      return binding.EnqueueInboxSteerForTurn(tabId, opts.turnId, display, submit || display, "");
    }
    return binding.EnqueueInboxSteer(tabId, display, submit || display, "");
  }
  return binding.EnqueueInboxFollowup(tabId, display, submit || display, opts?.idempotency ?? "");
}
