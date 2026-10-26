import { useCallback, useEffect, useLayoutEffect, useRef, type RefObject } from "react";
import type { Item } from "../state/session";
import type { HistoryPaging } from "./useHistoryPages";
import { listenAction } from "./listen";
import { renderedBox } from "./findland";

type Landing = (block: number, into: number, selector: string, clear?: number, then?: (found: HTMLElement, root: HTMLElement) => boolean, highlight?: boolean) => void;

interface OlderPageIntent {
  scroll: RefObject<HTMLDivElement | null>;
  hidden: boolean;
  load: () => void;
  generation: { current: number };
}

export function useOlderPageIntent({ scroll, hidden, load, generation }: OlderPageIntent) {
  useEffect(() => {
    if (hidden) { generation.current++; return; }
    const root = scroll.current;
    if (!root) return;
    let intentUntil = 0;
    const intent = () => { intentUntil = performance.now() + 500; };
    const nearTop = () => { if (performance.now() < intentUntil && root && root.scrollTop < 100 && !document.getSelection()?.toString()) load(); };
    const older = (event: WheelEvent) => { if (event.deltaY < 0) { intent(); nearTop(); } };
    const key = (event: KeyboardEvent) => { if (["ArrowUp", "PageUp", "Home"].includes(event.key)) { intent(); nearTop(); } };
    const offWheel = listenAction(root, "wheel", { action: "history.more", listener: older as EventListener }, { passive: true });
    const offPointer = listenAction(root, "pointerdown", { action: "history.more", listener: intent });
    const offTouch = listenAction(root, "touchmove", { action: "history.more", listener: intent }, { passive: true });
    const offKey = listenAction(root, "keydown", { action: "history.more", listener: key as EventListener });
    const offScroll = listenAction(root, "scroll", { action: "history.more", listener: nearTop }, { passive: true });
    return () => { offWheel(); offPointer(); offTouch(); offKey(); offScroll(); };
  }, [scroll, hidden, load, generation]);
}

/** Pages read on the hook's own initiative, past the one the open already read. */
const READ_AHEAD = 3;

export function useHistoryAnchor(paging: HistoryPaging | undefined, scroll: RefObject<HTMLDivElement | null>, blocks: Item[][], hidden: boolean,
  generation: { current: number }, release: () => void, land: Landing, settled = false) {
  const pending = useRef<{ generation: number; revision: number; snapshot: string; id: string; offset: number } | null>(null);
  const read = useRef(0);
  const more = paging?.more;
  const load = useCallback(() => {
    const root = scroll.current;
    if (!root || !paging?.hasMore || paging.busy || !more) return;
    release();
    const mine = ++generation.current;
    const top = root.getBoundingClientRect().top;
    const anchor = [...root.querySelectorAll<HTMLElement>("[data-item]")].find((node) => {
      const rect = renderedBox(node).getBoundingClientRect();
      return rect.height > 0 && rect.bottom > top;
    });
    pending.current = anchor ? { generation: mine, revision: paging.revision, snapshot: paging.snapshot, id: anchor.dataset.item!, offset: renderedBox(anchor).getBoundingClientRect().top - top } : null;
    void more().catch(() => {});
  }, [scroll, paging?.hasMore, paging?.busy, paging?.revision, paging?.snapshot, more, generation, release]);

  // Reading ahead goes through `load`, never through `more`: only `load` records
  // the anchor a prepended page is placed against, so the reader's place holds.
  // It also releases the follow, which costs nothing while no turn is running.
  useEffect(() => {
    if (!settled || hidden || !paging?.hasMore || paging.busy || paging.failed) return;
    if (read.current >= READ_AHEAD) return;
    const timer = setTimeout(() => { read.current += 1; load(); }, 0);
    return () => clearTimeout(timer);
  }, [settled, hidden, paging?.hasMore, paging?.busy, paging?.failed, paging?.revision, load]);

  // A new conversation gets its own budget; the last one's must not spend it.
  useEffect(() => { read.current = 0; }, [paging?.snapshot]);

  useLayoutEffect(() => {
    const anchor = pending.current;
    if (!anchor || paging?.revision === anchor.revision) return;
    pending.current = null;
    if (hidden || anchor.generation !== generation.current || anchor.snapshot !== paging?.snapshot) return;
    const block = blocks.findIndex((items) => items.some((item) => item.id === anchor.id));
    if (block >= 0) land(block, 0, `[data-item="${CSS.escape(anchor.id)}"]`, anchor.offset, undefined, false);
  }, [paging?.revision, paging?.snapshot, blocks, hidden, generation, land]);
  return load;
}
