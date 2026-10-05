import { useEffect, useRef } from "react";
import type { BrowserTab } from "../port/session";
import type { Item } from "../state/session";
import { UNREAD_TABS } from "./BrowserPanel";

/** Opens the browser column when the agent opens a page the reader has not
 *  seen, in the pane being looked at. The first list a pane reads is what was
 *  already there, so remounting or switching back never counts as an opening;
 *  a column the reader closed stays closed until another page arrives. */
export function useRevealAgentPages(pages: BrowserTab[], active: boolean, reveal: () => void) {
  const seen = useRef<Set<string> | null>(null);
  useEffect(() => {
    if (pages === UNREAD_TABS) return;
    const ids = pages.map((p) => p.target || p.id);
    if (seen.current === null) {
      seen.current = new Set(ids);
      return;
    }
    const fresh = ids.some((id) => !seen.current!.has(id));
    for (const id of ids) seen.current.add(id);
    if (fresh && active) reveal();
  }, [pages, active, reveal]);
}

// The tool whose whole purpose is to put a page in front of the agent. It may
// navigate a tab already open, which adds no tab for the rule above to see.
const OPENS_A_PAGE = "browser_open";

/** Opens the browser column when the agent starts opening a page in the pane
 *  being looked at. Only a call seen running counts, so a transcript read back
 *  from history never opens it. */
export function useRevealBrowserOpen(items: readonly Item[], active: boolean, reveal: () => void) {
  const seen = useRef(new Set<string>());
  useEffect(() => {
    for (const it of items) {
      if (it.t !== "tool" || !it.running) continue;
      if ((it.tool.resolvedName || it.tool.name) !== OPENS_A_PAGE || seen.current.has(it.id)) continue;
      seen.current.add(it.id);
      if (active) reveal();
    }
  }, [items, active, reveal]);
}
