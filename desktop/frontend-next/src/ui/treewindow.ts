import { useMemo } from "react";
import type { TreeScope, TreeWindow, TreeWorkspace } from "../port/hub";

export const TREE_PAGE = 50;
/** The most rows of one folder the sidebar will hold. Past it the reader
 *  narrows with a search instead of loading more, so the list a refresh
 *  re-reads and the nodes it draws stay bounded however long a history is. */
export const TREE_MAX = 300;

/** What the sidebar is looking at: which rows the kernel's window should count,
 *  and the word it should keep them to. */
export interface TreeView {
  scope: TreeScope;
  q: string;
}

export interface TreePaging {
  /** Asks the kernel for one more page of a folder's conversations. */
  more: (root: string) => Promise<void>;
  /** Tells the window what the reader is looking at; rows are re-read from the kernel. */
  view: (view: TreeView) => void;
}

export const NO_PAGING: TreePaging = { more: async () => {}, view: () => {} };

/** How many rows of each folder the window has asked the kernel for, under the
 *  view it was asked in. A reload asks for the same rows again, so an opened-up
 *  folder stays opened up; a different view starts from the first page. */
export function useTreeWindow() {
  return useMemo(() => {
    let wanted: Record<string, number> = {};
    let looking: TreeView = { scope: "", q: "" };
    return {
      current: (): TreeWindow => ({ limit: TREE_PAGE, limits: wanted, scope: looking.scope, q: looking.q }),
      async grow(root: string, read: () => Promise<void>) {
        const before = wanted;
        wanted = { ...wanted, [root]: Math.min(TREE_MAX, (wanted[root] ?? TREE_PAGE) + TREE_PAGE) };
        try {
          await read();
        } catch (e) {
          wanted = before;
          throw e;
        }
      },
      view(next: TreeView) {
        if (next.scope === looking.scope && next.q === looking.q) return false;
        looking = next;
        wanted = {};
        return true;
      },
    };
  }, []);
}

/** Rows the kernel's view of a folder holds that this window has not loaded. A
 *  kernel that predates the view says only the folder's total. */
export const unloaded = (ws: TreeWorkspace, loaded: number) => ws.remaining ?? Math.max(0, (ws.total ?? loaded) - loaded);

/** True when a folder's window has reached the most the sidebar will hold. */
export const atCap = (loaded: number) => loaded >= TREE_MAX;

/** Whether the rows in hand answer the view being asked for. Until the kernel
 *  answers it they belong to the last view, and filtering them to this one would
 *  say "none" for a list that is merely still being read. */
export const answers = (answered: TreeView | null, asked: TreeView) =>
  !answered || (answered.scope === asked.scope && answered.q === asked.q);
