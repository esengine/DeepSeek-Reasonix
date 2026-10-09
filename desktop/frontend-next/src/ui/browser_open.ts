import type { AgentPort, BrowserTab } from "../port/port";

const opening = new WeakMap<AgentPort, { page: Promise<BrowserTab>; pending: boolean }>();

/** The one way this window asks the kernel for a tab. What is being opened is
 *  remembered until the kernel answers, so a column that needs a page to show
 *  takes that one rather than asking for another. */
export function openPage(port: AgentPort, url: string): Promise<BrowserTab> {
  const page = port.browserOpen(url, true);
  const state = { page, pending: true };
  opening.set(port, state);
  const settled = () => { state.pending = false; };
  page.then(settled, settled);
  return page;
}

/** A page for a column that has none: the one already on its way, else a blank. */
export function pageForEmptyColumn(port: AgentPort): Promise<BrowserTab> {
  const current = opening.get(port);
  return newestPage(port, current?.pending ? current.page : openPage(port, "about:blank"));
}

async function newestPage(port: AgentPort, page: Promise<BrowserTab>): Promise<BrowserTab> {
  try {
    const tab = await page;
    const next = opening.get(port)?.page;
    return next && next !== page ? newestPage(port, next) : tab;
  } catch (error) {
    const next = opening.get(port)?.page;
    if (next && next !== page) return newestPage(port, next);
    throw error;
  }
}
