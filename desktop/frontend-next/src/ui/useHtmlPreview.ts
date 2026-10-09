import { useCallback, useMemo, useRef, useState } from "react";
import { host } from "../port/host";
import type { AgentPort, BrowserTab } from "../port/port";
import { openPage } from "./browser_open";
import { UNREAD_TABS } from "./BrowserPanel";

const HTML = /\.html?$/i;

/** Where a workspace file lives on the kernel's machine, spelled as a path: the
 *  browser reads a disk path as the file it names and judges it against the
 *  workspace itself. */
function diskPath(root: string, path: string): string {
  return `${root.replace(/[\\/]+$/, "")}/${path}`;
}

/** The disk path a file: address names, comparable with diskPath's, or "". */
function addressPath(address: string): string {
  try {
    const url = new URL(address);
    if (url.protocol !== "file:") return "";
    const path = decodeURIComponent(url.pathname);
    return /^\/[A-Za-z]:/.test(path) ? path.slice(1) : path;
  } catch {
    return "";
  }
}

function same(a: string, b: string): boolean {
  const flat = (s: string) => s.replaceAll("\\", "/");
  return /^[A-Za-z]:/.test(a) ? flat(a).toLowerCase() === flat(b).toLowerCase() : flat(a) === flat(b);
}

export interface HtmlPreview {
  /** The agent's pages without the ones that exist to preview a file. */
  tabs: BrowserTab[];
  /** Whether this window can draw the file as a page. */
  can(path: string): boolean;
  tabFor(path: string): BrowserTab | undefined;
  /** Puts the file in a page beside the agent's, or finds the one already there. */
  open(path: string, revision: string | undefined): Promise<void>;
  /** Reloads the page when the file on disk is no longer the one it loaded. */
  sync(path: string, revision: string): void;
}

/** A file previewed as a page in the one browser the person and the agent
 *  share. The page is the file's own view, so it is kept out of the strip,
 *  which would otherwise show the same file twice. */
export function useHtmlPreview(port: AgentPort, all: BrowserTab[], remote: boolean): HtmlPreview {
  const [owned, setOwned] = useState<Record<string, BrowserTab>>({});
  // The pages that existed when an open began: the one it adds is not the
  // agent's to be followed, however early the tab list reports it.
  const [claim, setClaim] = useState<Set<string> | null>(null);
  const loaded = useRef(new Map<string, string | undefined>());
  const inflight = useRef(new Map<string, Promise<void>>());
  const latest = useRef(all);
  latest.current = all;

  const tabs = useMemo(() => {
    if (all === UNREAD_TABS) return all;
    const mine = new Set(Object.values(owned).map((tab) => tab.target));
    return all.filter((tab) => !mine.has(tab.target) && !(claim && !claim.has(tab.target)));
  }, [all, owned, claim]);

  const can = useCallback(
    (path: string) => !remote && HTML.test(path) && host().drawsBrowserViews(),
    [remote],
  );
  const tabFor = useCallback(
    (path: string) => {
      const mine = owned[path];
      return mine && (all.find((tab) => tab.target === mine.target) ?? mine);
    },
    [owned, all],
  );

  const open = useCallback(
    (path: string, revision: string | undefined) => {
      const running = inflight.current.get(path);
      if (running) return running;
      const run = (async () => {
        // A list not read yet cannot say whether the page is already there.
        if (latest.current === UNREAD_TABS) return;
        const root = (await port.workspaces()).current;
        if (!root) throw new Error("this session has no workspace");
        const where = diskPath(root, path);
        const there = latest.current.find((tab) => !tab.active && same(addressPath(tab.url), where));
        let page = there;
        if (!page) {
          setClaim(new Set(latest.current.map((tab) => tab.target)));
          page = await openPage(port, where);
        }
        loaded.current.set(path, revision);
        setOwned((v) => ({ ...v, [path]: page }));
      })().finally(() => {
        inflight.current.delete(path);
        if (!inflight.current.size) setClaim(null);
      });
      inflight.current.set(path, run);
      return run;
    },
    [port],
  );

  const sync = useCallback(
    (path: string, revision: string) => {
      const page = owned[path];
      if (!page || !loaded.current.has(path) || loaded.current.get(path) === revision) return;
      loaded.current.set(path, revision);
      host().controlBrowserView(page.target, "reload");
    },
    [owned],
  );

  return { tabs, can, tabFor, open, sync };
}
