import type { BrowserTab } from "../port/port";
import { t } from "../i18n";

export type Surface =
  | { kind: "manual"; id: string }
  | { kind: "browser"; tab: BrowserTab }
  | { kind: "file"; path: string };
export function keyOf(s: Surface) {
  return `${s.kind}:${s.kind === "manual" ? s.id : s.kind === "browser" ? s.tab.target : s.path}`;
}
export function labelOf(s: Surface, hosts: Record<string, string>) {
  return s.kind === "manual"
    ? hosts[s.id] || t("浏览器")
    : s.kind === "browser"
      ? s.tab.title || s.tab.url || t("空白页")
      : s.path.split("/").at(-1) || s.path;
}
