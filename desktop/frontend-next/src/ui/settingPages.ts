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
    { id: "subagents", label: "子代理与协作", anchors: ["subagent-claims", "write-lease"] },
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
