import { restoreAttachmentRefsForSubmit } from "./attachmentDisplay";

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

  const originalFallbackSubmit = restoreAttachmentRefsForSubmit(originalDisplay).trim();
  if (originalFallbackSubmit && originalSubmit.endsWith(originalFallbackSubmit)) {
    return `${originalSubmit.slice(0, originalSubmit.length - originalFallbackSubmit.length)}${fallbackSubmit}`.trim();
  }
  return fallbackSubmit;
}
