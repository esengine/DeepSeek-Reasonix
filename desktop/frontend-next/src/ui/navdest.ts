import { useMemo, useSyncExternalStore } from "react";
import { arranged, navLayout, onNavLayoutChange } from "../state/navEntries";
import type { Section } from "./prefsnav";

export type Mark = "chat" | "files" | Section;
export type Destination = [mark: Mark, label: string, section: string, action?: string];

// One list of places the window can go, drawn twice: icon-only on the rail and
// with its name on the sidebar. Names are the settings table's own (prefsnav
// NAV), so a destination is called the same thing wherever it is offered.
export const DESTINATIONS: Destination[] = [
  ["usage", "用量", "usage"],
  ["files", "文件", "files", "files.open"],
  ["tools", "工具与权限", "tools"],
  ["ext", "扩展", "ext"],
  ["memory", "记忆", "memory"],
  ["remote", "远程", "remote"],
];

const KEYS = DESTINATIONS.map(([mark]) => mark);

/** The destinations this machine chose to show, in its order. */
export function useDestinations(): Destination[] {
  const layout = useSyncExternalStore(onNavLayoutChange, navLayout, navLayout);
  return useMemo(() => arranged(KEYS, layout).map((k) => DESTINATIONS.find(([mark]) => mark === k)!), [layout]);
}
