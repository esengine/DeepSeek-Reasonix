import type { Section } from "./prefsnav";

export interface SettingPage {
  id: string;
  label: string;
  anchors: readonly string[];
}

export const PAGES: Partial<Record<Section, readonly SettingPage[]>> = {
  tools: [
    { id: "approval", label: "批准与规则", anchors: ["approval", "plan-approval", "external-origin", "rules", "tool-list", "present-files"] },
    { id: "sandbox", label: "沙箱与执行", anchors: ["sandbox", "shell"] },
    { id: "web", label: "联网与浏览器", anchors: ["web-access", "browser-tools"] },
    { id: "subagents", label: "子代理与协作", anchors: ["subagent-claims", "subagent-steer", "write-lease"] },
  ],
  appearance: [
    { id: "look", label: "外观与主题", anchors: ["mode", "scheme", "font", "size", "weight", "contrast", "effects", "wallpaper", "language", "advanced-appearance"] },
    { id: "navigation", label: "导航与侧栏", anchors: ["navrail", "naventries", "session-quick-actions", "legacy-sessions", "live-grace", "open-session-limit", "live-stream-budget", "session-tabs"] },
    { id: "conversation", label: "会话显示", anchors: ["new-answer-marker", "composer-hint", "folding", "plan-progress", "office-preview"] },
    { id: "shortcuts", label: "快捷键", anchors: ["send-shortcut", "running-message-intent", "esc-stop", "global-shortcut", "keys"] },
    { id: "window", label: "窗口与性能", anchors: ["window", "notify", "page-sleep", "graphics"] },
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
