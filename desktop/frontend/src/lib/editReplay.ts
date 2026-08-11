import { restoreAttachmentRefsForSubmit } from "./attachmentDisplay";
import { invocationSegmentsFromMessage, serializeInvocationSubmit, type ComposerInvocation } from "./invocationDisplay";
import { splitSelectedTextContext } from "./selectedTextContext";

export function replaySubmitText(
  originalSubmitText: string | undefined,
  originalDisplayText: string,
  nextDisplayText: string,
  fallbackSubmitText: string,
): string {
  const originalSubmit = (originalSubmitText ?? "").trim();
  const originalDisplay = originalDisplayText.trim();
  const nextDisplay = nextDisplayText.trim();
  const fallbackSubmit = fallbackSubmitText.trim();
  if (!originalSubmit || originalSubmit === originalDisplay) return fallbackSubmit;
  if (nextDisplay === originalDisplay) return originalSubmit;

  const invocationSegments = invocationSegmentsFromMessage(originalDisplay, originalSubmit);
  const invocationItems = invocationSegments.filter((segment) => segment.type === "invocation");
  if (invocationItems.length > 0) {
    const scale = originalDisplay.length > 0 ? nextDisplay.length / originalDisplay.length : 0;
    const invocations: ComposerInvocation[] = invocationItems.map((segment, index) => ({
      id: `edit-invocation-${index}`,
      offset: index === 0 ? 0 : Math.min(nextDisplay.length, Math.round(segment.offset * scale)),
      command: {
        name: segment.invocation.name,
        description: "",
        kind: segment.invocation.kind ?? "skill",
      },
    }));
    const serialized = serializeInvocationSubmit(fallbackSubmit, invocations).trim();
    // Keep any hidden submit prefix (referenced-session context, memory
    // framing) exactly like the plain-text branch below: the display maps to
    // the serialized slash tail of the original submit, and everything before
    // that tail rides along unchanged. No match (e.g. attachment-expanded
    // tails) falls back to the bare serialized form.
    const originalInvocations: ComposerInvocation[] = invocationItems.map((segment, index) => ({
      id: `edit-invocation-original-${index}`,
      offset: segment.offset,
      command: {
        name: segment.invocation.name,
        description: "",
        kind: segment.invocation.kind ?? "skill",
      },
    }));
    const originalSerialized = serializeInvocationSubmit(originalDisplay, originalInvocations).trim();
    if (originalSerialized && originalSubmit.length > originalSerialized.length && originalSubmit.endsWith(originalSerialized)) {
      return `${originalSubmit.slice(0, originalSubmit.length - originalSerialized.length)}${serialized}`.trim();
    }
    return serialized;
  }

  const originalFallbackSubmit = restoreAttachmentRefsForSubmit(originalDisplay).trim();
  if (originalFallbackSubmit && originalSubmit.endsWith(originalFallbackSubmit)) {
    return `${originalSubmit.slice(0, originalSubmit.length - originalFallbackSubmit.length)}${fallbackSubmit}`.trim();
  }
  return fallbackSubmit;
}

export function replaySubmitTextPreservingSelectedContext(
  originalSubmitText: string | undefined,
  originalDisplayText: string,
  nextDisplayText: string,
  fallbackSubmitText: string,
): string {
  const selected = splitSelectedTextContext(originalSubmitText);
  const replayed = replaySubmitText(
    selected.submitText,
    originalDisplayText,
    nextDisplayText,
    fallbackSubmitText,
  );
  return [replayed, selected.contextBlock].filter(Boolean).join("\n\n");
}
