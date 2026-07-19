export type ConversationWidth = "standard" | "full";

export const CONVERSATION_WIDTH_STORAGE_KEY = "reasonix-conv-width";
export const STANDARD_CONVERSATION_MAX_WIDTH = "960px";
export const FULL_CONVERSATION_MAX_WIDTH = "max(960px, 90%)";

export function normalizeConversationWidth(value: unknown): ConversationWidth {
  return value === "full" ? "full" : "standard";
}

// localStorage is only an early-paint cache. The persisted desktop config is
// authoritative once DesktopStartupSettings / Settings has loaded.
export function getCachedConversationWidth(): ConversationWidth {
  try {
    return normalizeConversationWidth(localStorage.getItem(CONVERSATION_WIDTH_STORAGE_KEY));
  } catch {
    return "standard";
  }
}

export function applyConversationWidth(value: unknown): ConversationWidth {
  const width = normalizeConversationWidth(value);
  if (typeof document !== "undefined") {
    document.documentElement.style.setProperty(
      "--maxw",
      width === "full" ? FULL_CONVERSATION_MAX_WIDTH : STANDARD_CONVERSATION_MAX_WIDTH,
    );
    document.documentElement.setAttribute("data-conversation-width", width);
  }
  try {
    localStorage.setItem(CONVERSATION_WIDTH_STORAGE_KEY, width);
  } catch {
    // Storage may be unavailable in hardened webviews; config sync still works.
  }
  return width;
}

export function initConversationWidth(): void {
  applyConversationWidth(getCachedConversationWidth());
}
