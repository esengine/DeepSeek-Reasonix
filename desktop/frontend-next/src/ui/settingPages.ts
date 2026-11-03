import type { Section } from "./prefsnav";

export interface SettingPage {
  id: string;
  label: string;
  anchors: readonly string[];
}

export const PAGES: Partial<Record<Section, readonly SettingPage[]>> = {
  tools: [
    { id: "approval", label: "settingPages.approvalAndRules", anchors: ["approval", "plan-approval", "external-origin", "rules", "tool-list", "present-files"] },
    { id: "sandbox", label: "settingPages.sandboxAndExecution", anchors: ["sandbox", "shell"] },
    { id: "web", label: "settingPages.webAndBrowser", anchors: ["web-access", "browser-tools"] },
    { id: "subagents", label: "settingPages.subAgentsAndCollaboration", anchors: ["subagent-claims", "write-lease"] },
  ],
  appearance: [
    { id: "look", label: "settingPages.appearanceAndTheme", anchors: ["mode", "scheme", "font", "size", "weight", "contrast", "effects", "wallpaper", "language", "advanced-appearance"] },
    { id: "navigation", label: "settingPages.navigationAndSidebar", anchors: ["navrail", "naventries", "session-quick-actions", "legacy-sessions", "live-grace", "open-session-limit", "live-stream-budget", "session-tabs"] },
    { id: "conversation", label: "settingPages.conversationDisplay", anchors: ["new-answer-marker", "composer-hint", "folding", "plan-progress", "office-preview"] },
    { id: "shortcuts", label: "common.keyboardShortcuts", anchors: ["send-shortcut", "running-message-intent", "esc-stop", "global-shortcut", "keys"] },
    { id: "window", label: "settingPages.windowAndPerformance", anchors: ["window", "notify", "page-sleep", "graphics"] },
  ],
};

export const pagesOf = (section: Section): readonly SettingPage[] => PAGES[section] ?? [];

export const firstPage = (section: Section): string | undefined => PAGES[section]?.[0]?.id;

export const pageOf = (section: Section, anchor: string): string | undefined =>
  PAGES[section]?.find((p) => p.anchors.includes(anchor))?.id;

export const hiddenAnchors = (section: Section, page: string): string[] => {
  const pages = PAGES[section];
  if (!pages) return [];
  return pages.filter((p) => p.id !== page).flatMap((p) => [...p.anchors]);
};
