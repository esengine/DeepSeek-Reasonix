// MarkdownHistory — history (non-streaming) Markdown rendering driven by the
// parse worker (Phase E). Mounted rows: check the transcript markdown cache
// (stable row key + content revision) → on miss request a worker parse → render the
// resulting HAST blocks with the same components map react-markdown uses.
// Unmounted rows never reach this component, so cold-zone rows never parse.
//
// While a parse is in flight the caller-provided fallback stays on screen
// (plain full text for a fresh history mount — never truncated — or the
// committed streaming view when a live answer just completed). A single huge
// row keeps a viewport-driven tail window: opening a session paints its newest
// blocks, and scrolling toward older content prepends another bounded chunk.

import { Fragment, memo, startTransition, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { hastBlockToJsx } from "../lib/hastJsx";
import {
  estimateHastBytes,
  markdownContentRevision,
  type MarkdownBlock,
} from "../lib/markdownPipeline";
import { getMarkdownWorkerClient } from "../lib/markdownWorkerClient";
import { getTranscriptStore } from "../lib/transcriptStore";
import { createComponents } from "./markdownComponents";
import { VirtualMarkdownSourceTable } from "./MarkdownTable";
import { useTranscriptScrollOffsetWrite } from "./TranscriptLayoutIntentContext";

// A history surface opens at the newest transcript content. Keep the same
// ownership inside a giant Markdown row: mount a small tail, then move a
// bounded two-sided window only when one of its edges enters the viewport.
// The previous idle loop forced one React/layout commit per second until every block was in
// the DOM, which could keep WebView2 busy for minutes after a session switch.
const MARKDOWN_TAIL_BLOCKS = 24;
const MARKDOWN_PREPEND_BLOCKS = 96;
const MARKDOWN_WINDOW_BLOCKS = MARKDOWN_TAIL_BLOCKS + MARKDOWN_PREPEND_BLOCKS * 2;
const MARKDOWN_SENTINEL_STYLE = { display: "block", height: 1 } as const;
const MARKDOWN_ANCHOR_STYLE = { display: "block", height: 0 } as const;
const MARKDOWN_FALLBACK_MARKER_STYLE = { display: "none" } as const;
const MARKDOWN_PARSE_SWAP_BOTTOM_EPSILON_PX = 2;

type BlockWindow = {
  identity: MarkdownBlock[] | undefined;
  start: number;
  end: number;
};

type PendingScrollAnchor = {
  identity: MarkdownBlock[];
  index: number;
  top: number;
  scroller: HTMLElement | null;
};

function cachedBlocks(cacheKey: string | undefined, revision: number, text: string): MarkdownBlock[] | undefined {
  if (!cacheKey) return undefined;
  const cached = getTranscriptStore().getMarkdown(cacheKey, revision);
  // The revision is a content hash; the stored source comparison is the
  // fidelity backstop against collisions and stale writes.
  return cached && cached.source === text ? cached.blocks : undefined;
}

/** Keep a bounded block window whose edges advance only on viewport demand. */
function useProgressiveBlockWindow(total: number, identity: MarkdownBlock[] | undefined): [BlockWindow, (direction: "older" | "newer") => void] {
  const initial = useMemo<BlockWindow>(() => ({
    identity,
    start: Math.max(0, total - MARKDOWN_TAIL_BLOCKS),
    end: total,
  }), [identity, total]);
  const [window, setWindow] = useState(initial);
  // Derive the new tail synchronously so a worker result never performs one
  // discarded full-document JSX conversion before the reset effect commits.
  const current = window.identity === identity ? window : initial;
  useEffect(() => {
    setWindow((value) => value.identity === identity ? value : initial);
  }, [identity, initial]);
  const move = useCallback((direction: "older" | "newer") => {
    setWindow((value) => {
      const active = value.identity === identity ? value : initial;
      if (direction === "older") {
        const start = Math.max(0, active.start - MARKDOWN_PREPEND_BLOCKS);
        return { identity, start, end: Math.min(active.end, start + MARKDOWN_WINDOW_BLOCKS) };
      }
      const end = Math.min(total, active.end + MARKDOWN_PREPEND_BLOCKS);
      return { identity, start: Math.max(active.start, end - MARKDOWN_WINDOW_BLOCKS), end };
    });
  }, [identity, initial, total]);
  return [current, move];
}

function useBlockWindowSentinel(
  identity: MarkdownBlock[] | undefined,
  enabled: boolean,
  onEnter: (sentinel: HTMLSpanElement) => void,
) {
  const sentinelRef = useRef<HTMLSpanElement>(null);
  const armedRef = useRef(true);
  const observedIdentityRef = useRef(identity);
  useEffect(() => {
    if (observedIdentityRef.current !== identity) {
      observedIdentityRef.current = identity;
      armedRef.current = true;
    }
    const sentinel = sentinelRef.current;
    if (!enabled || !sentinel || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver((entries) => {
      const visible = entries.some((entry) => entry.isIntersecting);
      if (!visible) {
        armedRef.current = true;
        return;
      }
      if (!armedRef.current) return;
      armedRef.current = false;
      startTransition(() => onEnter(sentinel));
    }, { rootMargin: "240px 0px" });
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [enabled, identity, onEnter]);
  return sentinelRef;
}

export const MarkdownHistory = memo(function MarkdownHistory({
  text,
  plainStatusBlocks = false,
  cacheKey,
  entryId,
  fallback,
  onParsed,
  onError,
}: {
  text: string;
  plainStatusBlocks?: boolean;
  /** Stable transcript item key — enables cache reuse across live/history hosts. */
  cacheKey?: string;
  /** @deprecated Use cacheKey. Retained for focused history callers. */
  entryId?: string;
  /** What to show while the worker parses (plain text or the streaming view). */
  fallback: ReactNode;
  onParsed?: () => void;
  onError?: () => void;
}) {
  const stableCacheKey = cacheKey ?? entryId;
  const revision = useMemo(() => markdownContentRevision(text), [text]);
  // Parsed state is keyed by its source text: a text change renders the
  // fallback (never stale blocks) until the new parse lands.
  const [parsed, setParsed] = useState<{ text: string; blocks: MarkdownBlock[] } | undefined>(() => {
    const cached = cachedBlocks(stableCacheKey, revision, text);
    return cached ? { text, blocks: cached } : undefined;
  });
  const blocks = parsed && parsed.text === text ? parsed.blocks : undefined;
  const fallbackMarkerRef = useRef<HTMLSpanElement>(null);

  useEffect(() => {
    const cached = cachedBlocks(stableCacheKey, revision, text);
    if (cached) {
      setParsed({ text, blocks: cached });
      onParsed?.();
      return;
    }
    const handle = getMarkdownWorkerClient().parse(text);
    let cancelled = false;
    let releaseDeferredCommit: (() => void) | undefined;
    handle.promise
      .then((result) => {
        if (cancelled || !result) return;
        if (stableCacheKey) {
          getTranscriptStore().setMarkdown(stableCacheKey, revision, {
            source: text,
            blocks: result.blocks,
            selectionText: result.selectionText,
            selectionRevision: result.selectionRevision,
            bytes: text.length * 2 + result.selectionText.length * 2 + estimateHastBytes(result.blocks),
          });
        }
        const next = { text, blocks: result.blocks };
        const commit = () => {
          if (cancelled) return;
          releaseDeferredCommit?.();
          releaseDeferredCommit = undefined;
          setParsed(next);
          onParsed?.();
        };
        const scroller = fallbackMarkerRef.current?.closest<HTMLElement>(".transcript") ?? null;
        const isAtBottom = () => !scroller
          || scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight <= MARKDOWN_PARSE_SWAP_BOTTOM_EPSILON_PX;
        if (isAtBottom()) {
          commit();
          return;
        }

        // A fresh history mount uses the complete plain-text source while the
        // worker parses. Replacing that source mid-read with the bounded tail
        // block window removes the reader's visible blocks from the DOM and
        // makes the native scroller jump to the end. Cache the result above,
        // but keep the stable fallback until the reader deliberately returns
        // to the bottom; that handoff needs no competing scroll write.
        const handleScroll = () => {
          if (isAtBottom()) commit();
        };
        scroller?.addEventListener("scroll", handleScroll, { passive: true });
        releaseDeferredCommit = () => scroller?.removeEventListener("scroll", handleScroll);
      })
      .catch(() => {
        if (!cancelled) onError?.();
      });
    return () => {
      cancelled = true;
      releaseDeferredCommit?.();
      handle.cancel();
    };
    // onParsed/onError are stable caller callbacks; re-running per identity
    // change would re-request parses the cache already serves.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [text, stableCacheKey, revision]);

  const components = useMemo(() => createComponents(plainStatusBlocks), [plainStatusBlocks]);
  const totalBlocks = blocks?.length ?? 0;
  const [blockWindow, moveBlockWindow] = useProgressiveBlockWindow(totalBlocks, blocks);
  const rootRef = useRef<HTMLDivElement>(null);
  const pendingAnchorRef = useRef<PendingScrollAnchor | null>(null);
  const writeTranscriptOffset = useTranscriptScrollOffsetWrite();
  const moveFromBoundary = useCallback((direction: "older" | "newer", sentinel: HTMLSpanElement) => {
    if (!blocks) return;
    if (pendingAnchorRef.current?.identity === blocks) return;
    const scroller = rootRef.current?.closest<HTMLElement>(".transcript") ?? null;
    const index = direction === "older" ? blockWindow.start : blockWindow.end;
    const boundary = sentinel.getBoundingClientRect();
    pendingAnchorRef.current = {
      identity: blocks,
      index,
      // The older sentinel sits immediately before the old leading block, so
      // its bottom is that block's stable boundary. The newer sentinel's top
      // is already the boundary immediately after the old trailing block.
      top: direction === "older" ? boundary.bottom : boundary.top,
      scroller,
    };
    moveBlockWindow(direction);
  }, [blockWindow.end, blockWindow.start, blocks, moveBlockWindow]);
  const loadOlder = useCallback((sentinel: HTMLSpanElement) => moveFromBoundary("older", sentinel), [moveFromBoundary]);
  const loadNewer = useCallback((sentinel: HTMLSpanElement) => moveFromBoundary("newer", sentinel), [moveFromBoundary]);
  const olderSentinelRef = useBlockWindowSentinel(blocks, blockWindow.start > 0, loadOlder);
  const newerSentinelRef = useBlockWindowSentinel(blocks, blockWindow.end < totalBlocks, loadNewer);

  useLayoutEffect(() => {
    const pending = pendingAnchorRef.current;
    if (!pending) return;
    if (pending.identity !== blocks) {
      pendingAnchorRef.current = null;
      return;
    }
    const anchor = rootRef.current?.querySelector<HTMLElement>(`[data-markdown-scroll-anchor="${pending.index}"]`);
    pendingAnchorRef.current = null;
    if (!anchor || !pending.scroller?.isConnected) return;
    const delta = anchor.getBoundingClientRect().top - pending.top;
    if (delta !== 0 && writeTranscriptOffset) {
      writeTranscriptOffset("block-window-prepend", pending.scroller.scrollTop + delta);
    }
  }, [blockWindow.end, blockWindow.start, blocks, writeTranscriptOffset]);

  // JSX per block depends only on the block and the components map; build it
  // lazily so viewport-window growth never re-converts settled blocks.
  const jsxCacheRef = useRef<{ blocks: MarkdownBlock[]; nodes: Map<number, ReactNode> } | null>(null);
  if (!blocks) {
    return (
      <>
        <span ref={fallbackMarkerRef} style={MARKDOWN_FALLBACK_MARKER_STYLE} data-markdown-fallback-marker aria-hidden="true" />
        {fallback}
      </>
    );
  }
  let cache = jsxCacheRef.current;
  if (!cache || cache.blocks !== blocks) {
    cache = { blocks, nodes: new Map<number, ReactNode>() };
    jsxCacheRef.current = cache;
  }
  for (const index of cache.nodes.keys()) {
    if (index < blockWindow.start || index >= blockWindow.end) cache.nodes.delete(index);
  }
  return (
    <div
      ref={rootRef}
      className="md"
      data-markdown-blocks={blocks.length}
      data-markdown-visible-blocks={blockWindow.end - blockWindow.start}
      data-markdown-window-start={blockWindow.start}
      data-markdown-window-end={blockWindow.end}
      data-markdown-window-cap={MARKDOWN_WINDOW_BLOCKS}
    >
      {blockWindow.start > 0 && <span ref={olderSentinelRef} style={MARKDOWN_SENTINEL_STYLE} data-markdown-older-sentinel aria-hidden="true" />}
      {blocks.slice(blockWindow.start, blockWindow.end).map((block, offset) => {
        const index = blockWindow.start + offset;
        const cached = cache.nodes.get(index);
        const node = cached !== undefined
          ? cached
          : block.virtualTable
            ? <VirtualMarkdownSourceTable data={block.virtualTable} />
            : hastBlockToJsx(block, components);
        if (cached === undefined) cache.nodes.set(index, node);
        return (
          <Fragment key={block.key}>
            {pendingAnchorRef.current?.identity === blocks && pendingAnchorRef.current.index === index
              ? <span style={MARKDOWN_ANCHOR_STYLE} data-markdown-scroll-anchor={index} aria-hidden="true" />
              : null}
            {node}
          </Fragment>
        );
      })}
      {blockWindow.end < blocks.length && <span ref={newerSentinelRef} style={MARKDOWN_SENTINEL_STYLE} data-markdown-newer-sentinel aria-hidden="true" />}
    </div>
  );
});

export default MarkdownHistory;
