import type { BrowserTab } from "../port/port";
import { t } from "../i18n";

export type Surface =
  | { kind: "manual"; id: string }
  | { kind: "browser"; tab: BrowserTab }
  | { kind: "file"; path: string }
  | { kind: "agent"; ref: string; name: string }
  | { kind: "job"; id: string; name: string }
  | { kind: "terminal"; id: string; name: string; busy: boolean }
  | { kind: "changes" };
export function keyOf(s: Surface) {
  if (s.kind === "changes") return "changes:";
  return `${s.kind}:${s.kind === "manual" || s.kind === "job" || s.kind === "terminal" ? s.id : s.kind === "browser" ? s.tab.target : s.kind === "agent" ? s.ref : s.path}`;
}
export function labelOf(s: Surface, hosts: Record<string, string>) {
  if (s.kind === "changes") return t("common.changes");
  return s.kind === "manual"
    ? hosts[s.id] || t("workbench.browser")
    : s.kind === "browser"
      ? s.tab.title || s.tab.url || t("common.blank_page")
      : s.kind === "agent" || s.kind === "job" || s.kind === "terminal"
        ? s.name
        : s.path.split("/").at(-1) || s.path;
}
