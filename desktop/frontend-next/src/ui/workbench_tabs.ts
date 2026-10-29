import type { BrowserTab } from "../port/port";
import type { ExtensionSurface } from "../port/wire";
import { t } from "../i18n";

export type Surface =
  | { kind: "manual"; id: string }
  | { kind: "browser"; tab: BrowserTab }
  | { kind: "file"; path: string }
  | { kind: "agent"; ref: string; name: string }
  | { kind: "extension"; ext: ExtensionSurface }
  | { kind: "extensions" };
export function keyOf(s: Surface) {
  switch (s.kind) {
    case "manual": return `manual:${s.id}`;
    case "browser": return `browser:${s.tab.target}`;
    case "agent": return `agent:${s.ref}`;
    case "file": return `file:${s.path}`;
    case "extension": return `extension:${s.ext.pluginId}:${s.ext.surfaceId}`;
    case "extensions": return "extensions:";
  }
}
export function labelOf(s: Surface, hosts: Record<string, string>) {
  switch (s.kind) {
    case "manual": return hosts[s.id] || t("浏览器");
    case "browser": return s.tab.title || s.tab.url || t("空白页");
    case "agent": return s.name;
    case "file": return s.path.split("/").at(-1) || s.path;
    case "extension": return s.ext.pluginId;
    case "extensions": return t("扩展");
  }
}
/** hasTabMenu says whether a tab carries the browser tab menu. */
export const hasTabMenu = (s: Surface) => s.kind === "manual" || s.kind === "browser" || s.kind === "agent";
