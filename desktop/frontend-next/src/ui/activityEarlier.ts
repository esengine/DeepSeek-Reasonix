import { t } from "../i18n";
import type { Item } from "../state/session";
import { categoryOf } from "./icons";
import { toolFailed } from "./cards/outcome";

export const KEEP_RECENT = 6;

const SEARCH = /grep|search|glob|find/;

/** The verb an activity row leads with, or null when the call's category has none and its own name says more. */
export function activityVerb(name: string): string | null {
  const category = categoryOf(name);
  if (category === "read") return SEARCH.test(name) ? "chat.search" : "chat.read";
  if (category === "write") return "common.edit";
  if (category === "bash") return "chat.run_command";
  if (category === "net") return "chat.network";
  if (category === "deleg") return "chat.subtask";
  if (category === "mcp") return "settings.external_tools";
  return null;
}

function verbOf(name: string): string {
  return activityVerb(name) ?? "ext.other";
}

const nameOf = (tool: { name?: string; resolvedName?: string }) => tool.resolvedName || tool.name || "";
const stays = (item: Item) => (item.t === "tool" && (item.running || toolFailed(item.tool)))
  || (item.t === "reads" && item.tools.some(toolFailed));

export function foldEarlier(items: Item[]): { earlier: Item[]; recent: Item[]; kinds: [string, number][] } {
  if (items.length <= KEEP_RECENT) return { earlier: [], recent: items, kinds: [] };
  const cut = items.length - KEEP_RECENT;
  const earlier: Item[] = [];
  const recent: Item[] = [];
  items.forEach((item, i) => (i < cut && !stays(item) ? earlier : recent).push(item));
  const counts = new Map<string, number>();
  const add = (name: string) => counts.set(verbOf(name), (counts.get(verbOf(name)) ?? 0) + 1);
  for (const item of earlier) {
    if (item.t === "reads") item.tools.forEach((tool) => add(nameOf(tool)));
    else if (item.t === "tool") add(nameOf(item.tool));
  }
  return { earlier, recent, kinds: [...counts.entries()].map(([verb, n]) => [t(verb), n] as [string, number]) };
}
